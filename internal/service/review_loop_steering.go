package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// ReviewLoopMessenger queues a message for an agent's next turn.
type ReviewLoopMessenger interface {
	QueueMessage(ctx context.Context, taskID, message string) error
}

// ReviewLoopSteering is the operator steering a loop named by its ID from the
// host, outside any session (`legato review-pass another-pass|message
// --loop`). It runs in a process without the engine's callers: today's
// task-keyed engine actions on the loop's host task, whose state the
// engine's own process picks up on its next tick. The engine's operator
// actions by loop ID (AnotherPassOn and the rest, LEG-582) need the
// callers, which only the engine's process has, so this writes the record.
type ReviewLoopSteering struct {
	DB *store.Store
	// Engine is an engine on DB for its task-keyed actions; it never ticks.
	Engine *ReviewPassService
	// Agents tells the author; only MessageAuthor needs it.
	Agents ReviewLoopMessenger
}

// AnotherPass is Another pass: a stuck loop's next pass (one past its
// limit, or a retry), or, for a workflow run's loop that waits for its
// author, the next pass now. ErrReviewNotStuck otherwise.
func (s ReviewLoopSteering) AnotherPass(ctx context.Context, loopID string) error {
	l, err := OperatorReviewLoop(ctx, s.DB, loopID)
	if err != nil {
		return err
	}
	switch {
	case l.Phase == ReviewPhaseStuck:
		return s.Engine.RecordAnotherPass(ctx, l.HostTask)
	case l.Phase == ReviewPhaseWaitingForAuthor && l.OwnerKind == store.ReviewLoopOwnerRun:
		return s.Engine.NextPass(ctx, l.HostTask)
	}
	return fmt.Errorf("review loop %s is %s: %w", loopID, l.Phase, ErrReviewNotStuck)
}

// MessageAuthor is Message the author: the operator's words, tagged, as
// the author agent's next message. A person author reads GitHub; an author
// whose agent is not running is refused, since this process cannot resume
// it as the engine's callers do.
func (s ReviewLoopSteering) MessageAuthor(ctx context.Context, loopID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("a message needs text")
	}
	l, err := OperatorReviewLoop(ctx, s.DB, loopID)
	if err != nil {
		return err
	}
	author := ReviewLoopAuthor(ctx, s.DB, l.HostTask)
	if author == "" {
		return fmt.Errorf("the author of review loop %s is a person, who reads GitHub", loopID)
	}
	if _, err := s.DB.GetAgentSessionByTaskID(ctx, author); err != nil {
		return fmt.Errorf("the author's agent on %s is not running; start it, then message it", author)
	}
	if s.Agents == nil {
		return errors.New("no agents to message on this Legato")
	}
	return s.Agents.QueueMessage(ctx, author, reviewOperatorPrefix+text)
}
