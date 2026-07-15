package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/attachments"
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
	// ConductorAgent overrides DefaultAgent for the conductor only. Empty
	// falls back to DefaultAgent so existing configs keep working.
	ConductorAgent string
	// ConductorTier names a tier (configured under the resolved conductor
	// agent's adapter) at which the conductor itself should launch. Empty
	// falls back to the adapter's base launch_args.
	ConductorTier string
	// TierCatalog is the human-readable map of adapter → tier name →
	// description. Used to generate the "Available tiers" section appended
	// to the conductor's brief so it knows which tier names exist and how
	// each is intended to be used. Adapters with no configured tiers are
	// omitted; an empty catalog skips the brief section entirely.
	TierCatalog map[string]map[string]string
	// ValidateOptions holds the registry used by ApplyApprovedPlan to
	// re-validate plans submitted via the TUI/web edit-and-approve flow
	// (which bypasses the CLI's propose-plan validation pass). Wire from
	// main.go alongside TierCatalog. Zero value disables the in-service
	// check, leaving CLI-side validation as the only guard.
	ValidateOptions swarm.ValidateOptions
}

// SwarmSnapshot is a lightweight in-memory view of swarm progress suitable
// for fast status-line rendering.
type SwarmSnapshot struct {
	Total         int
	Done          int
	Cancelled     int
	Active        int
	LastEventKind string
	LastEventAt   time.Time
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
	LatestSnapshot(parentID string) *SwarmSnapshot
	FetchInbox(ctx context.Context, parentID string) ([]InboxEntry, error)
	PeekInbox(ctx context.Context, parentID string) ([]InboxEntry, error)
	LoadPlan(path string) (*SwarmPlan, error)

	// Conductor verbs.
	StartSwarm(ctx context.Context, parentID, workingDir string) error
	CreateAdhocSwarm(ctx context.Context, taskID, goal, workingDir string) error
	ApplyApprovedPlan(ctx context.Context, plan *swarm.Plan) error
	CancelSwarm(ctx context.Context, parentID string) error
	ExtendApprovedPlan(ctx context.Context, plan *swarm.Plan) error
	Dispatch(ctx context.Context, subtaskID string) error
	NextStep(ctx context.Context, parentID string) error
	Message(ctx context.Context, subtaskID, text string, urgent bool) error
	MessageParent(ctx context.Context, parentID, text string, urgent bool) error
	Broadcast(ctx context.Context, parentID, text string, urgent bool) (int, error)
	Close(ctx context.Context, subtaskID string) error
	Finish(ctx context.Context, parentID, summary string) error

	// Worker verbs.
	Progress(ctx context.Context, subtaskID, text string) error
	Question(ctx context.Context, subtaskID, text string) error
	Built(ctx context.Context, subtaskID string) error

	// Pending-plan persistence (survives server restarts / browser tab suspends).
	InsertPendingPlan(ctx context.Context, parentTaskID, planPath, replySocket string) error
	GetPendingPlan(ctx context.Context, parentTaskID string) (*store.PendingPlanEntry, error)
	ListAllPendingPlans(ctx context.Context) ([]store.PendingPlanEntry, error)
	DeletePendingPlan(ctx context.Context, parentTaskID string) error

	// Lifecycle.
	HandleAgentDied(ctx context.Context, parentTaskID, subtaskID, role string)
	StartEventLoop(ctx context.Context) func()
}

// SwarmPlan is a service-layer DTO mirroring an engine swarm plan. Lets TUI
// callers render proposed plans without importing the engine package.
type SwarmPlan struct {
	Header   SwarmPlanHeader    `json:"header"`
	Subtasks []SwarmPlanSubtask `json:"subtasks"`
}

// SwarmPlanHeader carries swarm-level fields from a plan.
type SwarmPlanHeader struct {
	ParentTaskID string `json:"parent_task_id"`
	WorkingDir   string `json:"working_dir"`
	Summary      string `json:"summary"`
}

