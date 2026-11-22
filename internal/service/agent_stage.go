package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

// Agent stages: where an agent place's work stands, apart from what it is
// doing now (activity) and from the card's column. "Stage" was taken in Go
// (preparing agents, review stages), so the names here say WorkStage. See
// docs/ai/agent-states.md § Stages.

// WorkStages lists the stages in the order work moves through them.
var WorkStages = []string{
	store.WorkStageExploring, store.WorkStagePlanning, store.WorkStageImplementing,
	store.WorkStageReviewing, store.WorkStageFinished,
}

// ErrInvalidWorkStage is a stage that is not one of WorkStages.
var ErrInvalidWorkStage = errors.New("invalid stage")

// ErrNoWorkStage: the place has no stage (the Reeve).
var ErrNoWorkStage = errors.New("the Reeve has no stage")

// ParseWorkStage is the stage s names, ignoring case and spaces.
func ParseWorkStage(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, stage := range WorkStages {
		if s == stage {
			return stage, nil
		}
	}
	return "", fmt.Errorf("%w %q: one of %s", ErrInvalidWorkStage, s, strings.Join(WorkStages, ", "))
}

func workStageRank(stage string) int {
	for i, s := range WorkStages {
		if s == stage {
			return i
		}
	}
	return -1
}

// The glance groups every list of agents sorts them into.
const (
	// GlanceNeedsYou: it asks (waiting), it failed, it has an open operator
	// item that is not a done note, or its task has a plan waiting on the
	// operator.
	GlanceNeedsYou = "needs_you"
	// GlanceYourTurn: idle, nothing running, its work not finished: it
	// stopped mid-work, waiting for the operator's reply.
	GlanceYourTurn = "your_turn"
	// GlanceWorking: working, or waiting on work it started (background).
	GlanceWorking = "working"
	// GlanceFinished: its stage is Finished and it is not working.
	GlanceFinished = "finished"
	// GlanceEnded: the agent is not running.
	GlanceEnded = "ended"
)

// GlanceInput is what an agent's glance group is worked out from.
type GlanceInput struct {
	// Status is the session's status (running, dead, interrupted).
	Status   string
	Activity string
	// NeedsItems counts its open operator items of kind question, action
	// or other; a done item never makes it need the operator.
	NeedsItems  int
	PlanPending bool
	Stage       string
}

// Glance is an agent's glance group. It is the one rule every surface
// sorts agents by: the web lists, the TUI, MQTT and the navigation summary.
func Glance(in GlanceInput) string {
	if in.Status != "running" {
		return GlanceEnded
	}
	switch {
	case in.Activity == store.ActivityWaiting, in.Activity == store.ActivityError, in.NeedsItems > 0, in.PlanPending:
		return GlanceNeedsYou
	case in.Activity == store.ActivityWorking, in.Activity == store.ActivityBackground:
		return GlanceWorking
	case in.Stage == store.WorkStageFinished:
		return GlanceFinished
	}
	return GlanceYourTurn
}

// Glance is the agent's glance group.
func (a AgentSession) Glance() string {
	return Glance(GlanceInput{
		Status: a.Status, Activity: a.Activity, NeedsItems: a.OpenItems - a.OpenDoneItems,
		PlanPending: a.PlanPending, Stage: a.Stage,
	})
}

// WorkStagePlace names an agent place: a task's slot, or a workflow agent's
// execution.
type WorkStagePlace = store.WorkStagePlace

// TaskPlace is the place of a task's slot ("" for main).
func TaskPlace(taskID, slot string) WorkStagePlace {
	if slot == "" {
		slot = store.SlotMain
	}
	return WorkStagePlace{TaskID: taskID, Slot: slot}
}

// PlaceOf is the place of an agent: its execution's for a workflow agent,
// else its task's slot.
func PlaceOf(a AgentSession) WorkStagePlace {
	place := TaskPlace(a.CardTaskID, a.Slot)
	if place.TaskID == "" {
		place.TaskID, _ = SplitAgentID(a.TaskID)
	}
	if a.ExecutionKind == store.ExecutionWorkflowAgent {
		place.PlaceExecutionID = a.ExecutionID
	}
	return place
}

