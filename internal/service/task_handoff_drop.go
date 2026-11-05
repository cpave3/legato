package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// A worker's side of Drop, Leave and unpairing a main. Drop is the main
// taking a task back without its results: the worker stops the agent,
// archives the card with a note and closes the handoff. Leave and Unpair
// are the same: the main's token is revoked and its open handoffs close,
// their cards left as ordinary cards with their agents running and a note.
// See docs/ai/integrations.md § Take back, Drop and Leave.

// Drop closes a main's handoff the main took back: the agent stops and the
// card is archived with a note. Repeating it changes nothing.
func (s *HandoffWorkerService) Drop(ctx context.Context, mainInstanceID, handoffID string) (HandoffDropped, error) {
	h, err := s.db.GetHandoff(ctx, mainInstanceID, handoffID)
	if errors.Is(err, store.ErrNotFound) {
		return HandoffDropped{}, handoffErr("not_found", "no handoff %s", handoffID)
	}
	if err != nil {
		return HandoffDropped{}, err
	}
	id, err := InstanceID(ctx, s.db)
	if err != nil {
		return HandoffDropped{}, err
	}
	if h.ClosedAt == nil {
		s.stopAgent(ctx, h)
		note := store.TaskNote{ID: workflowID("tn-"), Source: "local",
			Body: fmt.Sprintf("%s took this task back without its results, so it was archived here.", mainNameOf(h))}
		closed, err := s.db.CloseHandoffWithNote(ctx, mainInstanceID, handoffID, note, true, s.opts.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return HandoffDropped{}, err
		}
		if closed {
			s.publish(events.EventHandoffChanged, h.TaskID)
			s.publish(events.EventCardsRefreshed, nil)
		}
	}
	return HandoffDropped{HandoffID: h.HandoffID, WorkerInstanceID: id}, nil
}

// UnpairMain unpairs a main instance, from Settings › Main instances or
// the main's own Leave: its token stops working at once, and each of its
// open handoffs closes, its card left here as an ordinary card, its agent
// running, with a note that the main was unpaired. It returns how many
// handoffs it closed; ErrMainNotPaired when the main was neither paired
// nor had any open.
func (s *HandoffWorkerService) UnpairMain(ctx context.Context, mainInstanceID string) (int, error) {
	revokeErr := s.db.RevokeMainIntegrationToken(ctx, mainInstanceID)
	if revokeErr != nil && !errors.Is(revokeErr, store.ErrNotFound) {
		return 0, revokeErr
	}
	closed, err := s.db.CloseMainHandoffs(ctx, mainInstanceID, func(h store.Handoff) store.TaskNote {
		return store.TaskNote{ID: workflowID("tn-"), Source: "local",
			Body: fmt.Sprintf("%s was unpaired from this Legato, so this task is an ordinary card here now: it no longer goes back to %s.", mainNameOf(&h), mainNameOf(&h))}
	}, s.opts.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	if revokeErr != nil && len(closed) == 0 {
		return 0, fmt.Errorf("%w: %s", ErrMainNotPaired, mainInstanceID)
	}
	for _, h := range closed {
		s.publish(events.EventHandoffChanged, h.TaskID)
	}
	if len(closed) > 0 {
		s.publish(events.EventCardsRefreshed, nil)
	}
	return len(closed), nil
}

// Leave is a main's Leave call: it unpairs itself.
func (s *HandoffWorkerService) Leave(ctx context.Context, mainInstanceID string) (HandoffLeft, error) {
	id, err := InstanceID(ctx, s.db)
	if err != nil {
		return HandoffLeft{}, err
	}
	closed, err := s.UnpairMain(ctx, mainInstanceID)
	if err != nil && !errors.Is(err, ErrMainNotPaired) {
		return HandoffLeft{}, err
	}
	return HandoffLeft{WorkerInstanceID: id, Closed: closed}, nil
}

func mainNameOf(h *store.Handoff) string {
	if h.MainName != "" {
		return h.MainName
	}
	return h.MainInstanceID
}
