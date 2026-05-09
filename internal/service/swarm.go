package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/swarm"
)

// SwarmConfig configures swarm orchestration limits and behavior.
type SwarmConfig struct {
	MaxConcurrentAgents int
	MaxSubtasksPerPlan  int
	MaxStepsPerPlan     int
	StrictScope         bool
	RequireUserClose    bool
	DefaultAgent        string
}

// SwarmService is the conductor-driven orchestration surface. The CLI verbs
// (propose-plan, dispatch, message, broadcast, close, finish, progress,
// question, built) all route here.
type SwarmService interface {
	// Read-only views.
	ListSubtasks(ctx context.Context, parentID string) ([]store.Subtask, error)
	GetSubtask(ctx context.Context, id string) (*store.Subtask, error)
	ListSubtaskInfos(ctx context.Context, parentID string) ([]SwarmSubtaskInfo, error)
	Snapshot(ctx context.Context, parentID string) ([]byte, error)
	FetchInbox(ctx context.Context, parentID string) ([]InboxEntry, error)
	PeekInbox(ctx context.Context, parentID string) ([]InboxEntry, error)
	LoadPlan(path string) (*SwarmPlan, error)

	// Conductor verbs.
	StartSwarm(ctx context.Context, parentID, workingDir string) error
	ApplyApprovedPlan(ctx context.Context, plan *swarm.Plan) error
	Dispatch(ctx context.Context, subtaskID string) error
	NextStep(ctx context.Context, parentID string) error
	Message(ctx context.Context, subtaskID, text string) error
	MessageParent(ctx context.Context, parentID, text string) error
	Broadcast(ctx context.Context, parentID, text string) (int, error)
	Close(ctx context.Context, subtaskID string) error
	Finish(ctx context.Context, parentID, summary string) error

	// Worker verbs.
	Progress(ctx context.Context, subtaskID, text string) error
	Question(ctx context.Context, subtaskID, text string) error
	Built(ctx context.Context, subtaskID string) error

	// Lifecycle.
	HandleAgentDied(ctx context.Context, parentTaskID, subtaskID, role string)
	StartEventLoop(ctx context.Context) func()
}

// SwarmPlan is a service-layer DTO mirroring an engine swarm plan. Lets TUI
// callers render proposed plans without importing the engine package.
type SwarmPlan struct {
	Header   SwarmPlanHeader
	Subtasks []SwarmPlanSubtask
}

// SwarmPlanHeader carries swarm-level fields from a plan.
type SwarmPlanHeader struct {
	ParentTaskID string
	WorkingDir   string
	Summary      string
}

// SwarmPlanSubtask is a service-layer view of one plan entry.
type SwarmPlanSubtask struct {
	Title  string
	Role   string
	Agent  string
	Scope  []string
	Prompt string
}

// swarmService is the concrete implementation of SwarmService.
type swarmService struct {
	store    *store.Store
	agents   AgentService
	bus      *events.Bus
	cfg      SwarmConfig
	repoRoot string

	// Per-worker progress debouncer state. Keyed by subtask_id.
	debounceMu      sync.Mutex
	pendingProgress map[string]*pendingProgressEntry
	pendingMeta     map[string]progressMeta

	// Per-parent serializer for conductor-bound send-keys delivery. Without
	// this, two events (e.g. built + all_idle) can fire in microseconds and
	// claude's input handler may lose the Enter on the first.
	conductorLocksMu sync.Mutex
	conductorLocks   map[string]*sync.Mutex
}

type pendingProgressEntry struct {
	timer *time.Timer
	text  string
}

// NewSwarmService creates a SwarmService.
func NewSwarmService(s *store.Store, agents AgentService, bus *events.Bus, cfg SwarmConfig, repoRoot string) SwarmService {
	if cfg.MaxConcurrentAgents <= 0 {
		cfg.MaxConcurrentAgents = 4
	}
	if cfg.MaxSubtasksPerPlan <= 0 {
		cfg.MaxSubtasksPerPlan = 10
	}
	if cfg.MaxStepsPerPlan <= 0 {
		cfg.MaxStepsPerPlan = 10
	}
	return &swarmService{
		store:           s,
		agents:          agents,
		bus:             bus,
		cfg:             cfg,
		repoRoot:        repoRoot,
		pendingProgress: make(map[string]*pendingProgressEntry),
		pendingMeta:     make(map[string]progressMeta),
		conductorLocks:  make(map[string]*sync.Mutex),
	}
}

