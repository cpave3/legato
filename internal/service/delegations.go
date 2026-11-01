package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// Delegation modes and routes. See openspec/changes/argus-integration § D4.
const (
	DelegationFix     = "fix"
	DelegationFindOut = "find_out"

	RouteMessage = "message"
	RouteResume  = "resume"
	RouteStart   = "start"
)

// DelegationError is a delegation refused or failed, with the code the
// integration routes answer with.
type DelegationError struct {
	Code    string
	Message string
}

func (e *DelegationError) Error() string { return e.Message }

func delegationErr(code, format string, args ...any) *DelegationError {
	return &DelegationError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// DelegationTarget is what a delegation is for: a task, a repository's pull
// request, or a repository.
type DelegationTarget struct {
	Task   string `json:"task,omitempty"`
	Repo   string `json:"repo,omitempty"`
	PR     int    `json:"pr,omitempty"`
	Branch string `json:"branch,omitempty"`
}

// DelegationRequest is a brief from an integration.
type DelegationRequest struct {
	Integration    string
	CaseID         string
	Mode           string
	Target         DelegationTarget
	Brief          string
	Title          string
	IdempotencyKey string
}

// DelegationStatus is what an integration is told about a delegation.
type DelegationStatus struct {
	DelegationID string                 `json:"delegationId"`
	State        string                 `json:"state"`
	Route        string                 `json:"route"`
	TaskID       string                 `json:"taskId"`
	AgentID      string                 `json:"agentId,omitempty"`
	Enforced     bool                   `json:"enforced"`
	Error        *DelegationStatusError `json:"error,omitempty"`
	CreatedAt    string                 `json:"createdAt"`
	DeliveredAt  string                 `json:"deliveredAt,omitempty"`
}

type DelegationStatusError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// DelegationMessageStatus is what an integration is told about a follow-up
// message.
type DelegationMessageStatus struct {
	MessageID string `json:"messageId"`
	State     string `json:"state"`
}

// DelegationRepository is a repository of the catalog that delegations can
// start agents in.
type DelegationRepository struct {
	Repo       string
	PrimaryDir string
	BaseBranch string
	// WorkspaceID is where a new task for the repository goes; nil for none.
	WorkspaceID *int
}

// DelegationAgents is what the delegation service asks of agents.
type DelegationAgents interface {
	QueueMessage(ctx context.Context, taskID, message string) error
	SpawnAgent(ctx context.Context, taskID string, width, height int, opts ...AgentSpawnOptions) error
	GetAgentSession(ctx context.Context, taskID string) (*store.AgentSession, error)
	GetTaskResumeSession(ctx context.Context, taskID, agentKind string) (*TaskResumeSession, error)
}

// DelegationBoard creates the tasks delegations need.
type DelegationBoard interface {
	ListColumns(ctx context.Context) ([]Column, error)
	CreateTask(ctx context.Context, title, description, column, priority string, workspaceID *int) (*Card, error)
}

// DelegationWorktrees makes a worktree for a pull request's branch.
type DelegationWorktrees interface {
	Create(ctx context.Context, taskID, primaryDir, branch, baseBranch string) (*store.TaskWorktree, error)
}

// DelegationOptions configure the delegation service.
type DelegationOptions struct {
	// Delivery is auto or ask.
	Delivery string
	// Agent is the tool a started agent runs.
	Agent        string
	Repositories []DelegationRepository
	// Worktrees makes worktrees for pull requests; nil when none can be made.
	Worktrees DelegationWorktrees
	// Approvals asks the person first when Delivery is ask.
	Approvals *ApprovalService
	Bus       *events.Bus
	// Run runs a delivery; the default runs it in its own goroutine.
	Run func(func())
}

// DelegationService receives briefs from integrations and hands them to
// tasks' agents.
type DelegationService struct {
	db     *store.Store
	agents DelegationAgents
	board  DelegationBoard
	owners *OwnerLookupService
	opts   DelegationOptions
}

func NewDelegationService(db *store.Store, agents DelegationAgents, board DelegationBoard, opts DelegationOptions) *DelegationService {
	if opts.Run == nil {
		opts.Run = func(f func()) { go f() }
	}
	if opts.Delivery == "" {
		opts.Delivery = "auto"
	}
	if opts.Agent == "" {
		opts.Agent = "claude-code"
	}
	return &DelegationService{db: db, agents: agents, board: board, owners: NewOwnerLookupService(db, nil, ""), opts: opts}
}

const (
	maxDelegationBrief = 64 << 10
	maxDelegationField = 200
)

func validateDelegation(req DelegationRequest) error {
	switch {
	case req.Mode != DelegationFix && req.Mode != DelegationFindOut:
		return delegationErr("bad_request", "mode must be fix or find_out")
	case len(req.CaseID) == 0 || len(req.CaseID) > maxDelegationField:
		return delegationErr("bad_request", "caseId must be 1 to 200 characters")
	case len(req.Brief) == 0 || len(req.Brief) > maxDelegationBrief:
		return delegationErr("bad_request", "brief must be 1 byte to 64 KiB")
	case len(req.Title) > maxDelegationField:
		return delegationErr("bad_request", "title must be at most 200 characters")
	case len(req.IdempotencyKey) == 0 || len(req.IdempotencyKey) > maxDelegationField:
		return delegationErr("bad_request", "idempotencyKey must be 1 to 200 characters")
	}
	t := req.Target
	switch {
	case t.Task != "" && (t.Repo != "" || t.PR != 0 || t.Branch != ""):
		return delegationErr("bad_request", "target is {task}, {repo, pr} or {repo}, not a mix")
	case t.Task == "" && t.Repo == "":
		return delegationErr("bad_request", "target needs task or repo")
	case t.PR < 0:
		return delegationErr("bad_request", "target.pr must be a pull request number")
	}
	return nil
}

func requestHash(parts ...any) string {
	raw, _ := json.Marshal(parts)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func newDelegationID(prefix string) string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// routePlan is how a delegation will reach an agent.
type routePlan struct {
	route  string
	taskID string // empty for a new task
	// For resume: the conversation's kind.
	resumeKind string
	// For a new task.
	repo     *DelegationRepository
	branch   string
	worktree bool
}

// Delegate accepts a brief. It answers at once with the route; delivery
// follows. A repeated request (same key, same body) returns the delegation
// already made, with created false.
func (s *DelegationService) Delegate(ctx context.Context, req DelegationRequest) (DelegationStatus, bool, error) {
	if err := validateDelegation(req); err != nil {
		return DelegationStatus{}, false, err
	}
	hash := requestHash(req.CaseID, req.Mode, req.Target, req.Brief, req.Title)
	if existing, err := s.db.GetDelegationByKey(ctx, req.Integration, req.IdempotencyKey); err == nil {
		if existing.RequestHash != hash {
			return DelegationStatus{}, false, delegationErr("idempotency_conflict", "idempotencyKey %s was used for a different delegation", req.IdempotencyKey)
		}
		return statusOf(existing), false, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return DelegationStatus{}, false, err
	}
	plan, err := s.resolve(ctx, req.Mode, req.Target)
	if err != nil {
		return DelegationStatus{}, false, err
	}
	target, _ := json.Marshal(req.Target)
	d := store.Delegation{
		ID: newDelegationID("dl-"), Integration: req.Integration, IdempotencyKey: req.IdempotencyKey, RequestHash: hash,
		CaseID: req.CaseID, Mode: req.Mode, Target: string(target), Brief: req.Brief, Title: req.Title,
		TaskID: plan.taskID, Route: plan.route, State: store.DelegationPending,
	}
	if err := s.db.InsertDelegation(ctx, d); errors.Is(err, store.ErrConflict) {
		// The same key arrived twice at once; the first one stands.
		return s.Delegate(ctx, req)
	} else if err != nil {
		return DelegationStatus{}, false, err
	}
	s.changed(d.TaskID)
	if s.opts.Delivery == "ask" && s.opts.Approvals != nil {
		if err := s.ask(ctx, &d); err != nil {
			_ = s.db.SetDelegationFailed(ctx, d.ID, "internal", err.Error())
			return DelegationStatus{}, false, err
		}
	} else {
		id := d.ID
		s.opts.Run(func() { s.deliver(context.Background(), id) })
	}
	stored, err := s.db.GetDelegation(ctx, d.ID)
	if err != nil {
		return DelegationStatus{}, false, err
	}
	return statusOf(stored), true, nil
}

// Status is a delegation as its integration sees it.
func (s *DelegationService) Status(ctx context.Context, integration, id string) (DelegationStatus, error) {
	d, err := s.ownDelegation(ctx, integration, id)
	if err != nil {
		return DelegationStatus{}, err
	}
	return statusOf(d), nil
}

func (s *DelegationService) ownDelegation(ctx context.Context, integration, id string) (*store.Delegation, error) {
	d, err := s.db.GetDelegation(ctx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && d.Integration != integration) {
		return nil, delegationErr("not_found", "no delegation %s", id)
	}
	return d, err
}

func statusOf(d *store.Delegation) DelegationStatus {
	st := DelegationStatus{DelegationID: d.ID, State: d.State, Route: d.Route, TaskID: d.TaskID, Enforced: d.Enforced, CreatedAt: d.CreatedAt}
	if d.State == store.DelegationDelivered {
		st.AgentID = d.ExecutionID
		if d.DeliveredAt != nil {
			st.DeliveredAt = *d.DeliveredAt
		}
	}
	if d.State == store.DelegationFailed {
		st.Error = &DelegationStatusError{Code: d.ErrorCode, Message: d.ErrorMessage}
	}
	return st
}

// resolve finds the task and route for a target, or refuses it.
func (s *DelegationService) resolve(ctx context.Context, mode string, target DelegationTarget) (routePlan, error) {
	if target.Task != "" {
		task, err := s.db.GetTask(ctx, target.Task)
		if errors.Is(err, store.ErrNotFound) || (err == nil && task.ArchivedAt != nil) {
			return routePlan{}, delegationErr("unknown_task", "no task %s", target.Task)
		}
		if err != nil {
			return routePlan{}, err
		}
		return s.routeForTask(ctx, task)
	}
	if target.PR > 0 || target.Branch != "" {
		matches, err := s.owners.Lookup(ctx, OwnerQuery{Repo: target.Repo, PR: target.PR, Branch: target.Branch})
		if err != nil && !errors.Is(err, ErrOwnerQuery) {
			return routePlan{}, err
		}
		if len(matches) > 0 {
			task, err := s.db.GetTask(ctx, matches[0].Task.ID)
			if err != nil {
				return routePlan{}, err
			}
			return s.routeForTask(ctx, task)
		}
	}
	repo := s.repository(target.Repo)
	if repo == nil {
		return routePlan{}, delegationErr("unknown_repo", "%s is not in Legato's repositories with a primary_dir", target.Repo)
	}
	plan := routePlan{route: RouteStart, repo: repo}
	if target.PR == 0 {
		return plan, nil
	}
	if target.Branch != "" && s.opts.Worktrees != nil {
		plan.branch, plan.worktree = target.Branch, true
		return plan, nil
	}
	if mode == DelegationFindOut {
		return plan, nil
	}
	return routePlan{}, delegationErr("no_checkout", "no task owns %s#%d and Legato cannot make a worktree for its branch, so there is nowhere to fix it", target.Repo, target.PR)
}

func (s *DelegationService) repository(name string) *DelegationRepository {
	for i := range s.opts.Repositories {
		if r := &s.opts.Repositories[i]; strings.EqualFold(r.Repo, name) && r.PrimaryDir != "" {
			return r
		}
	}
	return nil
}

func (s *DelegationService) routeForTask(ctx context.Context, task *store.Task) (routePlan, error) {
	presence, err := taskAgentPresence(ctx, s.db, task)
	if err != nil {
		return routePlan{}, err
	}
	switch {
	case presence.running != nil:
		return routePlan{route: RouteMessage, taskID: task.ID}, nil
	case presence.notResumable != "":
		return routePlan{}, delegationErr("not_resumable", "task %s's agent has stopped and cannot be resumed for an integration: %s", task.ID, presence.notResumable)
	case presence.savedKind != "":
		return routePlan{route: RouteResume, taskID: task.ID, resumeKind: presence.savedKind}, nil
	}
	if taskWorkingDir(task) == "" {
		return routePlan{}, delegationErr("no_checkout", "task %s has no working directory to start an agent in", task.ID)
	}
	return routePlan{route: RouteStart, taskID: task.ID}, nil
}

func taskWorkingDir(task *store.Task) string {
	if task.WorktreePath != nil && *task.WorktreePath != "" {
		return *task.WorktreePath
	}
	if cc, _ := store.ParseCodeContext(task.CodeContext); cc != nil {
		return cc.WorkingDir
	}
	return ""
}

// deliver hands a pending delegation to its agent, checking its route again
// first: the agent may have stopped or started since.
func (s *DelegationService) deliver(ctx context.Context, id string) {
	d, err := s.db.GetDelegation(ctx, id)
	if err != nil || d.State != store.DelegationPending {
		return
	}
	fail := func(code, message string) {
		if err := s.db.SetDelegationFailed(ctx, id, code, message); err != nil {
			log.Printf("delegation %s: recording failure: %v", id, err)
		}
		s.changed(d.TaskID)
	}
	var target DelegationTarget
	_ = json.Unmarshal([]byte(d.Target), &target)
	var plan routePlan
	if d.TaskID != "" {
		task, err := s.db.GetTask(ctx, d.TaskID)
		if err != nil {
			fail("unknown_task", fmt.Sprintf("task %s: %v", d.TaskID, err))
			return
		}
		if plan, err = s.routeForTask(ctx, task); err != nil {
			fail(codeOf(err), err.Error())
			return
		}
	} else {
		if plan, err = s.resolve(ctx, d.Mode, target); err != nil {
			fail(codeOf(err), err.Error())
			return
		}
		if plan.taskID == "" {
			taskID, err := s.newTask(ctx, d, plan)
			if err != nil {
				fail(codeOf(err), err.Error())
				return
			}
			plan.taskID = taskID
			d.TaskID = taskID
		}
	}
	findOut := d.Mode == DelegationFindOut
	text := delegationText(d, modeLabel(d.Mode))
	if err := s.send(ctx, plan, d.TaskID, text, findOut, newTaskDir(ctx, s.db, plan)); err != nil {
		fail(codeOf(err), err.Error())
		return
	}
	executionID, enforced := "", false
	if session, err := s.agents.GetAgentSession(ctx, d.TaskID); err == nil {
		executionID = session.ExecutionID
		enforced = findOut && plan.route != RouteMessage && session.Sandboxed
	}
	if err := s.db.SetDelegationDelivered(ctx, id, plan.route, executionID, enforced); err != nil {
		log.Printf("delegation %s: recording delivery: %v", id, err)
	}
	s.changed(d.TaskID)
}

func newTaskDir(ctx context.Context, db *store.Store, plan routePlan) string {
	if plan.taskID == "" {
		return ""
	}
	task, err := db.GetTask(ctx, plan.taskID)
	if err != nil {
		return ""
	}
	return taskWorkingDir(task)
}

// send delivers text to a task's agent by a route.
func (s *DelegationService) send(ctx context.Context, plan routePlan, taskID, text string, readOnly bool, dir string) error {
	switch plan.route {
	case RouteMessage:
		if err := s.agents.QueueMessage(ctx, taskID, text); err != nil {
			return delegationErr("message_failed", "messaging task %s's agent: %v", taskID, err)
		}
		return nil
	case RouteResume:
		saved, err := s.agents.GetTaskResumeSession(ctx, taskID, plan.resumeKind)
		if err != nil || saved == nil {
			return delegationErr("not_resumable", "task %s's %s conversation is no longer saved", taskID, plan.resumeKind)
		}
		opts := AgentSpawnOptions{AgentKind: plan.resumeKind, SessionPolicy: "resume", ResumeSessionID: saved.ID, ResumeWorkingDir: saved.CWD,
			ResumeStorageDir: saved.StorageDir, WorkingDir: firstNonEmpty(saved.CWD, dir), Prompt: text, ReadOnlyCheckout: readOnly}
		if err := s.agents.SpawnAgent(ctx, taskID, 0, 0, opts); err != nil {
			return delegationErr("spawn_failed", "resuming task %s's agent: %v", taskID, err)
		}
		return nil
	default:
		if dir == "" {
			return delegationErr("no_checkout", "task %s has no working directory to start an agent in", taskID)
		}
		opts := AgentSpawnOptions{AgentKind: s.opts.Agent, WorkingDir: dir, Prompt: text, ReadOnlyCheckout: readOnly}
		if err := s.agents.SpawnAgent(ctx, taskID, 0, 0, opts); err != nil {
			return delegationErr("spawn_failed", "starting an agent for task %s: %v", taskID, err)
		}
		return nil
	}
}

// newTask creates the task a delegation without an owner needs, with its
// checkout: a worktree on the pull request's branch, or the main checkout.
func (s *DelegationService) newTask(ctx context.Context, d *store.Delegation, plan routePlan) (string, error) {
	title := d.Title
	if title == "" {
		title = "Argus: " + d.CaseID
	}
	card, err := s.board.CreateTask(ctx, title, "", s.newTaskColumn(ctx), "", plan.repo.WorkspaceID)
	if err != nil {
		return "", err
	}
	if err := s.db.SetDelegationTask(ctx, d.ID, card.ID); err != nil {
		return "", err
	}
	dir := plan.repo.PrimaryDir
	if plan.worktree {
		wt, err := s.opts.Worktrees.Create(ctx, card.ID, plan.repo.PrimaryDir, plan.branch, plan.repo.BaseBranch)
		if err != nil {
			return card.ID, delegationErr("no_checkout", "making a worktree on %s: %v", plan.branch, err)
		}
		dir = wt.Path
	}
	cc := &store.CodeContext{Repo: plan.repo.Repo, Branch: plan.branch, WorkingDir: dir, BaseBranch: plan.repo.BaseBranch, RepoSource: "integration"}
	if plan.worktree {
		cc.BranchSource = "worktree"
	}
	if err := s.db.SetCodeContext(ctx, card.ID, cc); err != nil {
		return card.ID, err
	}
	return card.ID, nil
}

// newTaskColumn is where a new task goes: the column named Doing or In
// Progress, else the first column.
func (s *DelegationService) newTaskColumn(ctx context.Context) string {
	columns, err := s.board.ListColumns(ctx)
	if err != nil || len(columns) == 0 {
		return "Doing"
	}
	for _, c := range columns {
		if strings.EqualFold(c.Name, "Doing") || strings.EqualFold(c.Name, "In Progress") {
			return c.Name
		}
	}
	return columns[0].Name
}

func codeOf(err error) string {
	var de *DelegationError
	if errors.As(err, &de) {
		return de.Code
	}
	return "internal"
}

func modeLabel(mode string) string {
	if mode == DelegationFindOut {
		return "find out"
	}
	return mode
}

// delegationText is what the agent receives: the brief, unchanged, between a
// tag and a footer that says who it is from and how to answer.
func delegationText(d *store.Delegation, label string) string {
	return delegationWrap(d, label, d.Brief)
}

func delegationWrap(d *store.Delegation, label, body string) string {
	return fmt.Sprintf("[%s case %s · delegation %s · %s]\n\n%s\n\n---\nFrom %s, not from the person. When you are done, have found something, have a question, or cannot do it, call the reply_case tool with delegation_id %s.",
		d.Integration, d.CaseID, d.ID, label, body, integrationTitle(d.Integration), d.ID)
}

func integrationTitle(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// Message delivers a follow-up message to a delegation's agent.
func (s *DelegationService) Message(ctx context.Context, integration, id, text, key string) (DelegationMessageStatus, bool, error) {
	if len(text) == 0 || len(text) > maxDelegationBrief {
		return DelegationMessageStatus{}, false, delegationErr("bad_request", "text must be 1 byte to 64 KiB")
	}
	if len(key) == 0 || len(key) > maxDelegationField {
		return DelegationMessageStatus{}, false, delegationErr("bad_request", "idempotencyKey must be 1 to 200 characters")
	}
	d, err := s.ownDelegation(ctx, integration, id)
	if err != nil {
		return DelegationMessageStatus{}, false, err
	}
	hash := requestHash(text)
	if existing, err := s.db.GetDelegationMessageByKey(ctx, id, key); err == nil {
		if existing.RequestHash != hash {
			return DelegationMessageStatus{}, false, delegationErr("idempotency_conflict", "idempotencyKey %s was used for a different message", key)
		}
		return DelegationMessageStatus{MessageID: existing.ID, State: existing.State}, false, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return DelegationMessageStatus{}, false, err
	}
	switch d.State {
	case store.DelegationFailed:
		return DelegationMessageStatus{}, false, delegationErr("closed", "delegation %s failed: %s", id, d.ErrorMessage)
	case store.DelegationPending, store.DelegationAwaitingApproval:
		return DelegationMessageStatus{}, false, delegationErr("not_delivered", "delegation %s has not reached its agent yet", id)
	}
	m := store.DelegationMessage{ID: newDelegationID("dm-"), DelegationID: id, IdempotencyKey: key, RequestHash: hash, Text: text, State: store.DelegationPending}
	if err := s.db.InsertDelegationMessage(ctx, m); errors.Is(err, store.ErrConflict) {
		return s.Message(ctx, integration, id, text, key)
	} else if err != nil {
		return DelegationMessageStatus{}, false, err
	}
	state, code, message := store.DelegationDelivered, "", ""
	if err := s.deliverMessage(ctx, d, text); err != nil {
		state, code, message = store.DelegationFailed, codeOf(err), err.Error()
	}
	if err := s.db.SetDelegationMessageState(ctx, m.ID, state, code, message); err != nil {
		return DelegationMessageStatus{}, false, err
	}
	s.changed(d.TaskID)
	return DelegationMessageStatus{MessageID: m.ID, State: state}, true, nil
}

func (s *DelegationService) deliverMessage(ctx context.Context, d *store.Delegation, text string) error {
	task, err := s.db.GetTask(ctx, d.TaskID)
	if err != nil {
		return delegationErr("unknown_task", "task %s: %v", d.TaskID, err)
	}
	plan, err := s.routeForTask(ctx, task)
	if err != nil {
		return err
	}
	if plan.route == RouteStart {
		// The agent that had the brief is gone with its conversation; a new
		// one would not know what the message answers.
		return delegationErr("not_resumable", "task %s's agent has ended without a saved conversation", d.TaskID)
	}
	return s.send(ctx, plan, d.TaskID, delegationWrap(d, "message", text), d.Mode == DelegationFindOut, taskWorkingDir(task))
}

// ResumeStale delivers delegations left pending for longer than age: ones a
// Legato accepted and stopped before delivering. Younger ones may still be
// on their way.
func (s *DelegationService) ResumeStale(ctx context.Context, age time.Duration) {
	pending, err := s.db.ListDelegationsByState(ctx, store.DelegationPending)
	if err != nil {
		log.Printf("delegations: listing pending: %v", err)
		return
	}
	cutoff := time.Now().UTC().Add(-age)
	for _, d := range pending {
		created, err := time.Parse("2006-01-02T15:04:05.000Z", d.CreatedAt)
		if err != nil || created.After(cutoff) {
			continue
		}
		id := d.ID
		s.opts.Run(func() { s.deliver(context.Background(), id) })
	}
}

// changed tells the rest of Legato a task's delegations changed.
func (s *DelegationService) changed(taskID string) {
	if s.opts.Bus != nil {
		s.opts.Bus.Publish(events.Event{Type: events.EventDelegationChanged, At: time.Now(), Payload: taskID})
	}
}

// taskDelegations lists a task's delegations with the replies to each.
func taskDelegations(ctx context.Context, db *store.Store, taskID string) ([]TaskDelegationActivity, error) {
	delegations, err := db.ListDelegationsForTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	replies, err := db.ListCaseRepliesForTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]TaskDelegationActivity, 0, len(delegations))
	for _, d := range delegations {
		item := TaskDelegationActivity{ID: d.ID, Integration: d.Integration, CaseID: d.CaseID, Mode: d.Mode, Route: d.Route, State: d.State,
			Enforced: d.Enforced, ErrorCode: d.ErrorCode, ErrorMessage: d.ErrorMessage, Brief: d.Brief, CreatedAt: d.CreatedAt, Replies: []TaskCaseReplyItem{}}
		if d.DeliveredAt != nil {
			item.DeliveredAt = *d.DeliveredAt
		}
		for _, r := range replies {
			if r.DelegationID != d.ID {
				continue
			}
			var body struct {
				Outcome string `json:"outcome"`
				Text    string `json:"text"`
			}
			_ = json.Unmarshal([]byte(r.Body), &body)
			item.Replies = append(item.Replies, TaskCaseReplyItem{ID: r.ID, Outcome: body.Outcome, Text: body.Text, State: r.State, LastError: r.LastError, CreatedAt: r.CreatedAt.Format(time.RFC3339)})
		}
		out = append(out, item)
	}
	return out, nil
}