// SwarmPlanSubtask is a service-layer view of one plan entry.
type SwarmPlanSubtask struct {
	Title  string   `json:"title"`
	Role   string   `json:"role,omitempty"`
	Agent  string   `json:"agent,omitempty"`
	Tier   string   `json:"tier,omitempty"`
	Scope  []string `json:"scope,omitempty"`
	Prompt string   `json:"prompt,omitempty"`
}

// swarmService is the concrete implementation of SwarmService.
type swarmService struct {
	store       *store.Store
	agents      AgentService
	bus         *events.Bus
	cfg         SwarmConfig
	repoRoot    string
	attachments *attachments.Cache

	// Per-worker progress debouncer state. Keyed by subtask_id.
	debounceMu      sync.Mutex
	pendingProgress map[string]*pendingProgressEntry
	pendingMeta     map[string]progressMeta

	// Per-parent serializer for conductor-bound send-keys delivery. Without
	// this, two events (e.g. built + all_idle) can fire in microseconds and
	// claude's input handler may lose the Enter on the first.
	conductorLocksMu sync.Mutex
	conductorLocks   map[string]*sync.Mutex

	// Per-parent in-memory snapshot cache. Updated incrementally on
	// mutation paths so that LatestSnapshot is sub-millisecond.
	snapshotMu    sync.RWMutex
	snapshotCache map[string]*SwarmSnapshot
}

type pendingProgressEntry struct {
	timer *time.Timer
	text  string
}

// NewSwarmService creates a SwarmService.
func NewSwarmService(s *store.Store, agents AgentService, bus *events.Bus, cfg SwarmConfig, repoRoot string, attachmentCache ...*attachments.Cache) SwarmService {
	if cfg.MaxConcurrentAgents <= 0 {
		cfg.MaxConcurrentAgents = 4
	}
	if cfg.MaxSubtasksPerPlan <= 0 {
		cfg.MaxSubtasksPerPlan = 10
	}
	if cfg.MaxStepsPerPlan <= 0 {
		cfg.MaxStepsPerPlan = 10
	}
	cache := attachments.NewCache(attachments.DefaultRoot(), 0)
	if len(attachmentCache) > 0 && attachmentCache[0] != nil {
		cache = attachmentCache[0]
	}
	return &swarmService{
		store:           s,
		attachments:     cache,
		agents:          agents,
		bus:             bus,
		cfg:             cfg,
		repoRoot:        repoRoot,
		pendingProgress: make(map[string]*pendingProgressEntry),
		pendingMeta:     make(map[string]progressMeta),
		conductorLocks:  make(map[string]*sync.Mutex),
		snapshotCache:   make(map[string]*SwarmSnapshot),
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
				Tier:   st.Tier,
				Scope:  append([]string(nil), st.Scope...),
				Prompt: st.Prompt,
			})
		}
	}
	return out, nil
}

// InsertPendingPlan delegates to the store's InsertPendingPlan.
func (s *swarmService) InsertPendingPlan(ctx context.Context, parentTaskID, planPath, replySocket string) error {
	return s.store.InsertPendingPlan(ctx, parentTaskID, planPath, replySocket)
}

// GetPendingPlan delegates to the store's GetPendingPlan.
func (s *swarmService) GetPendingPlan(ctx context.Context, parentTaskID string) (*store.PendingPlanEntry, error) {
	return s.store.GetPendingPlan(ctx, parentTaskID)
}

// ListAllPendingPlans delegates to the store's ListAllPendingPlans.
func (s *swarmService) ListAllPendingPlans(ctx context.Context) ([]store.PendingPlanEntry, error) {
	return s.store.ListAllPendingPlans(ctx)
}