// LoadPlan reads a swarm plan from disk and converts it to the service-layer
// DTO. Lets TUI callers display proposed plans without depending on engine
// types.
func (s *swarmService) LoadPlan(path string) (*SwarmPlan, error) {
	p, err := swarm.LoadPlan(path)
	if err != nil {
		return nil, err
	}
	out := &SwarmPlan{
		Header: SwarmPlanHeader{
			ParentTaskID: p.Swarm.ParentTaskID,
			WorkingDir:   p.Swarm.WorkingDir,
			Summary:      p.Swarm.Summary,
		},
		Subtasks: make([]SwarmPlanSubtask, 0),
	}
	for _, step := range p.Steps {
		for _, st := range step.Subtasks {
			out.Subtasks = append(out.Subtasks, SwarmPlanSubtask{
				Title:  st.Title,
				Role:   st.Role,
				Agent:  st.Agent,
				Scope:  append([]string(nil), st.Scope...),
				Prompt: st.Prompt,
			})
		}
	}
	return out, nil
}

// conductorLock returns the per-parent mutex guarding send-keys delivery to
// that swarm's conductor. Created on first use.
func (s *swarmService) conductorLock(parentID string) *sync.Mutex {
	s.conductorLocksMu.Lock()
	defer s.conductorLocksMu.Unlock()
	mu, ok := s.conductorLocks[parentID]
	if !ok {
		mu = &sync.Mutex{}
		s.conductorLocks[parentID] = mu
	}
	return mu
}

// generateSubtaskID returns a 13-char id ("st-" + 10 hex chars).
func generateSubtaskID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return "st-" + hex.EncodeToString(b)
}

// ListSubtasks returns the sub-tasks for a parent task ordered by created_at.
func (s *swarmService) ListSubtasks(ctx context.Context, parentID string) ([]store.Subtask, error) {
	return s.store.ListSubtasksByParent(ctx, parentID)
}

// GetSubtask returns a single sub-task by ID, or ErrNotFound.
func (s *swarmService) GetSubtask(ctx context.Context, id string) (*store.Subtask, error) {
	return s.store.GetSubtask(ctx, id)
}

// InboxEntry is an unacked swarm event ready for the conductor to consume.
type InboxEntry struct {
	ID          int    `json:"id"`
	SubtaskID   string `json:"subtask_id,omitempty"`
	Kind        string `json:"kind"`
	WorkerTitle string `json:"worker,omitempty"`
	Payload     string `json:"payload"`
	CreatedAt   string `json:"created_at"`
}

// FetchInbox returns all unacked swarm events for a parent and marks them
// acked in a single transaction. Used by `legato swarm inbox <parent-id>`.
func (s *swarmService) FetchInbox(ctx context.Context, parentID string) ([]InboxEntry, error) {
	rows, err := s.store.ListUnackedSwarmEvents(ctx, parentID)
	if err != nil {
		return nil, err
	}
	out := make([]InboxEntry, len(rows))
	ids := make([]int, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		entry := InboxEntry{
			ID:          r.ID,
			Kind:        r.Kind,
			WorkerTitle: r.WorkerTitle,
			Payload:     r.Payload,
			CreatedAt:   r.CreatedAt,
		}
		if r.SubtaskID != nil {
			entry.SubtaskID = *r.SubtaskID
		}
		out[i] = entry
	}
	if len(ids) > 0 {
		if err := s.store.AckSwarmEvents(ctx, ids); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// PeekInbox returns all unacked swarm events for a parent WITHOUT acking them.
func (s *swarmService) PeekInbox(ctx context.Context, parentID string) ([]InboxEntry, error) {
	rows, err := s.store.ListUnackedSwarmEvents(ctx, parentID)
	if err != nil {
		return nil, err
	}
	out := make([]InboxEntry, len(rows))
	for i, r := range rows {
		entry := InboxEntry{
			ID:          r.ID,
			Kind:        r.Kind,
			WorkerTitle: r.WorkerTitle,
			Payload:     r.Payload,
			CreatedAt:   r.CreatedAt,
		}
		if r.SubtaskID != nil {
			entry.SubtaskID = *r.SubtaskID
		}
		out[i] = entry
	}
	return out, nil
}

// SwarmSubtaskInfo is a UI-friendly view of a sub-task.
type SwarmSubtaskInfo struct {
	ID          string
	Title       string
	Description string
	Role        string
	AgentKind   string
	Status      string
	Scope       []string
	WorkerID    *int
	StepIndex   int
	StartedAt   string
	CompletedAt string
}

// ListSubtaskInfos returns parsed sub-task summaries for a parent.
func (s *swarmService) ListSubtaskInfos(ctx context.Context, parentID string) ([]SwarmSubtaskInfo, error) {
	rows, err := s.store.ListSubtasksByParent(ctx, parentID)
	if err != nil {
		return nil, err
	}
	out := make([]SwarmSubtaskInfo, len(rows))
	for i, r := range rows {
		globs, _ := store.ParseScopeGlobs(r.ScopeGlobs)
		info := SwarmSubtaskInfo{
			ID:          r.ID,
			Title:       r.Title,
			Description: r.Description,
			Role:        r.Role,
			AgentKind:   r.AgentKind,
			Status:      r.Status,
			Scope:       globs,
			WorkerID:    r.BuilderAgentID,
			StepIndex:   r.StepIndex,
		}
		if r.StartedAt != nil {
			info.StartedAt = *r.StartedAt
		}
		if r.CompletedAt != nil {
			info.CompletedAt = *r.CompletedAt
		}
		out[i] = info
	}
	return out, nil
}

// SnapshotData is the JSON payload returned by Snapshot.
type SnapshotData struct {
	Parent   SnapshotParent    `json:"parent"`
	Subtasks []SnapshotSubtask `json:"subtasks"`
}

type SnapshotParent struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	WorkingDir string `json:"working_dir,omitempty"`
	ActiveStep int    `json:"active_step"`
}

type SnapshotSubtask struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Role        string   `json:"role"`
	AgentKind   string   `json:"agent,omitempty"`
	ScopeGlobs  []string `json:"scope_globs"`
	Status      string   `json:"status"`
	WorkerID    *int     `json:"worker_agent_id,omitempty"`
	StepIndex   int      `json:"step_index"`
	StartedAt   string   `json:"started_at,omitempty"`
	CompletedAt string   `json:"completed_at,omitempty"`
}

