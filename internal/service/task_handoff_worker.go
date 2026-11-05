package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/stackgraph"
	"github.com/cpave3/legato/internal/engine/store"
)

// HandoffAgents is what a worker asks of its agent service to start a
// handed-off task's agent.
type HandoffAgents interface {
	SpawnAgent(ctx context.Context, taskID string, width, height int, opts ...AgentSpawnOptions) error
	RegisteredAdapters() []string
	DefaultAdapter() string
	AdapterFor(kind string) AIToolAdapter
	GetAgentSession(ctx context.Context, taskID string) (*store.AgentSession, error)
}

// HandoffRepositories is what a worker asks of its repository cache: which
// repositories it can clone, and whether GitHub has a branch a handoff
// starts from (RepositoryService).
type HandoffRepositories interface {
	CloneChecker
	// PushedBranch fetches the repository's cache and returns its default
	// branch, or ErrBranchNotOnGitHub when GitHub has no such branch.
	PushedBranch(ctx context.Context, repo, branch string) (base string, err error)
}

// HandoffGrants is what a worker asks of its credential broker for a
// make-a-change handoff's session.
type HandoffGrants interface {
	GrantBranches(ctx context.Context, g BranchGrant) (int, error)
}

// HandoffWorkerOptions configure a worker's side of handoff.
type HandoffWorkerOptions struct {
	// Agents starts the agents.
	Agents HandoffAgents
	// Repositories says which repositories this worker can clone and
	// which branches GitHub has (the repository service); nil when it can
	// clone none.
	Repositories HandoffRepositories
	// Sandbox says whether agents can run in Legato's sandbox here. A
	// worker without it refuses every handoff.
	Sandbox func() bool
	// Grants allows a make-a-change handoff's session to push its
	// branches and open draft pull requests without asking (the credential
	// broker); nil grants nothing, and the session asks as any other.
	Grants HandoffGrants
	// Bus hears of new cards and changed handoffs; nil for none.
	Bus *events.Bus
	// Run runs an agent start; the default runs it in its own goroutine.
	Run func(func())
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// StreamRescan is how often an Events stream looks again at its
	// main's handoffs, besides when this process publishes a change: an
	// agent's activity set by another process may tell this one nothing.
	// 5 seconds when zero.
	StreamRescan time.Duration
}

// HandoffMain is the paired main instance a handoff call comes from.
type HandoffMain struct {
	InstanceID string
	Name       string
}

// HandoffWorkerService is a worker instance's side of handoff: it accepts a
// main's Handoff call (a card in the main's workspace, its notes, a durable
// record), then starts the task's agent, sandboxed and unattended, with the
// handoff's brief. The record moves pending → in_progress → done (the card
// in a column named Done), or failed with the reason.
type HandoffWorkerService struct {
	db   *store.Store
	opts HandoffWorkerOptions
}

