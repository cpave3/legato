package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// A worker's side of Return and recall, which are the same two calls. Step
// 1 stops the handoff's agent (or keeps a pending one from starting),
// freezes its card and marks it returning, and answers with what the main
// takes back. Step 2 closes the handoff and archives the card; its job
// clone is left to the sweep. The worker takes both in any state, and
// either may be repeated. See docs/ai/integrations.md § Return and recall.

// HandoffReturnPath is the worker's route for step 1 of a return or recall
// of a handoff, HandoffArchivePath the one for step 2.
func HandoffReturnPath(handoffID string) string {
	return HandoffCallPath + "/" + url.PathEscape(handoffID) + "/return"
}

func HandoffArchivePath(handoffID string) string {
	return HandoffCallPath + "/" + url.PathEscape(handoffID) + "/archive"
}

// HandoffReturn is the worker's answer to step 1: the task as its card
// has it now. Notes are the worker's own, with their IDs (never the
// main's); Branches are those pushed, in stack order; Stack is Staccato's
// st graph import document, when the task's clone keeps an importable
// stack.
type HandoffReturn struct {
	HandoffID        string            `json:"handoff_id"`
	WorkerTaskID     string            `json:"worker_task_id"`
	WorkerInstanceID string            `json:"worker_instance_id"`
	State            string            `json:"state"`
	Title            string            `json:"title"`
	Details          string            `json:"details"`
	Column           string            `json:"column"`
	Notes            []HandoffCallNote `json:"notes"`
	Repo             string            `json:"repo"`
	Branches         []string          `json:"branches"`
	Stack            json.RawMessage   `json:"stack,omitempty"`
}

// HandoffArchived is the worker's answer to step 2.
type HandoffArchived struct {
	HandoffID        string `json:"handoff_id"`
	WorkerInstanceID string `json:"worker_instance_id"`
}

// HandoffStacks reads the Staccato stacks of a worker's cards: the task
// stack service.
type HandoffStacks interface {
	List(ctx context.Context, taskID string) ([]TaskStack, error)
	Document(ctx context.Context, taskID string, cloneID int64) (TaskStackDocument, error)
}

// Return is step 1 of a main's return or recall of a handoff.
func (s *HandoffWorkerService) Return(ctx context.Context, mainInstanceID, handoffID string) (HandoffReturn, error) {
	h, err := s.db.MarkHandoffReturning(ctx, mainInstanceID, handoffID, s.opts.Now().UTC().Format(time.RFC3339))
	if errors.Is(err, store.ErrNotFound) {
		return HandoffReturn{}, handoffErr("not_found", "no handoff %s", handoffID)
	}
	if err != nil {
		return HandoffReturn{}, err
	}
	if h.ClosedAt == nil {
		s.stopAgent(ctx, h)
		s.publish(events.EventHandoffChanged, h.TaskID)
	}
	id, err := InstanceID(ctx, s.db)
	if err != nil {
		return HandoffReturn{}, err
	}
	task, err := s.db.GetTask(ctx, h.TaskID)
	if err != nil {
		return HandoffReturn{}, err
	}
	notes, err := s.db.ListTaskNotes(ctx, h.TaskID)
	if err != nil {
		return HandoffReturn{}, err
	}
	back := HandoffReturn{HandoffID: h.HandoffID, WorkerTaskID: h.TaskID, WorkerInstanceID: id, State: h.State, Title: task.Title,
		Details: task.DescriptionMD, Column: task.Status, Notes: []HandoffCallNote{}, Repo: h.Repo, Branches: s.pushedBranches(ctx, *h, task)}
	if back.Branches == nil {
		back.Branches = []string{}
	}
	for _, n := range notes {
		if n.Source != NoteSourceMain {
			back.Notes = append(back.Notes, HandoffCallNote{ID: n.ID, Body: n.Body, CreatedAt: n.CreatedAt})
		}
	}
	if s.opts.Reviews != nil {
		if note := s.opts.Reviews.ReviewNote(ctx, h.TaskID); note != "" {
			back.Notes = append(back.Notes, HandoffCallNote{ID: "rv-" + h.HandoffID, Body: note, CreatedAt: s.opts.Now().UTC().Format(time.RFC3339)})
		}
	}
	back.Stack = s.stackDocument(ctx, h)
	return back, nil
}