// Snapshot returns the JSON coordination surface for the swarm.
func (s *swarmService) Snapshot(ctx context.Context, parentID string) ([]byte, error) {
	parent, err := s.store.GetTask(ctx, parentID)
	if err != nil {
		return nil, err
	}
	subs, err := s.store.ListSubtasksByParent(ctx, parentID)
	if err != nil {
		return nil, err
	}
	out := SnapshotData{
		Parent: SnapshotParent{ID: parent.ID, Title: parent.Title, Status: parent.Status, ActiveStep: parent.SwarmActiveStep},
	}
	if parent.SwarmWorkingDir != nil {
		out.Parent.WorkingDir = *parent.SwarmWorkingDir
	}
	for _, st := range subs {
		globs, _ := store.ParseScopeGlobs(st.ScopeGlobs)
		ss := SnapshotSubtask{
			ID:          st.ID,
			Title:       st.Title,
			Description: st.Description,
			Role:        st.Role,
			AgentKind:   st.AgentKind,
			ScopeGlobs:  globs,
			Status:      st.Status,
			WorkerID:    st.BuilderAgentID,
			StepIndex:   st.StepIndex,
		}
		if st.StartedAt != nil {
			ss.StartedAt = *st.StartedAt
		}
		if st.CompletedAt != nil {
			ss.CompletedAt = *st.CompletedAt
		}
		out.Subtasks = append(out.Subtasks, ss)
	}
	return json.MarshalIndent(out, "", "  ")
}

// StartSwarm spawns the conductor for a parent task. Refuses if the parent
// already has a running agent or if working dir doesn't validate. Persists
// the working dir on the parent task. The conductor's brief is the parent
// task title + description + working-dir framing.
func (s *swarmService) StartSwarm(ctx context.Context, parentID, workingDir string) error {
	parent, err := s.store.GetTask(ctx, parentID)
	if err != nil {
		return fmt.Errorf("parent task %s: %w", parentID, err)
	}

	// Refuse double-spawn.
	if existing, err := s.store.GetAgentSessionByTaskID(ctx, parentID); err == nil && existing.Status == "running" {
		return fmt.Errorf("parent task %s already has a running agent — kill it before starting a swarm", parentID)
	}

	if err := s.store.SetTaskSwarmWorkingDir(ctx, parentID, &workingDir); err != nil {
		return fmt.Errorf("persist working dir: %w", err)
	}

	brief := fmt.Sprintf(
		"You are the swarm conductor for task **%s — %s**.\n\nWorking directory: %s\n\n## Parent task description\n\n%s",
		parent.ID, parent.Title, workingDir, parent.Description,
	)

	if err := s.agents.SpawnAgent(ctx, parentID, 0, 0, AgentSpawnOptions{
		Role:         "conductor",
		ParentTaskID: parentID,
		WorkingDir:   workingDir,
		AgentKind:    s.cfg.DefaultAgent,
		Brief:        brief,
	}); err != nil {
		// Roll back the working dir on failure.
		_ = s.store.SetTaskSwarmWorkingDir(ctx, parentID, nil)
		return fmt.Errorf("spawn conductor: %w", err)
	}

	s.publishChanged(parentID, "", "started")
	return nil
}

