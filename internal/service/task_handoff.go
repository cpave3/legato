package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/stackgraph"
	"github.com/cpave3/legato/internal/engine/store"
)

var (
	// ErrHandoffInput: the handoff asked for cannot be made as asked (an
	// unknown purpose, no repository, a task that cannot travel).
	ErrHandoffInput = errors.New("cannot hand off that task")
	// ErrHandoffAgentRunning: the task's agent is running on this main.
	ErrHandoffAgentRunning = errors.New("the task's agent is running here")
)

// TaskHandoffRequest is a person handing off a task: to which worker (its
// instance ID), for what purpose, where the work starts, in which
// repository and with which agent tool (empty for the worker's default).
type TaskHandoffRequest struct {
	TaskID  string
	Worker  string
	Purpose string
	Kind    string // HandoffNewBranch when empty
	// Branch is the branch already on GitHub that a HandoffPushedBranch
	// handoff starts from; a new branch is always legato/<task id>.
	Branch    string
	Repo      string
	AgentKind string
	// Review overrides the worker's review passes; nil keeps its defaults.
	Review *ReviewPassOverride
}

// AwayTask is a task of this main away on a worker: where it went, how,
// and what the worker last said of it.
type AwayTask struct {
	TaskID           string `json:"task_id"`
	WorkerInstanceID string `json:"worker_instance_id"`
	WorkerName       string `json:"worker_name"`
	HandoffID        string `json:"handoff_id"`
	WorkerTaskID     string `json:"worker_task_id"`
	Purpose          string `json:"purpose"`
	Kind             string `json:"kind"`
	Repo             string `json:"repo"`
	Branch           string `json:"branch"`
	AgentKind        string `json:"agent_kind,omitempty"`
	LeftColumn       string `json:"left_column"`
	HandedOffAt      string `json:"handed_off_at"`
	// State is the worker's state of the handoff as last heard (pending,
	// in_progress, done, failed, returning), with Failure its reason when
	// failed and StateAt when it was heard; empty when nothing was.
	State   string `json:"state,omitempty"`
	Failure string `json:"failure,omitempty"`
	StateAt string `json:"state_at,omitempty"`
	// WorkerURL opens the task on the worker's web UI: the address the
	// worker gave, else its board with the card picked out.
	WorkerURL string `json:"worker_url,omitempty"`
	// Activity is the worker's agent as last heard (working, waiting,
	// idle, ended); WorkerColumn, WorkerTitle and Branches are the card's
	// column and title there and the branches pushed, in stack order.
	Activity     string   `json:"activity,omitempty"`
	WorkerColumn string   `json:"worker_column,omitempty"`
	WorkerTitle  string   `json:"worker_title,omitempty"`
	Branches     []string `json:"branches,omitempty"`
	// Review is the handoff's review passes as last heard, nil without.
	Review *HandoffReviewBlock `json:"review,omitempty"`
	// Lost says why the worker no longer has the task, since LostAt; empty
	// while it has it.
	Lost   string `json:"lost,omitempty"`
	LostAt string `json:"lost_at,omitempty"`
	// WorkerUnreachableSince is when the worker went down, once it has been
	// down for longer than the grace; WorkerRefusedAt is when it stopped
	// accepting this main.
	WorkerUnreachableSince string `json:"worker_unreachable_since,omitempty"`
	WorkerRefusedAt        string `json:"worker_refused_at,omitempty"`
	// View is what the main shows of the task: one of the AwayView
	// constants, from the worker's state and activity and the main's own
	// view of the worker.
	View string `json:"view"`
}

// HandoffSummary is what a worker last said of a handoff, kept as the away
// task's last summary: first the state it accepted the handoff in, then
// each summary row it sends.
type HandoffSummary struct {
	HandoffID    string `json:"handoff_id"`
	WorkerTaskID string `json:"worker_task_id"`
	State        string `json:"state"`
	Failure      string `json:"failure,omitempty"`
	// URL opens the task on the worker: an address, or a path on the
	// worker's web UI (/board?highlight_task=...) the main puts after the
	// worker's address; empty for the main to make one.
	URL string `json:"url,omitempty"`
	// Activity is the agent's (HandoffActivityWorking, Waiting, Idle,
	// Ended); empty while the handoff is pending.
	Activity string `json:"activity,omitempty"`
	// Column is the card's column on the worker, Title its title there.
	Column string `json:"column,omitempty"`
	Title  string `json:"title,omitempty"`
	// Branches are the branches pushed for the task, in stack order.
	Branches []string `json:"branches,omitempty"`
	// Review is the handoff's review passes, on a worker that runs them.
	Review *HandoffReviewBlock `json:"review,omitempty"`
}