func NewHandoffWorkerService(db *store.Store, opts HandoffWorkerOptions) *HandoffWorkerService {
	if opts.Run == nil {
		opts.Run = func(f func()) { go f() }
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &HandoffWorkerService{db: db, opts: opts}
}

const (
	maxHandoffField = 200
	maxHandoffTitle = 500
	maxHandoffText  = 256 << 10
	maxHandoffNotes = 500
)

func validateHandoffCall(call HandoffCall) error {
	switch {
	case len(call.HandoffID) == 0 || len(call.HandoffID) > maxHandoffField:
		return handoffErr("bad_request", "handoff_id must be 1 to 200 characters")
	case len(call.MainTaskID) == 0 || len(call.MainTaskID) > maxHandoffField:
		return handoffErr("bad_request", "main_task_id must be 1 to 200 characters")
	case strings.TrimSpace(call.Title) == "" || len(call.Title) > maxHandoffTitle:
		return handoffErr("bad_request", "title must be 1 to 500 characters")
	case len(call.Details) > maxHandoffText:
		return handoffErr("bad_request", "details must be at most 256 KiB")
	case len(call.Notes) > maxHandoffNotes:
		return handoffErr("bad_request", "at most 500 notes travel with a task")
	case call.Purpose != HandoffMakeChange && call.Purpose != HandoffFindOut:
		return handoffErr("bad_request", "purpose must be %s or %s", HandoffMakeChange, HandoffFindOut)
	case call.Kind != HandoffNewBranch && call.Kind != HandoffPushedBranch:
		return handoffErr("bad_request", "kind must be %s or %s", HandoffNewBranch, HandoffPushedBranch)
	case call.Jira != nil:
		return handoffErr("unsupported", "this worker cannot take a Jira-backed task yet")
	case validRepo(call.Repo) != nil:
		return handoffErr("bad_request", "repo must be a GitHub repository, owner/name")
	case !stackgraph.ValidBranchName(call.Branch):
		return handoffErr("bad_request", "branch must name a branch")
	}
	for _, n := range call.Notes {
		if n.ID == "" || len(n.ID) > maxHandoffField || len(n.Body) > maxHandoffText {
			return handoffErr("bad_request", "each note needs an id and at most 256 KiB of body")
		}
	}
	return nil
}

// Accept takes a main's Handoff call. It checks the handoff can run here
// (the sandbox, a known agent tool that can run unattended, a repository
// this worker can clone) before it makes anything, then makes the card in
// the main's workspace with the main's notes and stores the handoff as
// pending, all in one transaction; the agent starts afterwards. The same
// handoff ID and body again answers with the card already made (created
// false); the same ID with another body is idempotency_conflict.
func (s *HandoffWorkerService) Accept(ctx context.Context, main HandoffMain, call HandoffCall) (HandoffAccepted, bool, error) {
	if err := validateHandoffCall(call); err != nil {
		return HandoffAccepted{}, false, err
	}
	hash := requestHash(call)
	if existing, err := s.db.GetHandoff(ctx, main.InstanceID, call.HandoffID); err == nil {
		if existing.RequestHash != hash {
			return HandoffAccepted{}, false, handoffErr("idempotency_conflict", "handoff %s was made with a different request", call.HandoffID)
		}
		accepted, err := s.accepted(ctx, existing)
		return accepted, false, err
	} else if !errors.Is(err, store.ErrNotFound) {
		return HandoffAccepted{}, false, err
	}

	if s.opts.Sandbox == nil || !s.opts.Sandbox() {
		return HandoffAccepted{}, false, handoffErr("no_sandbox", "this worker cannot run agents in Legato's sandbox, which a handed-off agent needs")
	}
	agentKind, err := s.agentKind(call.AgentKind)
	if err != nil {
		return HandoffAccepted{}, false, err
	}
	if s.opts.Repositories == nil {
		return HandoffAccepted{}, false, handoffErr("cannot_clone", "this worker cannot clone %s: it has no repository cache", call.Repo)
	}
	if problem := s.opts.Repositories.CloneProblem(call.Repo); problem != "" {
		return HandoffAccepted{}, false, handoffErr("cannot_clone", "this worker cannot clone %s: %s", call.Repo, problem)
	}

	workspaceID, err := s.db.EnsureMainWorkspace(ctx, main.InstanceID, main.Name)
	if err != nil {
		return HandoffAccepted{}, false, err
	}
	column, err := s.newTaskColumn(ctx)
	if err != nil {
		return HandoffAccepted{}, false, err
	}
	now := s.opts.Now().UTC().Format(time.RFC3339)
	task := store.Task{ID: store.GenerateTaskID(), Title: call.Title, Description: call.Details, DescriptionMD: call.Details,
		Status: column, WorkspaceID: &workspaceID, CreatedAt: now, UpdatedAt: now}
	if existing, err := s.db.ListTasksByStatus(ctx, column); err == nil {
		for _, t := range existing {
			if t.SortOrder >= task.SortOrder {
				task.SortOrder = t.SortOrder + 1
			}
		}
	}
	notes := make([]store.TaskNote, 0, len(call.Notes))
	for _, n := range call.Notes {
		notes = append(notes, store.TaskNote{ID: workflowID("tn-"), TaskID: task.ID, Body: n.Body, Source: NoteSourceMain, RemoteID: n.ID})
	}
	record := store.Handoff{MainInstanceID: main.InstanceID, HandoffID: call.HandoffID, MainName: main.Name, MainTaskID: call.MainTaskID,
		TaskID: task.ID, Purpose: call.Purpose, Kind: call.Kind, Repo: call.Repo, Branch: call.Branch, AgentKind: agentKind,
		State: store.HandoffPending, RequestHash: hash, CreatedAt: now, UpdatedAt: now}
	if err := s.db.CreateHandoffCard(ctx, task, notes, record); errors.Is(err, store.ErrConflict) {
		// The same handoff arrived twice at once; the first one stands.
		return s.Accept(ctx, main, call)
	} else if err != nil {
		return HandoffAccepted{}, false, err
	}
	s.publish(events.EventCardsRefreshed, nil)
	s.publish(events.EventHandoffChanged, task.ID)
	mainID, handoffID := main.InstanceID, call.HandoffID
	s.opts.Run(func() { s.start(context.Background(), mainID, handoffID) })
	accepted, err := s.accepted(ctx, &record)
	return accepted, true, err
}

// Note sources of a handed-off task's notes: the main's, which came with the
// handoff, and the worker's own.
const (
	NoteSourceMain = "main"
)

// agentKind is the agent tool a handoff names, checked: one this worker
// knows (the default when empty) and can start with its own permission
// prompts off. It never falls back to another tool.
func (s *HandoffWorkerService) agentKind(kind string) (string, error) {
	if s.opts.Agents == nil {
		return "", handoffErr("unknown_agent", "this worker cannot start agents")
	}
	if kind == "" {
		kind = s.opts.Agents.DefaultAdapter()
	}
	known := kind != "" && kind == s.opts.Agents.DefaultAdapter()
	for _, name := range s.opts.Agents.RegisteredAdapters() {
		known = known || name == kind
	}
	if !known {
		return "", handoffErr("unknown_agent", "this worker does not know the agent tool %q (it knows %s)", kind, strings.Join(s.knownAgents(), ", "))
	}
	if _, ok := s.opts.Agents.AdapterFor(kind).(PromptsOffAdapter); !ok {
		return "", handoffErr("unknown_agent", "this worker cannot start %s with its permission prompts off, which a handed-off agent needs", kind)
	}
	return kind, nil
}

func (s *HandoffWorkerService) knownAgents() []string {
	names := s.opts.Agents.RegisteredAdapters()
	if d := s.opts.Agents.DefaultAdapter(); d != "" && !containsString(names, d) {
		names = append([]string{d}, names...)
	}
	if len(names) == 0 {
		return []string{"none"}
	}
	return names
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// newTaskColumn is where a handed-off card goes: the column named Doing or
// In Progress, else the first column.
func (s *HandoffWorkerService) newTaskColumn(ctx context.Context) (string, error) {
	columns, err := s.db.ListColumnMappings(ctx)
	if err != nil {
		return "", err
	}
	if len(columns) == 0 {
		return "", errors.New("this worker's board has no columns")
	}
	for _, c := range columns {
		if strings.EqualFold(c.ColumnName, "Doing") || strings.EqualFold(c.ColumnName, "In Progress") {
			return c.ColumnName, nil
		}
	}
	return columns[0].ColumnName, nil
}

// Get is a main's handoff as the main sees it, or not_found for a handoff
// of another main or none.
func (s *HandoffWorkerService) Get(ctx context.Context, mainInstanceID, handoffID string) (HandoffAccepted, error) {
	h, err := s.db.GetHandoff(ctx, mainInstanceID, handoffID)
	if errors.Is(err, store.ErrNotFound) {
		return HandoffAccepted{}, handoffErr("not_found", "no handoff %s", handoffID)
	}
	if err != nil {
		return HandoffAccepted{}, err
	}
	return s.accepted(ctx, h)
}

func (s *HandoffWorkerService) accepted(ctx context.Context, h *store.Handoff) (HandoffAccepted, error) {
	id, err := InstanceID(ctx, s.db)
	if err != nil {
		return HandoffAccepted{}, err
	}
	return HandoffAccepted{HandoffID: h.HandoffID, WorkerTaskID: h.TaskID, WorkerInstanceID: id, State: h.State, Failure: h.Failure}, nil
}

// start starts a pending handoff's agent: in a clone of its repository on
// its branch, sandboxed, with the tool's permission prompts off and the
// brief as its assignment. A failure makes the handoff failed, with why.
func (s *HandoffWorkerService) start(ctx context.Context, mainInstanceID, handoffID string) {
	h, err := s.db.GetHandoff(ctx, mainInstanceID, handoffID)
	if err != nil || h.ClosedAt != nil || h.State != store.HandoffPending {
		return
	}
	now := s.opts.Now().UTC()
	claimed, err := s.db.ClaimHandoffStart(ctx, mainInstanceID, handoffID, now.Format(time.RFC3339), now.Add(-handoffStartLease).Format(time.RFC3339))
	if err != nil {
		log.Printf("handoff %s: claiming its start: %v", handoffID, err)
		return
	}
	if !claimed {
		// Being started here or by another Legato on this database.
		return
	}
	move := func(to, failure string) {
		moved, err := s.db.SetHandoffState(ctx, mainInstanceID, handoffID, store.HandoffPending, to, failure, s.opts.Now().UTC().Format(time.RFC3339))
		if err != nil {
			log.Printf("handoff %s: recording %s: %v", handoffID, to, err)
		}
		if moved {
			s.publish(events.EventHandoffChanged, h.TaskID)
		}
	}
	// Another Legato on this database may have started it already.
	if session, err := s.opts.Agents.GetAgentSession(ctx, h.TaskID); err == nil && session != nil {
		move(store.HandoffInProgress, "")
		return
	}
	task, err := s.db.GetTask(ctx, h.TaskID)
	if err != nil {
		move(store.HandoffFailed, fmt.Sprintf("the task's card is gone: %v", err))
		return
	}
	notes, err := s.db.ListTaskNotes(ctx, h.TaskID)
	if err != nil {
		move(store.HandoffFailed, err.Error())
		return
	}
	in := handoffBriefInput(h, task, notes)
	if h.Kind == HandoffPushedBranch {
		base, failure := s.pushedBranch(ctx, h)
		if failure != "" {
			move(store.HandoffFailed, failure)
			return
		}
		in.Base = base
	}
	brief := HandoffBrief(in)
	opts := AgentSpawnOptions{AgentKind: h.AgentKind, Repo: h.Repo, Branch: h.Branch, Assignment: brief, Unattended: true,
		Branches: handoffSessionBranches(h), PushedBranch: h.Kind == HandoffPushedBranch}
	if err := s.opts.Agents.SpawnAgent(ctx, h.TaskID, 0, 0, opts); err != nil {
		move(store.HandoffFailed, startFailure(err))
		return
	}
	s.grant(ctx, h)
	move(store.HandoffInProgress, "")
}

// pushedBranch checks the branch a handoff starts from is on GitHub, after
// fetching the worker's copy of the repository, and is not its default
// branch, which an agent never works on. It returns the default branch, or
// why the handoff fails.
func (s *HandoffWorkerService) pushedBranch(ctx context.Context, h *store.Handoff) (base, failure string) {
	if s.opts.Repositories == nil {
		return "", fmt.Sprintf("this worker cannot clone %s: it has no repository cache", h.Repo)
	}
	base, err := s.opts.Repositories.PushedBranch(ctx, h.Repo, h.Branch)
	if err != nil {
		return "", startFailure(err)
	}
	if base == h.Branch {
		return "", fmt.Sprintf("%s is the default branch of %s: hand off a branch of your own", h.Branch, h.Repo)
	}
	return base, ""
}

// startFailure is why a handoff's start failed, as its main shows it: a
// branch GitHub does not have says so first.
func startFailure(err error) string {
	if errors.Is(err, ErrBranchNotOnGitHub) {
		msg := err.Error()
		if i := strings.Index(msg, ErrBranchNotOnGitHub.Error()); i > 0 {
			msg = msg[i:]
		}
		return msg
	}
	return err.Error()
}

// grant allows a make-a-change handoff's new session, without asking the
// person, to push the given branch (when the work starts from one),
// legato/<main task id> and legato/<main task id>-<a few words>, and to open
// draft pull requests from them: handing off was the person's approval. A
// find-out handoff gets nothing. A grant that fails is logged; the session
// then asks as any other would.
func (s *HandoffWorkerService) grant(ctx context.Context, h *store.Handoff) {
	if s.opts.Grants == nil || h.Purpose != HandoffMakeChange {
		return
	}
	session, err := s.opts.Agents.GetAgentSession(ctx, h.TaskID)
	if err != nil || session == nil || session.ExecutionID == "" {
		log.Printf("handoff %s: no session to grant pushes to: %v", h.HandoffID, err)
		return
	}
	given := ""
	if h.Kind == HandoffPushedBranch {
		given = h.Branch
	}
	if _, err := s.opts.Grants.GrantBranches(ctx, BranchGrant{TaskID: h.TaskID, ExecutionID: session.ExecutionID, Repo: h.Repo,
		Branches: HandoffGrantBranches(h.MainTaskID, given), By: "handoff " + h.HandoffID}); err != nil {
		log.Printf("handoff %s: granting its pushes: %v", h.HandoffID, err)
	}
}

// handoffBriefInput is what the brief says of a handoff: its task as the
// worker's card has it now, and the main's notes.
func handoffBriefInput(h *store.Handoff, task *store.Task, notes []store.TaskNote) HandoffBriefInput {
	in := HandoffBriefInput{MainName: h.MainName, MainTaskID: h.MainTaskID, Title: task.Title, Details: task.DescriptionMD,
		Purpose: h.Purpose, Kind: h.Kind, Branch: h.Branch}
	for _, n := range notes {
		if n.Source == NoteSourceMain {
			in.Notes = append(in.Notes, n.Body)
		}
	}
	return in
}

// handoffStartLease is how long a begun agent start holds its handoff: a
// clone can take minutes. A handoff still pending after it is started again.
const handoffStartLease = 15 * time.Minute

// ResumePending starts the agents of pending handoffs no Legato is starting:
// ones a stopped worker accepted and never started, or began starting and
// died (after handoffStartLease). The runtime calls it at start and every
// minute.
func (s *HandoffWorkerService) ResumePending(ctx context.Context) {
	open, err := s.db.ListOpenHandoffs(ctx)
	if err != nil {
		log.Printf("handoffs: listing pending: %v", err)
		return
	}
	stale := s.opts.Now().UTC().Add(-handoffStartLease).Format(time.RFC3339)
	for _, h := range open {
		if h.State != store.HandoffPending || (h.StartAttemptAt != nil && *h.StartAttemptAt >= stale) {
			continue
		}
		mainID, handoffID := h.MainInstanceID, h.HandoffID
		s.opts.Run(func() { s.start(context.Background(), mainID, handoffID) })
	}
}

// Reconcile brings each running handoff's state in line with its card: done
// while the card is in a column named Done, in progress otherwise.
func (s *HandoffWorkerService) Reconcile(ctx context.Context) error {
	open, err := s.db.ListOpenHandoffs(ctx)
	if err != nil {
		return err
	}
	for _, h := range open {
		if h.State != store.HandoffInProgress && h.State != store.HandoffDone {
			continue
		}
		task, err := s.db.GetTask(ctx, h.TaskID)
		if err != nil {
			continue
		}
		want := store.HandoffInProgress
		if strings.EqualFold(task.Status, "Done") {
			want = store.HandoffDone
		}
		if want == h.State {
			continue
		}
		moved, err := s.db.SetHandoffState(ctx, h.MainInstanceID, h.HandoffID, h.State, want, "", s.opts.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return err
		}
		if moved {
			s.publish(events.EventHandoffChanged, h.TaskID)
		}
	}
	return nil
}

// StartEventLoop reconciles handoffs whenever a card moves or changes, and
// every interval besides (a card moved by another process may tell this
// one nothing). stop ends it.
func (s *HandoffWorkerService) StartEventLoop(ctx context.Context, interval time.Duration) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	if s.opts.Bus == nil {
		return cancel
	}
	moved := s.opts.Bus.Subscribe(events.EventCardMoved)
	updated := s.opts.Bus.Subscribe(events.EventCardUpdated)
	refreshed := s.opts.Bus.Subscribe(events.EventCardsRefreshed)
	var tick <-chan time.Time
	var ticker *time.Ticker
	if interval > 0 {
		ticker = time.NewTicker(interval)
		tick = ticker.C
	}
	go func() {
		defer s.opts.Bus.Unsubscribe(moved)
		defer s.opts.Bus.Unsubscribe(updated)
		defer s.opts.Bus.Unsubscribe(refreshed)
		if ticker != nil {
			defer ticker.Stop()
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-moved:
			case <-updated:
			case <-refreshed:
			case <-tick:
			}
			if err := s.Reconcile(ctx); err != nil && ctx.Err() == nil {
				log.Printf("handoffs: %v", err)
			}
		}
	}()
	return cancel
}

// OpenMainHandoffs counts each paired main's open handoffs, by instance ID.
func (s *HandoffWorkerService) OpenMainHandoffs(ctx context.Context) (map[string]int, error) {
	return s.db.CountOpenHandoffsByMain(ctx)
}

func (s *HandoffWorkerService) publish(t events.EventType, payload any) {
	if s.opts.Bus != nil {
		s.opts.Bus.Publish(events.Event{Type: t, At: time.Now(), Payload: payload})
	}
}