// ApplyApprovedPlan persists sub-tasks from a (post-approval) plan. Idempotent
// per (parent_task_id, title) combination is NOT guaranteed — callers should
// only call this once per approved plan.
func (s *swarmService) ApplyApprovedPlan(ctx context.Context, plan *swarm.Plan) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	for si, step := range plan.Steps {
		for _, spec := range step.Subtasks {
			raw, _ := store.MarshalScopeGlobs(spec.Scope)
			role := spec.Role
			if role == "" {
				role = "worker"
			}
			st := store.Subtask{
				ID:           generateSubtaskID(),
				ParentTaskID: plan.Swarm.ParentTaskID,
				Title:        spec.Title,
				Prompt:       spec.Prompt,
				ScopeGlobs:   raw,
				Role:         role,
				AgentKind:    spec.Agent,
				Status:       "queued",
				StepIndex:    si,
			}
			if err := s.store.CreateSubtask(ctx, st); err != nil {
				return fmt.Errorf("create sub-task %q: %w", spec.Title, err)
			}
		}
	}
	s.publishChanged(plan.Swarm.ParentTaskID, "", "plan_applied")
	return nil
}

// NextStep advances the swarm to the next step after validating that the
// current active step is terminal (all its sub-tasks are done or cancelled).
// Returns an error if the current step is not terminal or if there are no
// further steps.
func (s *swarmService) NextStep(ctx context.Context, parentID string) error {
	parent, err := s.store.GetTask(ctx, parentID)
	if err != nil {
		return fmt.Errorf("parent task %s: %w", parentID, err)
	}

	subs, err := s.store.ListSubtasksByParent(ctx, parentID)
	if err != nil {
		return fmt.Errorf("list subtasks: %w", err)
	}

	maxStep := parent.SwarmActiveStep
	for _, st := range subs {
		if st.StepIndex > maxStep {
			maxStep = st.StepIndex
		}
	}
	if parent.SwarmActiveStep >= maxStep {
		return fmt.Errorf("no more steps (current = %d, max = %d)", parent.SwarmActiveStep, maxStep)
	}

	// Validate current step is terminal.
	for _, st := range subs {
		if st.StepIndex == parent.SwarmActiveStep {
			if st.Status != "done" && st.Status != "cancelled" {
				return fmt.Errorf("step %d is not terminal: sub-task %s (%s) is %s",
					parent.SwarmActiveStep, st.ID, st.Title, st.Status)
			}
		}
	}

	if err := s.store.SetParentActiveStep(ctx, parentID, parent.SwarmActiveStep+1); err != nil {
		return fmt.Errorf("advance active step: %w", err)
	}

	s.publishChanged(parentID, "", "next_step")
	return nil
}

