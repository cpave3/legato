package main

import (
	"context"
	"log"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/transcript"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// newSessionHistory is the session history, kept for agents.history.keep.
func newSessionHistory(cfg *config.Config, db *store.Store) *service.SessionHistoryService {
	return service.NewSessionHistoryService(db, sessionHistoryOptions(cfg, db, nil))
}

func sessionHistoryOptions(cfg *config.Config, db *store.Store, agents service.AgentService) service.SessionHistoryOptions {
	keep, forever := cfg.Agents.History.KeepDuration()
	if forever {
		keep = 0
	}
	opts := service.SessionHistoryOptions{
		Keep: keep,
		// Sandboxed agents share the host's ~/.claude and ~/.codex at the
		// same paths, so every transcript is read from the host.
		Transcripts: &transcript.Reader{},
		// On a main, a task handed to a worker is the worker's to restore.
		Away: func(ctx context.Context, taskID string) bool {
			away, err := db.OpenAwayTask(ctx, taskID)
			return err == nil && away.State == store.AwayAway
		},
	}
	if adapters, ok := agents.(interface {
		AdapterFor(string) service.AIToolAdapter
	}); ok {
		opts.ToolConfigured = func(kind string) bool {
			adapter := adapters.AdapterFor(kind)
			return adapter != nil && adapter.Name() == kind
		}
	}
	return opts
}

// wireSessionHistory serves the session history on a web server: listed,
// forgotten, and restored through the agent service or, for a Reeve
// conversation, the Reeve.
func wireSessionHistory(srv *server.Server, cfg *config.Config, db *store.Store, agents service.AgentService, board service.BoardService, reeve *service.ReeveService) {
	opts := sessionHistoryOptions(cfg, db, agents)
	if board != nil {
		opts.NewTask = service.BoardNewTask(board, db)
	}
	if restorer, ok := agents.(service.ConversationRestorer); ok {
		opts.Restorer = restorer
	}
	if reeve != nil {
		opts.ReeveResume = func(ctx context.Context, taskID string) error {
			_, err := reeve.Resume(ctx, taskID, service.ReeveStart{StartedBy: "session history"})
			return err
		}
	}
	srv.SetSessionHistory(service.NewSessionHistoryService(db, opts))
}

// sweepSessionHistory removes the session history's old entries at the
// start and once a day, while this process serves the broker, so one
// Legato does it.
func (rt *approvalRuntime) sweepSessionHistory(history *service.SessionHistoryService) {
	if rt == nil || history == nil || rt.brokerOwner == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	sweep := func(ctx context.Context) {
		removed, err := history.Sweep(ctx)
		if err != nil {
			log.Printf("session history: sweeping old entries: %v", err)
		} else if removed > 0 {
			log.Printf("session history: removed %d entries past agents.history.keep", removed)
		}
	}
	go func() {
		if rt.brokerOwner() {
			sweep(ctx)
		}
		whileBrokerOwner(ctx, rt.brokerOwner, service.SessionHistorySweepEvery, sweep)
	}()
	stop := rt.stop
	rt.stop = func() {
		cancel()
		stop()
	}
}

// captureConversationText keeps the first prompt and the last message of
// each conversation as its turns end, in the process that serves the
// broker.
func (rt *approvalRuntime) captureConversationText(db *store.Store, bus *events.Bus, agents service.AgentService) {
	reader, ok := agents.(service.AgentConversationReader)
	if rt == nil || !ok || bus == nil || rt.brokerOwner == nil {
		return
	}
	capture := service.NewConversationTextCapture(db, reader, service.ConversationTextOptions{Owner: rt.brokerOwner})
	stopCapture := capture.StartEventLoop(context.Background(), bus)
	stop := rt.stop
	rt.stop = func() {
		stopCapture()
		stop()
	}
}

// recoverSessionHistory looks for the conversations of past runs that never
// reported one, at the start and after each agent ends, while this process
// serves the broker.
func (rt *approvalRuntime) recoverSessionHistory(history *service.SessionHistoryService, bus *events.Bus) {
	if rt == nil || history == nil || rt.brokerOwner == nil {
		return
	}
	stopRecovery := history.StartRecovery(context.Background(), bus, rt.brokerOwner, log.Printf)
	stop := rt.stop
	rt.stop = func() {
		stopRecovery()
		stop()
	}
}
