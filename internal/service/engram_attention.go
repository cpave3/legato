package service

import (
	"context"

	"github.com/cpave3/legato/internal/engine/store"
)

// AttentionSignalEngramBootFailed is an agent asked to start as an engram
// that did not boot, so it runs without it.
const AttentionSignalEngramBootFailed = "engram_boot_failed"

// EngramAttentionAdapter shows the running agents whose engram did not
// boot, on their tasks.
type EngramAttentionAdapter struct {
	db   *store.Store
	name ReeveNameSource
}

// SetReeveName gives the adapter the Reeve's name, for the Reeve's item.
func (a *EngramAttentionAdapter) SetReeveName(name ReeveNameSource) { a.name = name }

func NewEngramAttentionAdapter(db *store.Store) *EngramAttentionAdapter {
	return &EngramAttentionAdapter{db: db}
}

func (a *EngramAttentionAdapter) Producer() string { return "engram" }

func (a *EngramAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	failures, err := a.db.ListEngramBootFailures(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]AttentionInput, 0, len(failures))
	for _, f := range failures {
		task := f.TaskTitle
		if task == "" {
			task = f.TaskID
		}
		// The Reeve's task is hidden: its item leads to its engram in
		// Settings.
		if kind, err := a.db.TaskKind(ctx, f.TaskID); err == nil && kind == store.TaskKindReeve {
			reeve := a.name.Of(ctx)
			items = append(items, AttentionInput{
				SignalKind: AttentionSignalEngramBootFailed, SourceKey: "engram:" + f.ExecutionID, SourceRevision: f.ExecutionID,
				TaskID: f.TaskID, Category: "agent", Urgency: "normal",
				Title:      reeve.Start() + "'s engram did not boot",
				Reason:     reeve.Start() + " started without " + f.Engram + ": it remembers nothing of earlier sessions, and keeps nothing from this one.",
				Detail:     f.Reason,
				Navigation: AttentionNavigation{Kind: "settings", Target: "reeve-engram"},
				Actions:    []AttentionAction{{ID: "open", Label: "Open " + reeve.Mid() + "'s engram", Kind: "navigate"}},
			})
			continue
		}
		items = append(items, AttentionInput{
			SignalKind: AttentionSignalEngramBootFailed, SourceKey: "engram:" + f.ExecutionID, SourceRevision: f.ExecutionID,
			TaskID: f.TaskID, Category: "agent", Urgency: "normal",
			Title:      f.Engram + " did not boot",
			Reason:     "The agent of " + task + " started without its engram, as a normal session.",
			Detail:     f.Reason,
			Navigation: AttentionNavigation{Kind: "task", Target: f.TaskID},
			Actions:    []AttentionAction{{ID: "open", Label: "Open agent", Kind: "navigate"}},
		})
	}
	return items, nil
}