// Dispatch spawns the worker for a queued sub-task. Returns nil on success;
// when the cap is reached or the sub-task is in the wrong state, returns an
// error and the conductor receives a `[swarm event]` notification explaining.
func (s *swarmService) Dispatch(ctx context.Context, subtaskID string) error {
	st, err := s.store.GetSubtask(ctx, subtaskID)
	if err != nil {
		return err
	}
	if st.Status != "queued" {
		return fmt.Errorf("sub-task %s is %s, not queued", subtaskID, st.Status)
	}

	parent, err := s.store.GetTask(ctx, st.ParentTaskID)
	if err != nil {
		return fmt.Errorf("parent task %s: %w", st.ParentTaskID, err)
	}

	// Step gating: subtasks for future steps are deferred until NextStep is called.
	if st.StepIndex > parent.SwarmActiveStep {
		s.recordEventForConductor(st.ParentTaskID, subtaskID, "step_deferred", st.Title,
			fmt.Sprintf("dispatch of worker %q deferred — step %d is not yet active (active step is %d).",
				st.Title, st.StepIndex, parent.SwarmActiveStep))
		return fmt.Errorf("dispatch deferred: step %d is not yet active", st.StepIndex)
	}

	// Cap check.
	if s.activeWorkerCount(ctx, st.ParentTaskID) >= s.cfg.MaxConcurrentAgents {
		s.recordEventForConductor(st.ParentTaskID, subtaskID, "cap_deferred", st.Title,
			fmt.Sprintf("dispatch of worker %q deferred — swarm at concurrent cap (%d). It will spawn when a slot frees.",
				st.Title, s.cfg.MaxConcurrentAgents))
		return fmt.Errorf("dispatch deferred: swarm at concurrent cap (%d)", s.cfg.MaxConcurrentAgents)
	}

	workingDir := ""
	if parent.SwarmWorkingDir != nil {
		workingDir = *parent.SwarmWorkingDir
	}

	// Resolve brief: per-plan prompt or default template if empty.
	brief := st.Prompt
	if brief == "" {
		brief = s.defaultBrief(parent, st)
	}

	scope, _ := store.ParseScopeGlobs(st.ScopeGlobs)
	agentKind := st.AgentKind
	if agentKind == "" {
		agentKind = s.cfg.DefaultAgent
	}

	if err := s.agents.SpawnAgent(ctx, subtaskID, 0, 0, AgentSpawnOptions{
		Role:         st.Role,
		ParentTaskID: st.ParentTaskID,
		SubtaskID:    subtaskID,
		Scope:        scope,
		WorkingDir:   workingDir,
		AgentKind:    agentKind,
		Brief:        brief,
		StrictScope:  s.cfg.StrictScope,
	}); err != nil {
		return fmt.Errorf("spawn worker: %w", err)
	}

	if err := s.store.SetSubtaskDispatched(ctx, subtaskID); err != nil {
		return err
	}
	if sess, err := s.store.GetAgentSessionByTaskID(ctx, subtaskID); err == nil {
		_ = s.store.SetSubtaskBuilderAgent(ctx, subtaskID, &sess.ID)
	}

	// Surface any advisory scope conflicts.
	for _, conflict := range s.agents.LastSpawnConflicts() {
		payload := fmt.Sprintf("worker %q overlaps with active sibling %q (%s); %d file(s) in conflict",
			st.Title, conflict.SiblingTitle, conflict.SiblingSubtaskID, len(conflict.Files))
		s.recordEventForConductor(st.ParentTaskID, subtaskID, "scope_warning", st.Title, payload)
	}

	s.publishChanged(st.ParentTaskID, subtaskID, "dispatched")
	return nil
}

// Message sends text into a worker's tmux pane via send-keys.
func (s *swarmService) Message(ctx context.Context, subtaskID, text string) error {
	if _, err := s.store.GetSubtask(ctx, subtaskID); err != nil {
		return err
	}
	sess, err := s.store.GetAgentSessionByTaskID(ctx, subtaskID)
	if err != nil {
		return fmt.Errorf("worker %s is not running", subtaskID)
	}
	return s.tmuxSendKeysLine(sess.TmuxSession, text)
}

// MessageParent sends text into the conductor's tmux pane via send-keys.
func (s *swarmService) MessageParent(ctx context.Context, parentID, text string) error {
	sess, err := s.store.GetAgentSessionByTaskID(ctx, parentID)
	if err != nil {
		return fmt.Errorf("conductor for parent %s is not running", parentID)
	}
	return s.tmuxSendKeysLine(sess.TmuxSession, text)
}

// Broadcast sends text to every live worker in the swarm.
func (s *swarmService) Broadcast(ctx context.Context, parentID, text string) (int, error) {
	subs, err := s.store.ListSubtasksByParent(ctx, parentID)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, st := range subs {
		if !isLiveStatus(st.Status) {
			continue
		}
		sess, err := s.store.GetAgentSessionByTaskID(ctx, st.ID)
		if err != nil {
			continue
		}
		if err := s.tmuxSendKeysLine(sess.TmuxSession, text); err == nil {
			count++
		}
	}
	return count, nil
}

// Close ratifies a worker's completion (from `reporting`) or terminates it
// mid-flight (from `dispatched`/`in_progress`).
func (s *swarmService) Close(ctx context.Context, subtaskID string) error {
	st, err := s.store.GetSubtask(ctx, subtaskID)
	if err != nil {
		return err
	}
	_ = s.agents.KillAgent(ctx, subtaskID)
	newStatus := "done"
	if st.Status == "dispatched" || st.Status == "in_progress" {
		newStatus = "cancelled"
	}
	if err := s.store.UpdateSubtaskStatus(ctx, subtaskID, newStatus); err != nil {
		return err
	}
	s.publishChanged(st.ParentTaskID, subtaskID, newStatus)

	// All-idle check after a closure.
	s.maybeNotifyAllIdle(ctx, st.ParentTaskID)
	return nil
}

