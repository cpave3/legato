package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
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
	TaskID    string
	Worker    string
	Purpose   string
	Kind      string // HandoffNewBranch when empty
	Repo      string
	AgentKind string
}

// AwayTask is a task of this main away on a worker.
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
}

func awayTaskFromRow(a store.AwayTask) AwayTask {
	return AwayTask{TaskID: a.TaskID, WorkerInstanceID: a.WorkerInstanceID, WorkerName: a.WorkerName, HandoffID: a.HandoffID,
		WorkerTaskID: a.WorkerTaskID, Purpose: a.Purpose, Kind: a.Kind, Repo: a.Repo, Branch: a.Branch, AgentKind: a.AgentKind,
		LeftColumn: a.LeftColumn, HandedOffAt: a.CreatedAt}
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
	switch {
	case req.Purpose != HandoffMakeChange && req.Purpose != HandoffFindOut:
		return AwayTask{}, fmt.Errorf("%w: the purpose is %s or %s", ErrHandoffInput, HandoffMakeChange, HandoffFindOut)
	case req.Kind == HandoffPushedBranch:
		return AwayTask{}, fmt.Errorf("%w: starting from a pushed branch is not supported yet", ErrHandoffInput)
	case req.Kind != HandoffNewBranch:
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
	case task.Provider != nil:
		return AwayTask{}, fmt.Errorf("%w: handing off a task from %s is not supported yet", ErrHandoffInput, *task.Provider)
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
	meta, err := client.Meta(ctx)
	if err != nil {
		return AwayTask{}, err
	}
	if meta.Handoff < HandoffProtocolVersion {
		return AwayTask{}, fmt.Errorf("%w: %s cannot take a handoff (it runs Legato %s)", ErrWorkerTooOld, client.Worker.Name, orUnknown(meta.Version))
	}
	if meta.Sandbox == nil || !*meta.Sandbox {
		return AwayTask{}, fmt.Errorf("%w: %s cannot run agents in Legato's sandbox, which a handed-off agent needs", ErrWorkerRefused, client.Worker.Name)
	}

	notes, err := s.db.ListTaskNotes(ctx, task.ID)
	if err != nil {
		return AwayTask{}, err
	}
	now := s.opts.Now().UTC().Format(time.RFC3339)
	row := store.AwayTask{TaskID: task.ID, WorkerInstanceID: client.Worker.InstanceID, State: store.AwaySending, Purpose: req.Purpose,
		Kind: req.Kind, Repo: req.Repo, Branch: "legato/" + strings.ToLower(task.ID), AgentKind: req.AgentKind, LeftColumn: task.Status,
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
	var accepted HandoffAccepted
	if err := client.Call(ctx, http.MethodPost, HandoffCallPath, call, &accepted); err != nil {
		if !errors.Is(err, ErrWorkerUnreachable) {
			// The worker answered and made nothing.
			_ = s.db.DeleteAwayTask(ctx, row.HandoffID)
		}
		return AwayTask{}, err
	}
	if accepted.WorkerInstanceID != client.Worker.InstanceID || accepted.WorkerTaskID == "" {
		_ = s.db.DeleteAwayTask(ctx, row.HandoffID)
		return AwayTask{}, fmt.Errorf("%w: it answers as %s, not %s", ErrWorkerRefused, orUnknown(accepted.WorkerInstanceID), client.Worker.InstanceID)
	}
	if err := s.db.SetAwayTaskAway(ctx, row.HandoffID, accepted.WorkerTaskID, s.opts.Now().UTC().Format(time.RFC3339)); err != nil {
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

func newHandoffID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "ho-" + hex.EncodeToString(b)
}