// FinishReturn is step 2: the handoff closes and its card is archived. Its
// agent is stopped first, should step 1 never have been made.
func (s *HandoffWorkerService) FinishReturn(ctx context.Context, mainInstanceID, handoffID string) (HandoffArchived, error) {
	h, err := s.db.GetHandoff(ctx, mainInstanceID, handoffID)
	if errors.Is(err, store.ErrNotFound) {
		return HandoffArchived{}, handoffErr("not_found", "no handoff %s", handoffID)
	}
	if err != nil {
		return HandoffArchived{}, err
	}
	id, err := InstanceID(ctx, s.db)
	if err != nil {
		return HandoffArchived{}, err
	}
	if h.ClosedAt == nil {
		s.stopAgent(ctx, h)
		if err := s.db.CloseHandoffAndArchive(ctx, mainInstanceID, handoffID, s.opts.Now().UTC().Format(time.RFC3339)); err != nil {
			return HandoffArchived{}, err
		}
		s.publish(events.EventHandoffChanged, h.TaskID)
		s.publish(events.EventCardsRefreshed, nil)
	}
	return HandoffArchived{HandoffID: h.HandoffID, WorkerInstanceID: id}, nil
}

// stopAgent ends a handoff's agents, if any run, in every slot.
func (s *HandoffWorkerService) stopAgent(ctx context.Context, h *store.Handoff) {
	if s.opts.Agents == nil {
		return
	}
	// Every slot: the author's and a reviewer's.
	if stopper, ok := s.opts.Agents.(TaskAgentsStopper); ok {
		if err := stopper.StopTaskAgents(ctx, h.TaskID); err != nil {
			log.Printf("handoff %s: stopping its agents: %v", h.HandoffID, err)
		}
		return
	}
	if session, err := s.opts.Agents.GetAgentSession(ctx, h.TaskID); err != nil || session == nil {
		return
	}
	if err := s.opts.Agents.KillAgent(ctx, h.TaskID); err != nil {
		log.Printf("handoff %s: stopping its agent: %v", h.HandoffID, err)
	}
}

// stackDocument is the st graph import document of the handoff's stack:
// the newest importable stack of its repository among the card's clones.
func (s *HandoffWorkerService) stackDocument(ctx context.Context, h *store.Handoff) json.RawMessage {
	if s.opts.Stacks == nil {
		return nil
	}
	stacks, err := s.opts.Stacks.List(ctx, h.TaskID)
	if err != nil {
		return nil
	}
	for i := len(stacks) - 1; i >= 0; i-- {
		st := stacks[i]
		if !st.Importable || (st.Repo != "" && !strings.EqualFold(st.Repo, h.Repo)) {
			continue
		}
		doc, err := s.opts.Stacks.Document(ctx, h.TaskID, st.CloneID)
		if err == nil && json.Valid(doc.Data) {
			return json.RawMessage(doc.Data)
		}
	}
	return nil
}

// ErrTaskReturning: the task is a card on its way back to the main that
// handed it to this worker, so it no longer changes here. It is
// ErrTaskAway, which every surface already refuses with.
type TaskReturningError struct {
	TaskID string
	Main   string
}

func (e *TaskReturningError) Error() string {
	return "task " + e.TaskID + " is going back to " + e.Main + ": it no longer changes here"
}

func (e *TaskReturningError) Is(target error) bool { return target == ErrTaskAway }

// refuseReturning is a *TaskReturningError when the task is a worker's card
// whose handoff is returning, else nil.
func refuseReturning(ctx context.Context, db *store.Store, taskID string) error {
	h, err := db.OpenHandoffForTask(ctx, taskID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && h.State != store.HandoffReturning) {
		return nil
	}
	if err != nil {
		return err
	}
	main := h.MainName
	if main == "" {
		main = h.MainInstanceID
	}
	return &TaskReturningError{TaskID: taskID, Main: main}
}