// DeletePendingPlan delegates to the store's DeletePendingPlan.
func (s *swarmService) DeletePendingPlan(ctx context.Context, parentTaskID string) error {
	return s.store.DeletePendingPlan(ctx, parentTaskID)
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

// LatestSnapshot returns the cheap in-memory snapshot for a parent task.
// nil means no swarm data is cached (parent not started). On a cold cache we
// rebuild from the DB once so Total/Done counters are correct after a server
// restart; subsequent reads stay in-memory.
func (s *swarmService) LatestSnapshot(parentID string) *SwarmSnapshot {
	if parentID == "" {
		return nil
	}
	s.snapshotMu.RLock()
	snap := s.snapshotCache[parentID]
	s.snapshotMu.RUnlock()
	if snap != nil {
		return snap
	}
	s.rebuildSnapshot(context.Background(), parentID)
	s.snapshotMu.RLock()
	defer s.snapshotMu.RUnlock()
	return s.snapshotCache[parentID]
}

// bumpSnapshot mutates the per-parent cache to reflect a single sub-task
// status change. Called from the hot paths so it must not block.
func (s *swarmService) bumpSnapshot(parentID string, oldStatus, newStatus string) {
	if parentID == "" {
		return
	}
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()

	snap := s.snapshotCache[parentID]
	if snap == nil {
		snap = &SwarmSnapshot{}
	}

	// Decrement old if it existed.
	switch oldStatus {
	case "done":
		snap.Done--
	case "cancelled":
		snap.Cancelled--
	case "dispatched", "in_progress", "reporting":
		snap.Active--
	}

	// Increment new.
	switch newStatus {
	case "done":
		snap.Done++
	case "cancelled":
		snap.Cancelled++
	case "dispatched", "in_progress", "reporting":
		snap.Active++
	case "queued":
		// Total stays the same; it was already counted.
	}

	s.snapshotCache[parentID] = snap
}

// setSnapshotEvent updates the cached last-event metadata for a parent.
func (s *swarmService) setSnapshotEvent(parentID, kind string) {
	if parentID == "" {
		return
	}
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()

	snap := s.snapshotCache[parentID]
	if snap == nil {
		snap = &SwarmSnapshot{}
	}
	snap.LastEventKind = kind
	snap.LastEventAt = time.Now()
	s.snapshotCache[parentID] = snap
}

// rebuildSnapshot recalculates the per-parent cache from DB. Used when we
// don't know the prior state (plan applied, finish, etc.).
func (s *swarmService) rebuildSnapshot(ctx context.Context, parentID string) {
	if parentID == "" {
		return
	}
	var snap SwarmSnapshot
	subs, err := s.store.ListSubtasksByParent(ctx, parentID)
	if err != nil {
		return
	}
	for _, st := range subs {
		snap.Total++
		switch st.Status {
		case "done":
			snap.Done++
		case "cancelled":
			snap.Cancelled++
		case "dispatched", "in_progress", "reporting":
			snap.Active++
		}
	}
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	s.snapshotCache[parentID] = &snap
}

// StartSwarm spawns the conductor for a parent task. Refuses if the parent
// already has a running agent, non-terminal sub-tasks, or a non-nil working dir.
// Persists the working dir on the parent task. The conductor's brief is the
// parent task title + description + working-dir framing.
func (s *swarmService) StartSwarm(ctx context.Context, parentID, workingDir string) error {
	parent, err := s.store.GetTask(ctx, parentID)
	if err != nil {
		return fmt.Errorf("parent task %s: %w", parentID, err)
	}

	// Refuse double-spawn.
	if existing, err := s.store.GetAgentSessionByTaskID(ctx, parentID); err == nil && existing.Status == "running" {
		return fmt.Errorf("parent task %s already has a running agent — kill it before starting a swarm", parentID)
	}

	// Refuse if there are leftover sub-tasks in non-terminal states.
	subs, err := s.store.ListSubtasksByParent(ctx, parentID)
	if err != nil {
		return fmt.Errorf("list subtasks: %w", err)
	}
	for _, st := range subs {
		if st.Status == "queued" || st.Status == "dispatched" || st.Status == "in_progress" || st.Status == "reporting" {
			return fmt.Errorf("parent task %s has leftover swarm sub-tasks — cancel the existing swarm first", parentID)
		}
	}
	// Refuse if swarm_working_dir is already set (even without subtasks).
	if parent.SwarmWorkingDir != nil {
		return fmt.Errorf("parent task %s already has a swarm working directory — cancel the existing swarm first", parentID)
	}

	if err := s.store.SetTaskSwarmWorkingDir(ctx, parentID, &workingDir); err != nil {
		return fmt.Errorf("persist working dir: %w", err)
	}

	brief := fmt.Sprintf(
		"You are the swarm conductor for task **%s — %s**.\n\nWorking directory: %s\n\n## Parent task description\n\n%s",
		parent.ID, parent.Title, workingDir, parent.Description,
	) + s.attachmentBrief(parent.ID)

	if catalog := formatTierCatalog(s.cfg.TierCatalog); catalog != "" {
		brief = brief + "\n\n" + catalog
	}

	conductorAgent := s.cfg.ConductorAgent
	if conductorAgent == "" {
		conductorAgent = s.cfg.DefaultAgent
	}

	if err := s.agents.SpawnAgent(ctx, parentID, 0, 0, AgentSpawnOptions{
		Role:         "conductor",
		ParentTaskID: parentID,
		WorkingDir:   workingDir,
		AgentKind:    conductorAgent,
		Tier:         s.cfg.ConductorTier,
		Brief:        brief,
	}); err != nil {
		// Roll back the working dir on failure.
		_ = s.store.SetTaskSwarmWorkingDir(ctx, parentID, nil)
		return fmt.Errorf("spawn conductor: %w", err)
	}

	s.publishChanged(parentID, "", "started")
	return nil
}

// CreateAdhocSwarm promotes an existing running agent session into a swarm
// conductor without spawning a replacement tmux session. The taskID is the
// current session's backing task; it may be an ephemeral task hidden from the
// board or a normal task-backed agent session.
func (s *swarmService) CreateAdhocSwarm(ctx context.Context, taskID, goal, workingDir string) error {
	taskID = strings.TrimSpace(taskID)
	goal = strings.TrimSpace(goal)
	workingDir = strings.TrimSpace(workingDir)
	if taskID == "" {
		return fmt.Errorf("taskID is required")
	}
	if goal == "" {
		return fmt.Errorf("goal is required")
	}
	if workingDir == "" {
		return fmt.Errorf("working directory is required")
	}
	info, err := os.Stat(workingDir)
	if err != nil {
		return fmt.Errorf("working directory not accessible: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("working directory is not a directory")
	}

	sess, err := s.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("current agent session for %s is not running", taskID)
	}
	if sess.Role != "" {
		if sess.Role == "conductor" {
			return fmt.Errorf("agent session %s is already a swarm conductor", taskID)
		}
		return fmt.Errorf("agent session %s is already a swarm %s", taskID, sess.Role)
	}
	parent, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("backing task %s: %w", taskID, err)
	}
	if parent.SwarmWorkingDir != nil {
		return fmt.Errorf("task %s already has a swarm working directory — cancel the existing swarm first", taskID)
	}
	subs, err := s.store.ListSubtasksByParent(ctx, taskID)
	if err != nil {
		return fmt.Errorf("list subtasks: %w", err)
	}
	for _, st := range subs {
		if st.Status == "queued" || st.Status == "dispatched" || st.Status == "in_progress" || st.Status == "reporting" {
			return fmt.Errorf("task %s has leftover swarm sub-tasks — cancel the existing swarm first", taskID)
		}
	}

	if parent.Ephemeral {
		parent.Title = "Adhoc swarm: " + goal
		parent.Description = goal
		parent.DescriptionMD = goal
		parent.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.store.UpdateTask(ctx, *parent); err != nil {
			return fmt.Errorf("update adhoc backing task: %w", err)
		}
	}

	if err := s.store.SetTaskSwarmWorkingDir(ctx, taskID, &workingDir); err != nil {
		return fmt.Errorf("persist working dir: %w", err)
	}
	if err := s.store.SetParentActiveStep(ctx, taskID, 0); err != nil {
		_ = s.store.SetTaskSwarmWorkingDir(ctx, taskID, nil)
		return fmt.Errorf("reset active step: %w", err)
	}
	if err := s.store.UpdateAgentSessionSwarmRole(ctx, taskID, "conductor", taskID, nil); err != nil {
		_ = s.store.SetTaskSwarmWorkingDir(ctx, taskID, nil)
		return fmt.Errorf("promote session: %w", err)
	}

	brief := fmt.Sprintf(
		"You are the swarm conductor for an adhoc request.\n\nParent task ID: %s\nWorking directory: %s\n\n## User request\n\n%s",
		taskID, workingDir, goal,
	)
	if catalog := formatTierCatalog(s.cfg.TierCatalog); catalog != "" {
		brief = brief + "\n\n" + catalog
	}

	agentKind := sess.AgentKind
	if agentKind == "" {
		agentKind = s.cfg.ConductorAgent
	}
	if agentKind == "" {
		agentKind = s.cfg.DefaultAgent
	}
	adapter := s.agents.AdapterFor(agentKind)
	rolePrompt := rolePromptForAdapter(adapter, "conductor")
	rolePath, briefPath, err := writeAgentPromptFiles(taskID, rolePrompt, brief)
	if err != nil {
		return fmt.Errorf("write conductor prompt files: %w", err)
	}

	tmuxer, ok := s.agents.(interface {
		Tmux() TmuxManager
	})
	if !ok {
		return fmt.Errorf("agent service does not expose tmux")
	}
	env := map[string]string{
		"LEGATO_AGENT_ROLE":     "conductor",
		"LEGATO_PARENT_TASK_ID": taskID,
	}
	if rolePath != "" {
		env["LEGATO_ROLE_PROMPT_FILE"] = rolePath
	}
	if briefPath != "" {
		env["LEGATO_BRIEF_FILE"] = briefPath
	}
	for k, v := range env {
		if err := tmuxer.Tmux().SetEnv(sess.TmuxSession, k, v); err != nil {
			return fmt.Errorf("set tmux env %s: %w", k, err)
		}
	}
	kickoff := fmt.Sprintf("You are now the Legato swarm conductor for this adhoc request. Parent task ID: %s. Read the conductor role prompt at %s and the full brief at %s, then create, validate, and propose a swarm plan.", taskID, rolePath, briefPath)
	if rolePath == "" {
		kickoff = fmt.Sprintf("You are now the Legato swarm conductor for this adhoc request. Parent task ID: %s. Read the full brief at %s, then create, validate, and propose a swarm plan.", taskID, briefPath)
	}
	if err := tmuxer.Tmux().SendKeysLine(sess.TmuxSession, kickoff); err != nil {
		return fmt.Errorf("send conductor kickoff: %w", err)
	}

	s.rebuildSnapshot(ctx, taskID)
	s.publishChanged(taskID, "", "started")
	return nil
}