// WorkMilestone is something that happened to the work, which moves the
// stage once, when it happens.
type WorkMilestone struct {
	Stage  string
	Reason string
	// Feedback is review feedback delivered to the author: it may move the
	// stage back (to Implementing). Every other milestone only moves it
	// forward.
	Feedback    bool
	ExecutionID string
}

// WorkStageService keeps each agent place's stage, applying the order of
// trust between the signals that set it:
//
//  1. the operator sets it (SetByOperator), any stage;
//  2. the agent reports it (Report), any stage;
//  3. milestones (Milestone) move it forward only, except feedback;
//  4. evidence of edits (Evidence) moves it to Implementing, never further,
//     and is ignored for a read-only slot (a reviewer).
//
// The Reeve has no stage: every signal about it is ignored.
type WorkStageService struct {
	store *store.Store
	bus   *events.Bus
	mu    sync.Mutex
}

// NewWorkStageService keeps stages in db and publishes each change on bus
// (nil for none).
func NewWorkStageService(db *store.Store, bus *events.Bus) *WorkStageService {
	return &WorkStageService{store: db, bus: bus}
}

// Get is a place's stage, nil when none is recorded.
func (s *WorkStageService) Get(ctx context.Context, place WorkStagePlace) (*store.WorkStage, error) {
	return s.store.GetWorkStage(ctx, place)
}

// History is a place's stage changes, newest first.
func (s *WorkStageService) History(ctx context.Context, place WorkStagePlace, limit int) ([]store.WorkStageEvent, error) {
	return s.store.ListWorkStageEvents(ctx, place, limit)
}

// PlaceOfAgent is the place of the agent an agent ID (a task ID, or
// <task>_<slot>) addresses: its running session's, else its newest one's.
func (s *WorkStageService) PlaceOfAgent(ctx context.Context, agentID string) (WorkStagePlace, string, error) {
	taskID, slot := SplitAgentID(agentID)
	place := TaskPlace(taskID, slot)
	session, err := s.store.GetAgentSessionInSlot(ctx, taskID, slot)
	if errors.Is(err, store.ErrNotFound) {
		session, err = s.store.LatestAgentSessionInSlot(ctx, taskID, slot)
	}
	if errors.Is(err, store.ErrNotFound) {
		return place, "", fmt.Errorf("no agent %s: %w", agentID, store.ErrNotFound)
	}
	if err != nil {
		return place, "", err
	}
	if session.ExecutionKind == store.ExecutionWorkflowAgent {
		place.PlaceExecutionID = session.ExecutionID
	}
	return place, session.ExecutionID, nil
}

// Start resets a place for a new conversation: Exploring, or Reviewing for
// a reviewer slot, which only ever reviews. SpawnAgent calls it unless the
// spawn resumes a conversation.
func (s *WorkStageService) Start(ctx context.Context, place WorkStagePlace, executionID string) error {
	stage := store.WorkStageExploring
	if place.Slot == SlotReviewer {
		stage = store.WorkStageReviewing
	}
	_, err := s.apply(ctx, place, store.WorkStage{
		Stage: stage, Source: store.WorkStageSourceStart, ExecutionID: executionID,
	}, func(*store.WorkStage) bool { return true })
	return err
}

// SetByOperator sets a place's stage as the operator chose it.
func (s *WorkStageService) SetByOperator(ctx context.Context, place WorkStagePlace, stage, reason string) (*store.WorkStage, error) {
	stage, err := ParseWorkStage(stage)
	if err != nil {
		return nil, err
	}
	if IsReeveTask(ctx, s.store, place.TaskID) {
		return nil, ErrNoWorkStage
	}
	executionID := ""
	if session, err := s.store.GetAgentSessionInSlot(ctx, place.TaskID, place.Slot); err == nil {
		executionID = session.ExecutionID
	}
	if _, err := s.apply(ctx, place, store.WorkStage{
		Stage: stage, Reason: strings.TrimSpace(reason), Source: store.WorkStageSourceOperator, ExecutionID: executionID,
	}, func(*store.WorkStage) bool { return true }); err != nil {
		return nil, err
	}
	return s.store.GetWorkStage(ctx, place)
}

