package service

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/cpave3/legato/internal/engine/store"
)

// Roles of a review loop's related agent, as the other side sees it.
const (
	// RelatedRoleReviewer is the loop's reviewer, seen by its author.
	RelatedRoleReviewer = "reviewer"
	// RelatedRoleAuthor is the loop's author agent, seen by its reviewer.
	RelatedRoleAuthor = "author"
	// RelatedRoleLinkedTask is the board task of a run's pull request, seen
	// by its reviewer when no agent authors the loop.
	RelatedRoleLinkedTask = "linked_task"
)

// Kinds of review loop a related agent comes from.
const (
	RelatedLoopHandoff  = "handoff"
	RelatedLoopWorkflow = "workflow"
)

// Statuses of a related agent.
const (
	RelatedAgentRunning = "running"
	RelatedAgentEnded   = "ended"
	// RelatedAgentNone is a related task with no agent in its slot.
	RelatedAgentNone = "none"
)

// RelatedAgent is an agent on the other side of a review loop from the agent
// asked about: its reviewer, its author, or the run's linked board task.
type RelatedAgent struct {
	// AgentID is the agent to open (<task> for a main slot, <task>_<slot>
	// for a named one); "" for a linked task with no agent.
	AgentID string `json:"agent_id,omitempty"`
	TaskID  string `json:"task_id"`
	Slot    string `json:"slot"`
	Title   string `json:"title"`
	Role    string `json:"role"`
	// Pass is the loop's pass, for a reviewer.
	Pass      int    `json:"pass,omitempty"`
	Status    string `json:"status"`
	Activity  string `json:"activity,omitempty"`
	AgentKind string `json:"agent_kind,omitempty"`
	// Loop is the loop the two agents share.
	Loop RelatedLoop `json:"loop"`
}

// RelatedLoop is a review loop as a related agent row names it.
type RelatedLoop struct {
	Kind string `json:"kind"`
	// TaskID keys the loop: the handed-off card, or the workflow run's task.
	TaskID string `json:"task_id"`
	// Phase is the loop's state (ReviewPhase*, "author" for the author's
	// turn).
	Phase string `json:"phase"`
	RunID string `json:"run_id,omitempty"`
	Link  string `json:"link"`
}

// RelatedAgentsService finds the agents on the other side of the review
// loops an agent takes part in. The link comes from the loops' own records
// (the loop record a handoff's card or a run's task hosts, and the run's
// waiting github.pr.review_passes step), never from folders: the reviewer works in
// a clone of its own.
type RelatedAgentsService struct {
	db *store.Store
}

// NewRelatedAgentsService reads loops and agents from db.
func NewRelatedAgentsService(db *store.Store) *RelatedAgentsService {
	return &RelatedAgentsService{db: db}
}

// RelatedAgents are the agents related to agentID (an agent ID) through an
// open review loop: a reviewer's author (or linked task), an author's
// reviewers. A loop drops out once it has ended: the handoff closed or the
// loop ended, the run's review step no longer waiting.
func (s *RelatedAgentsService) RelatedAgents(ctx context.Context, agentID string) ([]RelatedAgent, error) {
	taskID, slot := SplitAgentID(agentID)
	runLoops, err := s.openRunLoops(ctx)
	if err != nil {
		return nil, err
	}
	var out []RelatedAgent
	if slot == SlotReviewer {
		if run, ok := runLoops[taskID]; ok {
			return s.runAuthor(ctx, taskID, run)
		}
		loop, err := s.openHandoffLoop(ctx, taskID)
		if err != nil || loop == nil {
			return nil, err
		}
		row, err := s.agentRow(ctx, taskID, store.SlotMain, RelatedRoleAuthor)
		if err != nil {
			return nil, err
		}
		row.Loop = handoffRelatedLoop(taskID, loop)
		return append(out, row), nil
	}
	if IsNamedSlot(slot) {
		return nil, nil
	}
	if _, isRun := runLoops[taskID]; !isRun {
		loop, err := s.openHandoffLoop(ctx, taskID)
		if err != nil {
			return nil, err
		}
		if loop != nil {
			if row, ok, err := s.reviewerRow(ctx, taskID, loop); err != nil {
				return nil, err
			} else if ok {
				row.Loop = handoffRelatedLoop(taskID, loop)
				out = append(out, row)
			}
		}
	}
	for _, run := range sortedRunLoops(runLoops) {
		if run.run.AuthorTask != taskID && (run.run.AuthorTask != "" || run.run.LinkedTask != taskID) {
			continue
		}
		loop, err := s.db.GetHandoffReview(ctx, run.taskID)
		if err != nil {
			return nil, err
		}
		if loop == nil || loop.Phase == ReviewPhaseEnded {
			continue
		}
		row, ok, err := s.reviewerRow(ctx, run.taskID, loop)
		if err != nil {
			return nil, err
		}
		if ok {
			row.Loop = runRelatedLoop(run.taskID, run.run, loop)
			out = append(out, row)
		}
	}
	return out, nil
}