// ApplyApprovedPlan persists sub-tasks from a (post-approval) plan. Idempotent
// per (parent_task_id, title) combination is NOT guaranteed — callers should
// only call this once per approved plan.
//
// Re-runs ValidatePlan defensively: the CLI propose-plan path validates an
// edited plan before reaching here, but the TUI/web edit-and-approve path
// calls this method directly with a freshly-loaded plan. Persisting an
// invalid plan would silently bypass tier/scope checks.
func (s *swarmService) ApplyApprovedPlan(ctx context.Context, plan *swarm.Plan) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	if err := swarm.ValidatePlan(plan, s.cfg.ValidateOptions); err != nil {
		return fmt.Errorf("validate plan: %w", err)
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
				Tier:         spec.Tier,
				Status:       "queued",
				StepIndex:    si,
			}
			if err := s.store.CreateSubtask(ctx, st); err != nil {
				return fmt.Errorf("create sub-task %q: %w", spec.Title, err)
			}
		}
	}
	s.rebuildSnapshot(ctx, plan.Swarm.ParentTaskID)
	s.publishChanged(plan.Swarm.ParentTaskID, "", "plan_applied")
	return nil
}

// CancelSwarm terminates an entire swarm: kills the conductor and every live
// worker, transitions non-terminal sub-tasks to cancelled, deletes all
// sub-tasks, clears the parent's working dir and active step, cleans up runtime
// files, and publishes a `cancelled` event. Idempotent: safe to call when no
// swarm exists.
func (s *swarmService) CancelSwarm(ctx context.Context, parentID string) error {
	// Kill conductor if alive.
	_ = s.agents.KillAgent(ctx, parentID)

	// Kill workers and delete sub-tasks.
	subs, err := s.store.ListSubtasksByParent(ctx, parentID)
	if err != nil {
		return fmt.Errorf("list subtasks: %w", err)
	}
	for _, st := range subs {
		_ = s.agents.KillAgent(ctx, st.ID)
		if st.Status != "done" && st.Status != "cancelled" {
			_ = s.store.UpdateSubtaskStatus(ctx, st.ID, "cancelled")
		}
		_ = s.store.DeleteSubtask(ctx, st.ID)
	}

	// Clear parent swarm fields.
	if err := s.store.SetTaskSwarmWorkingDir(ctx, parentID, nil); err != nil {
		return fmt.Errorf("clear working dir: %w", err)
	}
	if err := s.store.SetParentActiveStep(ctx, parentID, 0); err != nil {
		return fmt.Errorf("reset active step: %w", err)
	}

	// Clean up runtime files.
	s.cleanupRuntimeFiles(parentID, subs)

	// Drop in-memory snapshot cache.
	s.snapshotMu.Lock()
	delete(s.snapshotCache, parentID)
	s.snapshotMu.Unlock()

	// Drain any pending plan.
	_ = s.store.DeletePendingPlan(ctx, parentID)

	s.publishChanged(parentID, "", "cancelled")
	return nil
}

