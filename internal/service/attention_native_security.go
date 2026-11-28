package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

const AttentionSignalNativeSecurityIncident = "native_security_incident"

// NativeSecurityAttentionAdapter projects refusal history, not pending
// approvals: a valid answer or an ended session must not erase an incident.
type NativeSecurityAttentionAdapter struct{ db *store.Store }

func NewNativeSecurityAttentionAdapter(db *store.Store) *NativeSecurityAttentionAdapter {
	return &NativeSecurityAttentionAdapter{db: db}
}

func (a *NativeSecurityAttentionAdapter) Producer() string { return "native_security" }

func (a *NativeSecurityAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	records, err := a.db.ListNativeSecurityIncidents(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]AttentionInput, 0, len(records))
	for _, r := range records {
		key, _ := json.Marshal([2]string{r.ExecutionID, r.Generation})
		items = append(items, AttentionInput{
			SignalKind: AttentionSignalNativeSecurityIncident, SourceKey: string(key), SourceRevision: r.Generation,
			TaskID: r.TaskID, Category: "security", Urgency: "high",
			Title:      fmt.Sprintf("%s's native session tried an unauthorized control call", r.TaskID),
			Reason:     "Legato refused a native control call without valid host authorization. Review the session before continuing.",
			Detail:     fmt.Sprintf("Category: %s. Refusals: %d. First: %s. Last: %s.", r.Category, r.Count, r.FirstSeenAt, r.LastSeenAt),
			Navigation: AttentionNavigation{Kind: "task", Target: r.TaskID},
			Actions:    []AttentionAction{{ID: "open", Label: "Open task", Kind: "navigate"}},
		})
	}
	return items, nil
}