// runAuthor is what a run's reviewer relates to: its author's main slot, or
// the linked board task when no agent authors the loop, or nothing.
func (s *RelatedAgentsService) runAuthor(ctx context.Context, taskID string, run *reviewRun) ([]RelatedAgent, error) {
	loop, err := s.db.GetHandoffReview(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if loop == nil || loop.Phase == ReviewPhaseEnded {
		return nil, nil
	}
	var row RelatedAgent
	switch {
	case run.AuthorTask != "":
		if row, err = s.agentRow(ctx, run.AuthorTask, store.SlotMain, RelatedRoleAuthor); err != nil {
			return nil, err
		}
	case run.LinkedTask != "":
		task, err := s.db.GetTask(ctx, run.LinkedTask)
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		row = RelatedAgent{TaskID: task.ID, Slot: store.SlotMain, Title: task.Title, Role: RelatedRoleLinkedTask, Status: RelatedAgentNone}
	default:
		return nil, nil
	}
	row.Loop = runRelatedLoop(taskID, run, loop)
	return []RelatedAgent{row}, nil
}

// reviewerRow is a loop's reviewer: the reviewer slot's newest agent, which
// stays (marked ended) while the loop is open. None before a first pass.
func (s *RelatedAgentsService) reviewerRow(ctx context.Context, taskID string, loop *store.HandoffReview) (RelatedAgent, bool, error) {
	row, err := s.agentRow(ctx, taskID, SlotReviewer, RelatedRoleReviewer)
	if err != nil {
		return RelatedAgent{}, false, err
	}
	if row.Status == RelatedAgentNone {
		if loop.Pass == 0 {
			return RelatedAgent{}, false, nil
		}
		// A reviewer that ran a pass has ended once its session is gone
		// (a stopped agent's row is removed).
		row.Status = RelatedAgentEnded
	}
	row.Pass = loop.Pass
	return row, true, nil
}

// agentRow is a task slot's newest agent as a row.
func (s *RelatedAgentsService) agentRow(ctx context.Context, taskID, slot, role string) (RelatedAgent, error) {
	row := RelatedAgent{TaskID: taskID, Slot: slot, Role: role, AgentID: AgentSlotKey(taskID, slot), Status: RelatedAgentNone}
	if task, err := s.db.GetTask(ctx, taskID); err == nil {
		row.Title = task.Title
	} else if !errors.Is(err, store.ErrNotFound) {
		return RelatedAgent{}, err
	}
	session, err := s.db.LatestAgentSessionInSlot(ctx, taskID, slot)
	if errors.Is(err, store.ErrNotFound) {
		return row, nil
	}
	if err != nil {
		return RelatedAgent{}, err
	}
	row.AgentKind = session.AgentKind
	if session.Status == "running" {
		row.Status, row.Activity = RelatedAgentRunning, session.Activity
	} else {
		row.Status = RelatedAgentEnded
	}
	return row, nil
}

// openHandoffLoop is a card's review loop while its handoff is open and the
// loop has not ended; nil when there is none.
func (s *RelatedAgentsService) openHandoffLoop(ctx context.Context, taskID string) (*store.HandoffReview, error) {
	if _, err := s.db.OpenHandoffForTask(ctx, taskID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	loop, err := s.db.GetHandoffReview(ctx, taskID)
	if err != nil || loop == nil {
		return nil, err
	}
	if loop.Phase == ReviewPhaseEnded || loop.Phase == ReviewPhaseSkipped {
		return nil, nil
	}
	return loop, nil
}

type openRunLoop struct {
	taskID string
	run    *reviewRun
}

// openRunLoops are the runs whose review step waits on its loop, by the
// run's task.
func (s *RelatedAgentsService) openRunLoops(ctx context.Context) (map[string]*reviewRun, error) {
	attempts, err := s.db.ListWaitingWorkflowAttemptsByExecutor(ctx, ReviewPassesStepType)
	if err != nil {
		return nil, err
	}
	out := map[string]*reviewRun{}
	for _, attempt := range attempts {
		run, err := readReviewRun(ctx, s.db, attempt.TaskID)
		if err != nil {
			continue
		}
		out[attempt.TaskID] = run
	}
	return out, nil
}

func sortedRunLoops(loops map[string]*reviewRun) []openRunLoop {
	out := make([]openRunLoop, 0, len(loops))
	for taskID, run := range loops {
		out = append(out, openRunLoop{taskID: taskID, run: run})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].taskID < out[j].taskID })
	return out
}

func handoffRelatedLoop(taskID string, loop *store.HandoffReview) RelatedLoop {
	return RelatedLoop{Kind: RelatedLoopHandoff, TaskID: taskID, Phase: relatedPhase(loop.Phase), Link: "/board?highlight_task=" + taskID}
}

func runRelatedLoop(taskID string, run *reviewRun, loop *store.HandoffReview) RelatedLoop {
	return RelatedLoop{Kind: RelatedLoopWorkflow, TaskID: taskID, Phase: relatedPhase(loop.Phase), RunID: run.RunID,
		Link: fmt.Sprintf("/workflows/runs/%s", run.RunID)}
}

// relatedPhase names the author's turn, which the loop record keeps as "".
func relatedPhase(phase string) string {
	if phase == ReviewPhaseAuthor {
		return "author"
	}
	return phase
}