// ExtendApprovedPlan appends the steps of a new plan to an existing swarm. The
// new sub-tasks receive step indices starting after the current max so they
// queue behind the existing material.
func (s *swarmService) ExtendApprovedPlan(ctx context.Context, plan *swarm.Plan) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	parent, err := s.store.GetTask(ctx, plan.Swarm.ParentTaskID)
	if err != nil {
		return fmt.Errorf("parent task %s: %w", plan.Swarm.ParentTaskID, err)
	}
	if parent.SwarmWorkingDir == nil {
		return fmt.Errorf("parent task %s has no active swarm", plan.Swarm.ParentTaskID)
	}

	// Validate with inherited working_dir allowed.
	validateOpts := s.cfg.ValidateOptions
	validateOpts.AllowMissingWorkingDir = true
	if err := swarm.ValidatePlan(plan, validateOpts); err != nil {
		return fmt.Errorf("validate plan: %w", err)
	}

	// Compute step offset.
	maxStep, err := s.store.GetMaxStepIndex(ctx, plan.Swarm.ParentTaskID)
	if err != nil {
		return fmt.Errorf("get max step index: %w", err)
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
				Tier:         spec.Tier,
				Status:       "queued",
				StepIndex:    maxStep + 1 + si,
			}
			if err := s.store.CreateSubtask(ctx, st); err != nil {
				return fmt.Errorf("create sub-task %q: %w", spec.Title, err)
			}
		}
	}
	s.rebuildSnapshot(ctx, plan.Swarm.ParentTaskID)
	s.publishChanged(plan.Swarm.ParentTaskID, "", "plan_extended")
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
	} else {
		brief += s.attachmentBrief(parent.ID)
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
		Tier:         st.Tier,
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
		s.setSnapshotEvent(st.ParentTaskID, "scope_warning")
	}

	s.bumpSnapshot(st.ParentTaskID, "queued", "dispatched")
	s.publishChanged(st.ParentTaskID, subtaskID, "dispatched")
	return nil
}