// Finish terminates the entire swarm: kills all live workers + the conductor,
// appends the summary to the parent task description.
// Finish closes out a swarm: every live worker is killed and the summary is
// appended to the parent task description. The conductor session is left
// alive so the user can still query it (`legato swarm status`, ask questions
// of the conductor, etc.) and confirm the work themselves. The user can
// terminate the conductor manually via the agents view (`K`) or
// `legato kill` when satisfied.
func (s *swarmService) Finish(ctx context.Context, parentID, summary string) error {
	subs, err := s.store.ListSubtasksByParent(ctx, parentID)
	if err != nil {
		return err
	}
	// Kill every live worker.
	for _, st := range subs {
		if isLiveStatus(st.Status) {
			_ = s.agents.KillAgent(ctx, st.ID)
			finalStatus := "done"
			if st.Status != "reporting" {
				finalStatus = "cancelled"
			}
			_ = s.store.UpdateSubtaskStatus(ctx, st.ID, finalStatus)
		}
	}

	// Append summary to parent task description.
	parent, err := s.store.GetTask(ctx, parentID)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("\n\n---\n## Swarm summary (%s)\n\n", time.Now().UTC().Format(time.RFC3339))
	parent.Description = parent.Description + header + summary
	parent.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.store.UpdateTask(ctx, *parent); err != nil {
		return err
	}

	// Notify the conductor that the swarm is complete and it's free to be
	// queried for confirmation. The conductor pane stays alive until the
	// user dismisses it.
	s.recordEventForConductor(parentID, "", "finished", "",
		"Swarm finished. All workers terminated, summary appended to the parent task. This conductor session remains active for any final questions or confirmation; close it manually when you're done.")

	s.publishChanged(parentID, "", "finished")
	return nil
}

// Progress records a worker progress report and forwards a debounced event
// to the conductor.
func (s *swarmService) Progress(ctx context.Context, subtaskID, text string) error {
	st, err := s.store.GetSubtask(ctx, subtaskID)
	if err != nil {
		return err
	}
	// First progress call transitions dispatched → in_progress.
	if st.Status == "dispatched" {
		_ = s.store.UpdateSubtaskStatus(ctx, subtaskID, "in_progress")
		s.publishChanged(st.ParentTaskID, subtaskID, "in_progress")
		st.Status = "in_progress"
	}
	s.scheduleProgressEvent(st.ParentTaskID, st.Title, subtaskID, text)
	return nil
}

// Question delivers a worker question to the conductor pane immediately.
func (s *swarmService) Question(ctx context.Context, subtaskID, text string) error {
	st, err := s.store.GetSubtask(ctx, subtaskID)
	if err != nil {
		return err
	}
	s.flushProgressEvent(subtaskID)
	s.recordEventForConductor(st.ParentTaskID, subtaskID, "question", st.Title, text)
	return nil
}

// Built marks a sub-task as `reporting` and notifies the conductor.
func (s *swarmService) Built(ctx context.Context, subtaskID string) error {
	st, err := s.store.GetSubtask(ctx, subtaskID)
	if err != nil {
		return err
	}
	if st.Status != "in_progress" && st.Status != "dispatched" {
		return fmt.Errorf("sub-task %s is %s, cannot mark built", subtaskID, st.Status)
	}
	if err := s.store.UpdateSubtaskStatus(ctx, subtaskID, "reporting"); err != nil {
		return err
	}
	s.flushProgressEvent(subtaskID)
	payload := fmt.Sprintf(
		"worker %q (%s) marked itself built. Inspect the worker's diff, then run `legato swarm close %s` to ratify completion, or `legato swarm message %s \"...\"` to send corrections.",
		st.Title, subtaskID, subtaskID, subtaskID,
	)
	s.recordEventForConductor(st.ParentTaskID, subtaskID, "built", st.Title, payload)
	s.publishChanged(st.ParentTaskID, subtaskID, "reporting")
	s.maybeNotifyAllIdle(ctx, st.ParentTaskID)
	return nil
}

// HandleAgentDied is called by the event-bus subscriber when an agent dies.
// For workers in a non-terminal state, transitions to `cancelled` and notifies
// the conductor. For the conductor itself, no automatic action — the user
// (or `legato swarm finish`) cleans up.
func (s *swarmService) HandleAgentDied(ctx context.Context, parentTaskID, subtaskID, role string) {
	if subtaskID == "" || role == "conductor" {
		return
	}
	st, err := s.store.GetSubtask(ctx, subtaskID)
	if err != nil {
		return
	}
	if st.Status == "done" || st.Status == "cancelled" {
		return
	}
	_ = s.store.UpdateSubtaskStatus(ctx, subtaskID, "cancelled")
	s.flushProgressEvent(subtaskID)
	payload := fmt.Sprintf("worker %q (%s) died unexpectedly; sub-task transitioned to `cancelled`.", st.Title, subtaskID)
	s.recordEventForConductor(parentTaskID, subtaskID, "died", st.Title, payload)
	s.publishChanged(parentTaskID, subtaskID, "cancelled")
	s.maybeNotifyAllIdle(ctx, parentTaskID)
}