// Report sets the stage an agent reported for itself, with its note.
func (s *WorkStageService) Report(ctx context.Context, place WorkStagePlace, executionID, stage, note string) (bool, error) {
	stage, err := ParseWorkStage(stage)
	if err != nil {
		return false, err
	}
	return s.apply(ctx, place, store.WorkStage{
		Stage: stage, Reason: strings.TrimSpace(note), Source: store.WorkStageSourceAgent, ExecutionID: executionID,
	}, func(current *store.WorkStage) bool {
		return current == nil || current.Stage != stage || current.Reason != strings.TrimSpace(note)
	})
}

// Milestone applies something that happened to the work: forward only,
// except feedback, which may move the stage back.
func (s *WorkStageService) Milestone(ctx context.Context, place WorkStagePlace, m WorkMilestone) (bool, error) {
	stage, err := ParseWorkStage(m.Stage)
	if err != nil {
		return false, err
	}
	return s.apply(ctx, place, store.WorkStage{
		Stage: stage, Reason: strings.TrimSpace(m.Reason), Source: store.WorkStageSourceMilestone, ExecutionID: m.ExecutionID,
	}, func(current *store.WorkStage) bool {
		if current == nil {
			return true
		}
		if m.Feedback {
			return current.Stage != stage || current.Reason != strings.TrimSpace(m.Reason)
		}
		return workStageRank(stage) > workStageRank(current.Stage)
	})
}

// Evidence applies an edit the agent made (or a push it made): Implementing
// from any other stage, never further. It is ignored for a read-only place
// (a reviewer slot, or readOnly set by the caller for a read-only
// execution).
func (s *WorkStageService) Evidence(ctx context.Context, place WorkStagePlace, executionID, reason string, readOnly bool) (bool, error) {
	return s.EvidenceAt(ctx, place, executionID, reason, readOnly, time.Time{})
}

// EvidenceAt is Evidence of an edit made at a time (zero for now): an edit
// from before the place reached its stage is ignored, so a replayed stream
// never counts its old events against a later stage.
func (s *WorkStageService) EvidenceAt(ctx context.Context, place WorkStagePlace, executionID, reason string, readOnly bool, at time.Time) (bool, error) {
	if readOnly || place.Slot == SlotReviewer {
		return false, nil
	}
	return s.apply(ctx, place, store.WorkStage{
		Stage: store.WorkStageImplementing, Reason: strings.TrimSpace(reason), Source: store.WorkStageSourceEvidence, ExecutionID: executionID,
	}, func(current *store.WorkStage) bool {
		if current == nil {
			return true
		}
		if current.Stage == store.WorkStageImplementing {
			return false
		}
		if since := parseStageTime(current.Since); since != nil && !at.IsZero() && at.Before(*since) {
			return false
		}
		return true
	})
}

// apply writes next when wanted says so of the place's current stage, and
// publishes the change. It reports whether it wrote.
func (s *WorkStageService) apply(ctx context.Context, place WorkStagePlace, next store.WorkStage, wanted func(*store.WorkStage) bool) (bool, error) {
	if place.TaskID == "" {
		return false, errors.New("an agent place needs a task")
	}
	if place.Slot == "" {
		place.Slot = store.SlotMain
	}
	if IsReeveTask(ctx, s.store, place.TaskID) {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.store.GetWorkStage(ctx, place)
	if err != nil {
		return false, err
	}
	if !wanted(current) {
		return false, nil
	}
	next.WorkStagePlace = place
	if _, err := s.store.SetWorkStage(ctx, next); err != nil {
		return false, err
	}
	if s.bus != nil {
		s.bus.Publish(events.Event{Type: events.EventAgentStageChanged, At: time.Now(), Payload: events.AgentStageChangedPayload{
			TaskID: next.TaskID, Slot: next.Slot, ExecutionID: next.PlaceExecutionID,
			Stage: next.Stage, Reason: next.Reason, Source: next.Source,
		}})
	}
	return true, nil
}

func parseStageTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	return &t
}