func awayTaskFromRow(a store.AwayTask) AwayTask {
	away := AwayTask{TaskID: a.TaskID, WorkerInstanceID: a.WorkerInstanceID, WorkerName: a.WorkerName, HandoffID: a.HandoffID,
		WorkerTaskID: a.WorkerTaskID, Purpose: a.Purpose, Kind: a.Kind, Repo: a.Repo, Branch: a.Branch, AgentKind: a.AgentKind,
		LeftColumn: a.LeftColumn, HandedOffAt: a.CreatedAt}
	var summary HandoffSummary
	if a.LastSummary != nil && json.Unmarshal([]byte(*a.LastSummary), &summary) == nil {
		away.State, away.Failure, away.WorkerURL = summary.State, summary.Failure, summary.URL
		away.Activity, away.WorkerColumn, away.WorkerTitle, away.Branches = summary.Activity, summary.Column, summary.Title, summary.Branches
		away.Review = summary.Review
		if a.LastSummaryAt != nil {
			away.StateAt = *a.LastSummaryAt
		}
	}
	if strings.HasPrefix(away.WorkerURL, "/") && a.WorkerAddress != "" {
		away.WorkerURL = strings.TrimRight(a.WorkerAddress, "/") + away.WorkerURL
	}
	if away.WorkerURL == "" && a.WorkerAddress != "" && a.WorkerTaskID != "" {
		away.WorkerURL = strings.TrimRight(a.WorkerAddress, "/") + "/board?highlight_task=" + url.QueryEscape(a.WorkerTaskID)
	}
	awayReach(&away, a)
	return away
}

// TaskHandoffOptions configure a main's handoffs.
type TaskHandoffOptions struct {
	// Workers reaches the paired workers.
	Workers *WorkerService
	// Bus hears that a task went away; nil for none.
	Bus *events.Bus
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// TaskHandoffService is a main instance's side of handing tasks to workers:
// it sends the Handoff call and keeps the tasks it sent as away. While a
// task is away, the board refuses to change it (RefuseAway).
type TaskHandoffService struct {
	db   *store.Store
	opts TaskHandoffOptions
	// returning holds a *sync.Mutex per handoff being brought home.
	returning sync.Map
}

func NewTaskHandoffService(db *store.Store, opts TaskHandoffOptions) *TaskHandoffService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &TaskHandoffService{db: db, opts: opts}
}