// StartEventLoop subscribes to EventAgentDied and dispatches to HandleAgentDied.
func (s *swarmService) StartEventLoop(ctx context.Context) func() {
	if s.bus == nil {
		return func() {}
	}
	ch := s.bus.Subscribe(events.EventAgentDied)
	stopped := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopped:
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if p, ok := ev.Payload.(events.AgentDiedPayload); ok {
					s.HandleAgentDied(ctx, p.ParentTaskID, p.SubtaskID, p.Role)
				}
			}
		}
	}()
	return func() {
		close(stopped)
		s.bus.Unsubscribe(ch)
	}
}

// activeWorkerCount returns the number of live workers in a swarm (counts
// dispatched, in_progress, reporting).
func (s *swarmService) activeWorkerCount(ctx context.Context, parentID string) int {
	subs, err := s.store.ListSubtasksByParent(ctx, parentID)
	if err != nil {
		return 0
	}
	count := 0
	for _, st := range subs {
		if isLiveStatus(st.Status) {
			count++
		}
	}
	return count
}

func isLiveStatus(s string) bool {
	return s == "dispatched" || s == "in_progress" || s == "reporting"
}

func (s *swarmService) defaultBrief(parent *store.Task, st *store.Subtask) string {
	scope, _ := store.ParseScopeGlobs(st.ScopeGlobs)
	scopeLine := "(no declared scope)"
	if len(scope) > 0 {
		scopeLine = strings.Join(scope, ", ")
	}
	return fmt.Sprintf(
		"## Sub-task: %s\n\nParent task: %s — %s\n\n## Parent task description\n\n%s\n\n## Your scope\n\n%s\n\n## When done\n\nRun: legato swarm built $LEGATO_SUBTASK_ID",
		st.Title, parent.ID, parent.Title, parent.Description, scopeLine,
	)
}

// recordEventForConductor writes an event to the swarm_events inbox and sends
// a short, plain-text pointer to the conductor's pane via send-keys. The
// conductor's role prompt instructs it to fetch the full event content via
// `legato swarm inbox <parent-id>`.
//
// This pattern avoids embedding multi-line or quoted content in send-keys
// payloads, which would otherwise need base64 wrapping and could trigger
// safety filters in some AI tools.
func (s *swarmService) recordEventForConductor(parentTaskID, subtaskID, kind, workerTitle, payload string) {
	if parentTaskID == "" {
		return
	}
	row := store.SwarmEvent{
		ParentTaskID: parentTaskID,
		Kind:         kind,
		WorkerTitle:  workerTitle,
		Payload:      payload,
	}
	if subtaskID != "" {
		row.SubtaskID = &subtaskID
	}
	id, err := s.store.InsertSwarmEvent(context.Background(), row)
	if err != nil {
		return
	}

	sess, err := s.store.GetAgentSessionByTaskID(context.Background(), parentTaskID)
	if err != nil || sess == nil {
		return
	}
	pointer := fmt.Sprintf(
		"[legato] new swarm event #%d (%s) — run `legato swarm inbox %s` to read.",
		id, kind, parentTaskID,
	)

	// Serialize delivery per conductor and insert a small gap after each
	// send so claude's input handler can process one Enter before the next
	// payload arrives. Without this, back-to-back events (e.g. built +
	// all_idle from a single Built call) race and one Enter gets dropped.
	mu := s.conductorLock(parentTaskID)
	mu.Lock()
	defer mu.Unlock()
	_ = s.tmuxSendKeysLine(sess.TmuxSession, pointer)
	time.Sleep(conductorEventGap)
}

// conductorEventGap is the minimum interval between consecutive send-keys
// deliveries to a single conductor. Tuned empirically to give claude code's
// input handler time to absorb one turn before the next arrives.
const conductorEventGap = 250 * time.Millisecond

// tmuxSendKeysLine reaches the agent service's TmuxManager and sends a single
// line of text + Enter. Used for short notifications that don't need
// base64 wrapping.
func (s *swarmService) tmuxSendKeysLine(session, text string) error {
	tmuxer, ok := s.agents.(interface {
		Tmux() TmuxManager
	})
	if !ok {
		return fmt.Errorf("agent service does not expose tmux")
	}
	return tmuxer.Tmux().SendKeysLine(session, text)
}

