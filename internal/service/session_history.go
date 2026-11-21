package service

import (
	"context"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// SessionHistoryOptions configures the session history.
type SessionHistoryOptions struct {
	// Keep is how long an entry is kept after it was last used
	// (agents.history.keep); 0 keeps entries until they are forgotten.
	Keep time.Duration
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// SessionHistoryService is the session history: the conversations Legato's
// agents had on this instance, kept with their runs to restore them.
type SessionHistoryService struct {
	store *store.Store
	opts  SessionHistoryOptions
}

// NewSessionHistoryService returns the history kept in db.
func NewSessionHistoryService(db *store.Store, opts SessionHistoryOptions) *SessionHistoryService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &SessionHistoryService{store: db, opts: opts}
}

// SessionHistorySweepEvery is how often the history is swept.
const SessionHistorySweepEvery = 24 * time.Hour

// Sweep removes the entries last used longer ago than Keep, never one with
// a running or interrupted run, and returns how many it removed. With Keep
// 0 it removes none.
func (h *SessionHistoryService) Sweep(ctx context.Context) (int, error) {
	if h.opts.Keep <= 0 {
		return 0, nil
	}
	return h.store.SweepAgentConversations(ctx, h.opts.Now().Add(-h.opts.Keep))
}