// HandOff hands a task to a worker. It checks the task can travel and the
// worker can take it (reachable, new enough, with the sandbox) before
// anything is sent, sends the Handoff call, and records the task as away.
// A call the worker never answered leaves the task here, not away; handing
// it to the same worker again reuses the handoff ID, so a worker that did
// get the first call makes no second card.
func (s *TaskHandoffService) HandOff(ctx context.Context, req TaskHandoffRequest) (AwayTask, error) {
	if s.opts.Workers == nil {
		return AwayTask{}, errors.New("this Legato has no workers")
	}
	if req.Kind == "" {
		req.Kind = HandoffNewBranch
	}
	req.Repo = strings.TrimSpace(req.Repo)
	req.Branch = strings.TrimSpace(req.Branch)
	switch {
	case req.Purpose != HandoffMakeChange && req.Purpose != HandoffFindOut:
		return AwayTask{}, fmt.Errorf("%w: the purpose is %s or %s", ErrHandoffInput, HandoffMakeChange, HandoffFindOut)
	case req.Kind == HandoffPushedBranch && req.Branch == "":
		return AwayTask{}, fmt.Errorf("%w: name the pushed branch the work starts from", ErrHandoffInput)
	case req.Kind == HandoffPushedBranch && !stackgraph.ValidBranchName(req.Branch):
		return AwayTask{}, fmt.Errorf("%w: %q is not a branch name", ErrHandoffInput, req.Branch)
	case req.Kind != HandoffNewBranch && req.Kind != HandoffPushedBranch:
		return AwayTask{}, fmt.Errorf("%w: the work starts on %s or %s", ErrHandoffInput, HandoffNewBranch, HandoffPushedBranch)
	case !strings.Contains(req.Repo, "/"):
		return AwayTask{}, fmt.Errorf("%w: name the repository as owner/name", ErrHandoffInput)
	}
	task, err := s.db.GetTask(ctx, req.TaskID)
	if errors.Is(err, store.ErrNotFound) {
		return AwayTask{}, fmt.Errorf("%w: no task %s", ErrHandoffInput, req.TaskID)
	}
	if err != nil {
		return AwayTask{}, err
	}
	switch {
	case task.ArchivedAt != nil:
		return AwayTask{}, fmt.Errorf("%w: task %s is archived", ErrHandoffInput, task.ID)
	case task.Ephemeral:
		return AwayTask{}, fmt.Errorf("%w: task %s is not on the board", ErrHandoffInput, task.ID)
	case task.Provider != nil && (task.RemoteID == nil || *task.RemoteID == ""):
		return AwayTask{}, fmt.Errorf("%w: task %s has no %s key to hand off with", ErrHandoffInput, task.ID, TicketProviderLabel(*task.Provider))
	}
	if err := RefuseAway(ctx, s.db, task.ID); err != nil {
		return AwayTask{}, err
	}
	if _, err := s.db.GetAgentSessionByTaskID(ctx, task.ID); err == nil {
		return AwayTask{}, fmt.Errorf("%w: stop task %s's agent before handing it off", ErrHandoffAgentRunning, task.ID)
	} else if !errors.Is(err, store.ErrNotFound) {
		return AwayTask{}, err
	}

	client, err := s.opts.Workers.Client(ctx, req.Worker)
	if err != nil {
		return AwayTask{}, err
	}
	name := client.Worker.Name
	meta, err := client.Meta(ctx)
	if errors.Is(err, ErrWorkerUnreachable) {
		s.opts.Workers.MarkUnreachable(ctx, client.Worker.InstanceID)
		return AwayTask{}, &handoffWorkerError{ErrWorkerUnreachable, fmt.Sprintf("%s did not answer, so nothing was sent", name)}
	}
	if err != nil {
		return AwayTask{}, refusedBy(name, err)
	}
	if meta.Handoff < HandoffProtocolVersion {
		return AwayTask{}, &handoffWorkerError{ErrWorkerTooOld, fmt.Sprintf("update the worker: %s runs Legato %s, which cannot take a handoff from this one", name, orUnknown(meta.Version))}
	}
	if meta.Sandbox == nil || !*meta.Sandbox {
		return AwayTask{}, &handoffWorkerError{ErrWorkerRefused, fmt.Sprintf("%s cannot run agents in Legato's sandbox, which a handed-off agent needs", name)}
	}
	// Review passes turned off, to a worker that runs none, is what the
	// worker does anyway: the override is left out.
	if req.Review != nil && meta.ReviewPasses == nil && req.Review.OnlyOff() {
		req.Review = nil
	}
	if req.Review != nil && meta.ReviewPasses == nil {
		return AwayTask{}, &handoffWorkerError{ErrWorkerRefused, fmt.Sprintf("%s does not run review passes", name)}
	}

	notes, err := s.db.ListTaskNotes(ctx, task.ID)
	if err != nil {
		return AwayTask{}, err
	}
	now := s.opts.Now().UTC().Format(time.RFC3339)
	branch := HandoffBranchBase(task.ID)
	if req.Kind == HandoffPushedBranch {
		branch = req.Branch
	}
	row := store.AwayTask{TaskID: task.ID, WorkerInstanceID: client.Worker.InstanceID, State: store.AwaySending, Purpose: req.Purpose,
		Kind: req.Kind, Repo: req.Repo, Branch: branch, AgentKind: req.AgentKind, LeftColumn: task.Status,
		CreatedAt: now, UpdatedAt: now}
	// An earlier attempt to this worker that was never answered: its ID
	// again, so the worker answers with the card it made, if it made one.
	if earlier, err := s.db.OpenAwayTask(ctx, task.ID); err == nil && earlier.State == store.AwaySending && earlier.WorkerInstanceID == row.WorkerInstanceID {
		row.HandoffID = earlier.HandoffID
		row.CreatedAt = earlier.CreatedAt
		if err := s.db.DeleteAwayTask(ctx, earlier.HandoffID); err != nil {
			return AwayTask{}, err
		}
	} else if err == nil {
		// A handoff to another worker that was never answered is given up.
		if err := s.db.DeleteAwayTask(ctx, earlier.HandoffID); err != nil {
			return AwayTask{}, err
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return AwayTask{}, err
	}
	if row.HandoffID == "" {
		row.HandoffID = newHandoffID()
	}
	if err := s.db.InsertAwayTask(ctx, row); errors.Is(err, store.ErrConflict) {
		return AwayTask{}, fmt.Errorf("%w: task %s is being handed off already", ErrHandoffInput, task.ID)
	} else if err != nil {
		return AwayTask{}, err
	}

	call := HandoffCall{HandoffID: row.HandoffID, MainTaskID: task.ID, Title: task.Title, Details: task.DescriptionMD, Notes: []HandoffCallNote{},
		Repo: row.Repo, Purpose: row.Purpose, Kind: row.Kind, Branch: row.Branch, AgentKind: row.AgentKind}
	for _, n := range notes {
		call.Notes = append(call.Notes, HandoffCallNote{ID: n.ID, Body: n.Body, CreatedAt: n.CreatedAt})
	}
	call.Ticket = handoffTicket(task)
	call.Review = req.Review
	var accepted HandoffAccepted
	if err := client.Call(ctx, http.MethodPost, HandoffCallPath, call, &accepted); err != nil {
		if errors.Is(err, ErrWorkerUnreachable) {
			s.opts.Workers.MarkUnreachable(ctx, client.Worker.InstanceID)
			return AwayTask{}, &handoffWorkerError{ErrWorkerUnreachable, fmt.Sprintf("%s did not answer; handing the task to %s again is safe", name, name)}
		}
		// The worker answered and made nothing.
		_ = s.db.DeleteAwayTask(ctx, row.HandoffID)
		return AwayTask{}, refusedBy(name, err)
	}
	if accepted.WorkerInstanceID != client.Worker.InstanceID || accepted.WorkerTaskID == "" {
		_ = s.db.DeleteAwayTask(ctx, row.HandoffID)
		return AwayTask{}, &handoffWorkerError{ErrWorkerRefused, fmt.Sprintf("%s answers as %s, not %s", name, orUnknown(accepted.WorkerInstanceID), client.Worker.InstanceID)}
	}
	at := s.opts.Now().UTC().Format(time.RFC3339)
	if err := s.db.SetAwayTaskAway(ctx, row.HandoffID, accepted.WorkerTaskID, at); err != nil {
		return AwayTask{}, err
	}
	// The state it was accepted in is the first thing known of it.
	summary, _ := json.Marshal(HandoffSummary{HandoffID: row.HandoffID, WorkerTaskID: accepted.WorkerTaskID, State: accepted.State, Failure: accepted.Failure})
	// A row heard on the worker's connection meanwhile is newer.
	if err := s.db.SetAwayTaskSummaryIfNone(ctx, row.HandoffID, string(summary), at); err != nil {
		return AwayTask{}, err
	}
	if s.opts.Bus != nil {
		s.opts.Bus.Publish(events.Event{Type: events.EventCardsRefreshed, At: time.Now()})
	}
	away, err := s.Away(ctx, task.ID)
	if err != nil {
		return AwayTask{}, err
	}
	return *away, nil
}

// handoffTicket is what travels of a provider-backed task's ticket, whatever
// its provider: its key, its text as last synced and its address. Its
// Legato details travel as the call's details, and the link to the provider
// and its sync stay here. A copy handed on names the provider it came from.
// Nil for a local task.
func handoffTicket(task *store.Task) *HandoffTicket {
	if task.Provider == nil || task.RemoteID == nil {
		return nil
	}
	provider := *task.Provider
	if from := HandoffCopyOf(provider); from != "" {
		provider = from
	}
	return &HandoffTicket{Provider: provider, Key: *task.RemoteID, Text: task.RemoteDescriptionMD, URL: parseRemoteMeta(task.RemoteMeta).URL}
}

// Away is a task's handoff while it is away, or nil when it is not away.
func (s *TaskHandoffService) Away(ctx context.Context, taskID string) (*AwayTask, error) {
	row, err := s.db.OpenAwayTask(ctx, taskID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && row.State != store.AwayAway) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	away := awayTaskFromRow(*row)
	return &away, nil
}

// ListAway lists the tasks away on workers, oldest handoff first.
func (s *TaskHandoffService) ListAway(ctx context.Context) ([]AwayTask, error) {
	rows, err := s.db.ListAwayTasks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AwayTask, 0, len(rows))
	for _, r := range rows {
		out = append(out, awayTaskFromRow(r))
	}
	return out, nil
}

// handoffWorkerError is a handoff that failed at the worker, in words that
// name it; errors.Is matches Kind (ErrWorkerUnreachable, ErrWorkerRefused,
// ErrWorkerTooOld).
type handoffWorkerError struct {
	Kind    error
	Message string
}

func (e *handoffWorkerError) Error() string { return e.Message }
func (e *handoffWorkerError) Unwrap() error { return e.Kind }

// refusedBy is a worker's refusal of a handoff, in its own words.
func refusedBy(name string, err error) error {
	var refusal *WorkerRefusal
	if errors.As(err, &refusal) {
		return &handoffWorkerError{ErrWorkerRefused, fmt.Sprintf("%s refused the handoff: %s", name, refusal.Message)}
	}
	return err
}

func newHandoffID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "ho-" + hex.EncodeToString(b)
}