// Message sends text into a worker's tmux pane via send-keys. When urgent is
// true the adapter's interrupt keys are sent first (e.g. Escape) to abort the
// agent's current turn before the message is delivered.
func (s *swarmService) Message(ctx context.Context, subtaskID, text string, urgent bool) error {
	st, err := s.store.GetSubtask(ctx, subtaskID)
	if err != nil {
		return err
	}
	sess, err := s.store.GetAgentSessionByTaskID(ctx, subtaskID)
	if err != nil {
		return fmt.Errorf("worker %s is not running", subtaskID)
	}
	if err := s.maybeInterrupt(sessionTarget{tmuxSession: sess.TmuxSession, agentKind: st.AgentKind}, urgent); err != nil {
		return err
	}
	return s.tmuxSendKeysLine(sess.TmuxSession, text)
}

// MessageParent sends text into the conductor's tmux pane via send-keys. When
// urgent is true the conductor adapter's interrupt keys are sent first.
func (s *swarmService) MessageParent(ctx context.Context, parentID, text string, urgent bool) error {
	sess, err := s.store.GetAgentSessionByTaskID(ctx, parentID)
	if err != nil {
		return fmt.Errorf("conductor for parent %s is not running", parentID)
	}
	if err := s.maybeInterrupt(sessionTarget{tmuxSession: sess.TmuxSession, agentKind: s.cfg.ConductorAgent}, urgent); err != nil {
		return err
	}
	return s.tmuxSendKeysLine(sess.TmuxSession, text)
}