// scheduleProgressEvent debounces multiple progress reports from the same
// worker within a 1s window. The most recent text wins.
func (s *swarmService) scheduleProgressEvent(parentTaskID, workerTitle, subtaskID, text string) {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()

	// Capture the parent + title so flushProgressEvent can format the
	// notification without re-querying the DB. Done under the same lock
	// that protects pendingProgress, so flush sees a consistent snapshot.
	s.pendingMeta[subtaskID] = progressMeta{ParentTaskID: parentTaskID, WorkerTitle: workerTitle}

	if entry, ok := s.pendingProgress[subtaskID]; ok {
		entry.text = text
		entry.timer.Reset(progressDebounceWindow)
		return
	}
	entry := &pendingProgressEntry{text: text}
	entry.timer = time.AfterFunc(progressDebounceWindow, func() {
		s.flushProgressEvent(subtaskID)
	})
	s.pendingProgress[subtaskID] = entry
}

type progressMeta struct {
	ParentTaskID string
	WorkerTitle  string
}

// flushProgressEvent immediately emits any pending progress event for the
// given sub-task, cancelling its timer. Safe to call when no event is pending.
func (s *swarmService) flushProgressEvent(subtaskID string) {
	s.debounceMu.Lock()
	entry, ok := s.pendingProgress[subtaskID]
	if !ok {
		s.debounceMu.Unlock()
		return
	}
	entry.timer.Stop()
	delete(s.pendingProgress, subtaskID)
	meta := s.pendingMeta[subtaskID]
	delete(s.pendingMeta, subtaskID)
	text := entry.text
	s.debounceMu.Unlock()

	if meta.ParentTaskID == "" {
		return
	}
	s.recordEventForConductor(meta.ParentTaskID, subtaskID, "progress", meta.WorkerTitle, text)
}

// maybeNotifyAllIdle delivers an all-idle notification when every sub-task
// of the parent is in a non-active state (queued, reporting, done, cancelled).
// Fires whenever the swarm has no active workers (`dispatched` or
// `in_progress`) but has at least one sub-task overall. Catches both the
// "workers built, awaiting conductor decision" case and the "all sub-tasks
// terminal, ready to finish" case. Per-conductor mutex + 250ms gap in
// recordEventForConductor prevents spam when called from multiple paths.
//
// When the current active step is terminal and there are more steps, the
// notification includes a `step_completed` indicator so the conductor knows it
// can approve advancement to the next step.
func (s *swarmService) maybeNotifyAllIdle(ctx context.Context, parentID string) {
	subs, err := s.store.ListSubtasksByParent(ctx, parentID)
	if err != nil || len(subs) == 0 {
		return
	}
	parent, err := s.store.GetTask(ctx, parentID)
	if err != nil {
		return
	}
	for _, st := range subs {
		if st.Status == "dispatched" || st.Status == "in_progress" {
			return
		}
	}

	// Check whether the current active step is terminal.
	currentStepDone := true
	var maxStep int
	for _, st := range subs {
		if st.StepIndex > maxStep {
			maxStep = st.StepIndex
		}
		if st.StepIndex == parent.SwarmActiveStep {
			if st.Status != "done" && st.Status != "cancelled" {
				currentStepDone = false
			}
		}
	}
	if currentStepDone && maxStep > parent.SwarmActiveStep {
		s.recordEventForConductor(parentID, "", "all_idle", "",
			fmt.Sprintf("Step %d is complete. Dispatchable sub-tasks in the next step are gated until you approve advancement. Run `legato swarm next-step %s` to proceed.",
				parent.SwarmActiveStep, parentID))
		return
	}

	s.recordEventForConductor(parentID, "", "all_idle", "",
		"No workers in this swarm are active. Decide: dispatch more queued sub-tasks, ask the user, or call `legato swarm finish` if the parent goal is met.")
}

// publishChanged emits an EventSwarmChanged event for downstream UI refresh.
func (s *swarmService) publishChanged(parentID, subtaskID, newStatus string) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(events.Event{
		Type: events.EventSwarmChanged,
		Payload: events.SwarmChangedPayload{
			ParentTaskID: parentID,
			SubtaskID:    subtaskID,
			NewStatus:    newStatus,
		},
	})
}

// AgentDiedPublisher is a small adapter that lets the agent service publish
// EventAgentDied without importing the events package directly.
type AgentDiedPublisher struct {
	Bus *events.Bus
}

func (p AgentDiedPublisher) PublishAgentDied(taskID, parentTaskID, subtaskID, role string) {
	if p.Bus == nil {
		return
	}
	p.Bus.Publish(events.Event{
		Type: events.EventAgentDied,
		Payload: events.AgentDiedPayload{
			TaskID:       taskID,
			ParentTaskID: parentTaskID,
			SubtaskID:    subtaskID,
			Role:         role,
		},
	})
}

// progressDebounceWindow is the per-worker progress collapse window.
const progressDebounceWindow = 1 * time.Second
