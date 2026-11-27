package service

import (
	"context"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// Bounds of one recovery pass: how many runs it looks at and for how long.
const (
	recoveryRuns     = 200
	recoveryTime     = 30 * time.Second
	recoveryLeadIn   = time.Minute
	recoveryLookBack = 180 * 24 * time.Hour
)

// Recover looks for the conversations of past runs that never reported
// one: for each ended interactive or conductor run of Claude Code or Codex
// with no run in the history, the transcripts of its tool, in the default
// storage folder and every one the history has seen (only the launch account
// storage folder for a Claude run with an account snapshot), whose folder is the
// run's and whose first line is from a minute before it began to when it
// ended. Each is kept as found by folder and time, with that run; one
// already recorded is never listed again. Each run is looked at once. It
// returns how many runs gained a conversation.
func (h *SessionHistoryService) Recover(ctx context.Context) (int, error) {
	if h.opts.Transcripts == nil {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, recoveryTime)
	defer cancel()
	since := h.opts.Now().Add(-recoveryLookBack)
	if h.opts.Keep > 0 {
		since = h.opts.Now().Add(-h.opts.Keep)
	}
	runs, err := h.store.ListUnrecordedExecutions(ctx, since, recoveryRuns)
	if err != nil {
		return 0, err
	}
	dirs := map[string][]string{}
	found := 0
	for _, run := range runs {
		if ctx.Err() != nil {
			break
		}
		storageDirs, err := h.storageDirsFor(ctx, run, dirs)
		if err != nil {
			return found, err
		}
		end := h.opts.Now()
		if run.EndedAt != nil {
			end = *run.EndedAt
		}
		seen := map[string]bool{}
		matched := false
		for _, dir := range storageDirs {
			list, err := h.opts.Transcripts.Scan(run.AgentKind, dir, run.WorkingDir, run.StartedAt.Add(-recoveryLeadIn), end)
			if err != nil {
				continue
			}
			for _, t := range list {
				if seen[t.Path] {
					continue
				}
				seen[t.Path] = true
				ok, err := h.store.RecordRecoveredConversation(ctx, run, store.RecoveredConversation{
					AgentKind: run.AgentKind, ProviderSessionID: t.SessionID, StorageDir: dir, CWD: t.CWD,
					TranscriptPath: t.Path, FirstMessage: firstOperatorText(t.FirstUser), StartedAt: t.StartedAt,
				})
				if err != nil {
					return found, err
				}
				matched = matched || ok
			}
		}
		if matched {
			found++
		}
		if err := h.store.MarkExecutionScanned(ctx, run.ID); err != nil {
			return found, err
		}
	}
	return found, nil
}

// storageDirsFor is where a run's conversations may be: a Claude Code
// run's launch account's folder only, else every folder the tool's
// conversations have been kept in, read once per tool into dirs.
func (h *SessionHistoryService) storageDirsFor(ctx context.Context, run store.UnrecordedExecution, dirs map[string][]string) ([]string, error) {
	if run.AgentKind == "claude-code" {
		identity, err := h.store.ExecutionAccount(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		if identity != nil {
			return []string{identity.ConfigDir}, nil
		}
	}
	if known, ok := dirs[run.AgentKind]; ok {
		return known, nil
	}
	list, err := h.store.ConversationStorageDirs(ctx, run.AgentKind)
	if err != nil {
		return nil, err
	}
	dirs[run.AgentKind] = list
	return list, nil
}

// StartRecovery runs a recovery pass now, off the caller's path, and again
// after each agent that ends, since a run that never reported its
// conversation may have had one. It returns the stop.
func (h *SessionHistoryService) StartRecovery(ctx context.Context, bus *events.Bus, owner func() bool, logf func(string, ...any)) func() {
	ctx, cancel := context.WithCancel(ctx)
	kick := make(chan struct{}, 1)
	kick <- struct{}{}
	var ch <-chan events.Event
	if bus != nil {
		ch = bus.Subscribe(events.EventAgentDied)
	}
	go func() {
		if bus != nil {
			defer bus.Unsubscribe(ch)
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ch:
				select {
				case kick <- struct{}{}:
				default:
				}
			case <-kick:
				if owner != nil && !owner() {
					continue
				}
				if n, err := h.Recover(ctx); err != nil {
					logf("session history: recovering conversations: %v", err)
				} else if n > 0 {
					logf("session history: found the conversations of %d past run(s) by folder and time", n)
				}
			}
		}
	}()
	return cancel
}