// Broadcast sends text to every live worker in the swarm. When urgent is true
// each target's adapter interrupt keys are sent before the message.
func (s *swarmService) Broadcast(ctx context.Context, parentID, text string, urgent bool) (int, error) {
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
		if err := s.maybeInterrupt(sessionTarget{tmuxSession: sess.TmuxSession, agentKind: st.AgentKind}, urgent); err != nil {
			continue
		}
		if err := s.tmuxSendKeysLine(sess.TmuxSession, text); err == nil {
			count++
		}
	}
	return count, nil
}

// sessionTarget bundles the tmux session name and the adapter kind so that
// maybeInterrupt can resolve the correct adapter for interrupt keys.
type sessionTarget struct {
	tmuxSession string
	agentKind   string
}

// maybeInterrupt sends the adapter's interrupt keys when urgent is true. If the
// adapter doesn't implement InterruptAdapter the method is a no-op. Each key is
// sent with the standard send-keys inter-call gap.
func (s *swarmService) maybeInterrupt(target sessionTarget, urgent bool) error {
	if !urgent {
		return nil
	}
	agentKind := target.agentKind
	if agentKind == "" {
		agentKind = s.cfg.DefaultAgent
	}
	adapter := s.agents.AdapterFor(agentKind)
	if adapter == nil {
		return nil
	}
	ia, ok := adapter.(InterruptAdapter)
	if !ok {
		return nil
	}
	for _, key := range ia.InterruptKeys() {
		if err := s.tmuxSendKey(target.tmuxSession, key); err != nil {
			return err
		}
		time.Sleep(sendKeysInterCallGap)
	}
	return nil
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
	s.bumpSnapshot(st.ParentTaskID, st.Status, newStatus)
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

	s.cleanupRuntimeFiles(parentID, subs)

	s.rebuildSnapshot(ctx, parentID)

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
	s.setSnapshotEvent(parentID, "finished")

	s.publishChanged(parentID, "", "finished")
	return nil
}

// cleanupRuntimeFiles best-effort removes the ephemeral agent prompt dirs and
// plan files for a finished swarm. Logs on error but does not abort.
func (s *swarmService) cleanupRuntimeFiles(parentID string, subs []store.Subtask) {
	root, err := swarm.LegatoHome()
	if err != nil {
		return
	}

	// Remove agent dirs for parent + each subtask.
	for _, taskID := range append([]string{parentID}, subtaskIDsFrom(subs)...) {
		agentDir := filepath.Join(root, "agents", taskID)
		if err := os.RemoveAll(agentDir); err != nil {
			log.Printf("cleanupRuntimeFiles: remove agent dir %s: %v", agentDir, err)
		}
	}

	// Remove plan files named <parentID>-*.yaml from the plans dir.
	plansDir, err := swarm.PlansDir()
	if err != nil {
		log.Printf("cleanupRuntimeFiles: resolve plans dir: %v", err)
		return
	}
	matches, err := filepath.Glob(filepath.Join(plansDir, parentID+"-*.yaml"))
	if err != nil {
		log.Printf("cleanupRuntimeFiles: glob plans: %v", err)
		return
	}
	for _, p := range matches {
		if err := os.Remove(p); err != nil {
			log.Printf("cleanupRuntimeFiles: remove plan %s: %v", p, err)
		}
	}
}

