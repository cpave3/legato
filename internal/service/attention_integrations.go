package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

const (
	AttentionSignalDelegationFailed        = "delegation_failed"
	AttentionSignalCaseReplyFailed         = "case_reply_failed"
	AttentionSignalIntegrationDisconnected = "integration_disconnected"
)

// integrationAttentionWindow is how far back failures are shown.
const integrationAttentionWindow = 7 * 24 * time.Hour

// IntegrationAttentionAdapter shows what went wrong between Legato and an
// integration: a delegation that could not reach its agent, a reply Argus
// never took, and a connection Argus refused.
type IntegrationAttentionAdapter struct{ db *store.Store }

func NewIntegrationAttentionAdapter(db *store.Store) *IntegrationAttentionAdapter {
	return &IntegrationAttentionAdapter{db: db}
}

func (a *IntegrationAttentionAdapter) Producer() string { return "integrations" }

func (a *IntegrationAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	var items []AttentionInput
	since := time.Now().UTC().Add(-integrationAttentionWindow)
	failed, err := a.db.ListDelegationsByState(ctx, store.DelegationFailed)
	if err != nil {
		return nil, err
	}
	for _, d := range failed {
		// The person refused it themselves; nothing to tell them.
		if d.ErrorCode == "denied" {
			continue
		}
		if created, err := time.Parse("2006-01-02T15:04:05.000Z", d.CreatedAt); err == nil && created.Before(since) {
			continue
		}
		items = append(items, AttentionInput{
			SignalKind: AttentionSignalDelegationFailed, SourceKey: d.ID, SourceRevision: d.ErrorCode, TaskID: d.TaskID,
			Title:    fmt.Sprintf("%s's %s on case %s did not reach an agent", integrationTitle(d.Integration), modeLabel(d.Mode), d.CaseID),
			Reason:   d.ErrorMessage,
			Detail:   "Argus sees this delegation as failed (" + d.ErrorCode + ").",
			Category: "integration", Urgency: "normal",
			Navigation: AttentionNavigation{Kind: "task", Target: d.TaskID},
			Actions:    []AttentionAction{{ID: "open", Label: "Open task", Kind: "navigate"}},
		})
	}
	replies, err := a.db.ListFailedCaseRepliesSince(ctx, since)
	if err != nil {
		return nil, err
	}
	for _, r := range replies {
		items = append(items, AttentionInput{
			SignalKind: AttentionSignalCaseReplyFailed, SourceKey: r.ID, SourceRevision: "failed", TaskID: r.TaskID,
			Title:    "An agent's reply on case " + r.CaseID + " did not reach Argus",
			Reason:   r.LastError,
			Category: "integration", Urgency: "normal",
			Navigation: AttentionNavigation{Kind: "task", Target: r.TaskID},
			Actions:    []AttentionAction{{ID: "open", Label: "Open task", Kind: "navigate"}},
		})
	}
	in, err := a.db.GetIntegration(ctx, "argus")
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if in != nil && in.State == store.IntegrationDisconnected {
		items = append(items, AttentionInput{
			SignalKind: AttentionSignalIntegrationDisconnected, SourceKey: in.Name, SourceRevision: in.ConnectedAt,
			Title:    "Legato lost its connection to Argus",
			Reason:   "Argus refused Legato's tokens, so agents' replies wait.",
			Detail:   "Run legato integration connect argus at the host and allow Legato again.",
			Category: "integration", Urgency: "high",
			Navigation: AttentionNavigation{Kind: "settings", Target: "integrations"},
			Actions:    []AttentionAction{{ID: "open", Label: "Open settings", Kind: "navigate"}},
		})
	}
	return items, nil
}