// SessionPlace is the place of a stored agent session: its execution's for
// a workflow agent, else its task's slot.
func SessionPlace(s store.AgentSession) WorkStagePlace {
	place := TaskPlace(s.TaskID, s.Slot)
	if s.ExecutionKind == store.ExecutionWorkflowAgent {
		place.PlaceExecutionID = s.ExecutionID
	}
	return place
}

// ReportWorkStage records the stage the agent of session reported for
// itself, from a process apart from the host's (legato mcp serve, legato
// agent stage, or the host's broker): it writes the stage and tells running
// Legatos over IPC, which publish it as EventAgentStageChanged
// (StageChangedFromIPC).
func ReportWorkStage(ctx context.Context, db *store.Store, session store.AgentSession, stage, note string) (bool, error) {
	place := SessionPlace(session)
	changed, err := NewWorkStageService(db, nil).Report(ctx, place, session.ExecutionID, stage, note)
	if err != nil || !changed {
		return changed, err
	}
	current, err := db.GetWorkStage(ctx, place)
	if err != nil || current == nil {
		return changed, err
	}
	ipc.Broadcast(StageIPCMessage(*current))
	return true, nil
}

// RecordWorkMilestone applies a milestone to a place from any process (a
// swarm or workflow command in an agent's session, or the host): it writes
// the stage and tells running Legatos over IPC, as ReportWorkStage does.
func RecordWorkMilestone(ctx context.Context, db *store.Store, place WorkStagePlace, m WorkMilestone) (bool, error) {
	changed, err := NewWorkStageService(db, nil).Milestone(ctx, place, m)
	if err != nil || !changed {
		return changed, err
	}
	current, err := db.GetWorkStage(ctx, place)
	if err != nil || current == nil {
		return changed, err
	}
	ipc.Broadcast(StageIPCMessage(*current))
	return true, nil
}

// StageIPCMessage tells other Legato processes of a place's new stage:
// task_id the card's task, status its slot, item_id a workflow agent's
// execution, content the stage, notes the reason, kind the source.
func StageIPCMessage(stage store.WorkStage) ipc.Message {
	return ipc.Message{
		Type: "agent_stage_changed", TaskID: stage.TaskID, Status: stage.Slot, ItemID: stage.PlaceExecutionID,
		Content: stage.Stage, Notes: stage.Reason, Kind: stage.Source,
	}
}

// StageChangedFromIPC is the event an agent_stage_changed IPC message
// stands for.
func StageChangedFromIPC(msg ipc.Message) events.AgentStageChangedPayload {
	return events.AgentStageChangedPayload{
		TaskID: msg.TaskID, Slot: msg.Status, ExecutionID: msg.ItemID,
		Stage: msg.Content, Reason: msg.Notes, Source: msg.Kind,
	}
}

// stagePrompt asks an agent to report each change in where its work stands.
const stagePrompt = "## Your stage\n\n" +
	"Legato shows the operator where your work stands: exploring (reading, searching, answering), " +
	"planning (a plan waits on the operator), implementing (changing the code), reviewing (the work is up for review) " +
	"or finished (the work is done). Legato infers most of it, but report each change yourself with Legato's " +
	"report_stage tool, or `legato agent stage <stage> --note \"…\"` when you do not have the tool. " +
	"Report finished when you have answered a question that needed no change, and reviewing when you open " +
	"a pull request or mark a review tour ready. Keep the note to a short phrase, such as \"PR #12 open\"."