func subtaskIDsFrom(subs []store.Subtask) []string {
	out := make([]string, 0, len(subs))
	for _, st := range subs {
		if st.ID != "" {
			out = append(out, st.ID)
		}
	}
	return out
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
		s.bumpSnapshot(st.ParentTaskID, "dispatched", "in_progress")
		s.publishChanged(st.ParentTaskID, subtaskID, "in_progress")
		st.Status = "in_progress"
	}
	s.scheduleProgressEvent(st.ParentTaskID, st.Title, subtaskID, text)
	s.setSnapshotEvent(st.ParentTaskID, "progress")
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
	s.setSnapshotEvent(st.ParentTaskID, "question")
	return nil
}

// Built marks a sub-task as `reporting` and notifies the conductor. When the
// sub-task is already `reporting` (e.g. after feedback via `swarm message`), the
// DB write is skipped but a fresh event is still emitted so the conductor knows
// the worker is re-reporting.
func (s *swarmService) Built(ctx context.Context, subtaskID string) error {
	st, err := s.store.GetSubtask(ctx, subtaskID)
	if err != nil {
		return err
	}
	if st.Status != "in_progress" && st.Status != "dispatched" && st.Status != "reporting" {
		return fmt.Errorf("sub-task %s is %s, cannot mark built", subtaskID, st.Status)
	}
	isReReport := st.Status == "reporting"
	if !isReReport {
		if err := s.store.UpdateSubtaskStatus(ctx, subtaskID, "reporting"); err != nil {
			return err
		}
	}
	s.flushProgressEvent(subtaskID)
	var payload string
	if isReReport {
		payload = fmt.Sprintf(
			"worker %q (%s) re-reported built after feedback. Inspect the worker's diff, then run `legato swarm close %s` to ratify completion, or `legato swarm message %s \"...\"` to send corrections.",
			st.Title, subtaskID, subtaskID, subtaskID,
		)
	} else {
		payload = fmt.Sprintf(
			"worker %q (%s) marked itself built. Inspect the worker's diff, then run `legato swarm close %s` to ratify completion, or `legato swarm message %s \"...\"` to send corrections.",
			st.Title, subtaskID, subtaskID, subtaskID,
		)
	}
	s.recordEventForConductor(st.ParentTaskID, subtaskID, "built", st.Title, payload)
	if !isReReport {
		s.bumpSnapshot(st.ParentTaskID, st.Status, "reporting")
	}
	s.setSnapshotEvent(st.ParentTaskID, "built")
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
	s.bumpSnapshot(parentTaskID, st.Status, "cancelled")
	s.setSnapshotEvent(parentTaskID, "died")
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
	) + s.attachmentBrief(parent.ID)
}

func (s *swarmService) attachmentBrief(taskID string) string {
	items, err := s.attachments.List(taskID)
	if err != nil || len(items) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString("\n\n## Local attachments\n\n")
	for _, item := range items {
		fmt.Fprintf(&out, "- %s: `%s`\n", item.Filename, item.Path)
	}
	return out.String()
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

// sendKeysInterCallGap is the pause between interrupt key and the subsequent
// text+Enter in an urgent send. Mirrors the value in tmux.Manager to avoid
// cross-package coupling for a timing constant.
const sendKeysInterCallGap = 75 * time.Millisecond

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

// tmuxSendKey reaches the agent service's TmuxManager and sends a single named
// key (e.g. "Escape", "Enter"). Mirrors tmuxSendKeysLine; used for interrupt
// keys before urgent messages.
func (s *swarmService) tmuxSendKey(session, key string) error {
	tmuxer, ok := s.agents.(interface {
		Tmux() TmuxManager
	})
	if !ok {
		return fmt.Errorf("agent service does not expose tmux")
	}
	return tmuxer.Tmux().SendKey(session, key)
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
