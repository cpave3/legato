package service

import (
	"context"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// AttentionSignalRestartRolledBack is a restart of Legato that failed and
// was rolled back to the configuration it had before.
const AttentionSignalRestartRolledBack = "restart_rolled_back"

// RestartAttentionAdapter shows restarts rolled back in the last week.
type RestartAttentionAdapter struct {
	db  *store.Store
	now func() time.Time
}

func NewRestartAttentionAdapter(db *store.Store) *RestartAttentionAdapter {
	return &RestartAttentionAdapter{db: db, now: time.Now}
}

func (a *RestartAttentionAdapter) Producer() string { return "restart" }

func (a *RestartAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	records, err := a.db.ListRestartRecords(ctx, 50)
	if err != nil {
		return nil, err
	}
	since := a.now().Add(-7 * 24 * time.Hour).UTC().Format("2006-01-02T15:04:05")
	var items []AttentionInput
	for _, r := range records {
		if r.State != store.RestartRolledBack || r.CreatedAt < since {
			continue
		}
		reason := "A restart"
		if r.Reason != "" {
			reason = "The restart to " + r.Reason
		}
		if r.RequestedBy != "" {
			reason += fmt.Sprintf(" (asked for by %s)", r.RequestedBy)
		}
		items = append(items, AttentionInput{
			SignalKind: AttentionSignalRestartRolledBack, SourceKey: "restart:" + r.ID, SourceRevision: r.ID,
			Category: "maintenance", Urgency: "high",
			Navigation: AttentionNavigation{Kind: "settings", Target: "updates"},
			Title:      "A restart of Legato failed and was rolled back",
			Reason:     reason + " failed, so Legato put its configuration back and started on it.",
			Detail:     r.Failure,
			Actions:    []AttentionAction{{ID: "open", Label: "Open settings", Kind: "navigate"}},
		})
	}
	return items, nil
}
