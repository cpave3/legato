package service

import (
	"context"
	"fmt"
)

// AttentionSignalUpdateAvailable is a newer release of Legato waiting for
// the person: in ask mode, or after an install failed.
const AttentionSignalUpdateAvailable = "update_available"

// UpdateAttentionAdapter shows an update that waits for the person in
// Attention, with an action that installs it.
type UpdateAttentionAdapter struct {
	updates *UpdateService
}

func NewUpdateAttentionAdapter(updates *UpdateService) *UpdateAttentionAdapter {
	return &UpdateAttentionAdapter{updates: updates}
}

func (a *UpdateAttentionAdapter) Producer() string { return "updates" }

func (a *UpdateAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	st := a.updates.Status(ctx)
	if !st.Available {
		return nil, nil
	}
	item := AttentionInput{
		SignalKind: AttentionSignalUpdateAvailable, SourceKey: "legato-update",
		Category: "maintenance", Urgency: "normal",
		Navigation: AttentionNavigation{Kind: "settings", Target: "updates"},
	}
	switch {
	case st.State == UpdateFailed:
		item.SourceRevision = st.Latest + ":failed"
		item.Title = fmt.Sprintf("Updating Legato to %s failed", st.Latest)
		item.Reason = "This Legato still runs " + st.Version
		item.Detail = st.Error
		item.Urgency = "high"
		item.Actions = []AttentionAction{{ID: "update", Label: "Try again", Kind: "execute", Intent: "primary"}}
	case st.State == UpdateAvailable && st.Mode == UpdateModeAsk:
		item.SourceRevision = st.Latest
		item.Title = fmt.Sprintf("Legato %s is available", st.Latest)
		item.Reason = "This Legato runs " + st.Version
		item.Detail = "Updating installs it and restarts Legato. Agents keep running."
		item.Actions = []AttentionAction{{ID: "update", Label: "Update and restart", Kind: "execute", Intent: "primary"}}
	default:
		return nil, nil
	}
	item.Actions = append(item.Actions, AttentionAction{ID: "open", Label: "Open updates", Kind: "navigate"})
	return []AttentionInput{item}, nil
}

// Execute installs the update: the "update" action.
func (a *UpdateAttentionAdapter) Execute(ctx context.Context, req AttentionActionRequest) error {
	if req.ActionID != "update" {
		return fmt.Errorf("unknown update action %q", req.ActionID)
	}
	return a.updates.Apply(ctx)
}
