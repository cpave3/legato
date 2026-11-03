package service

import (
	"context"
	"fmt"
	"strings"
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
	items := toolUpdateItems(st)
	if item, ok := legatoUpdateItem(st); ok {
		items = append([]AttentionInput{item}, items...)
	}
	return items, nil
}

// toolUpdateSourcePrefix starts the source key of a tool's update item.
const toolUpdateSourcePrefix = "tool-update:"

// toolUpdateItems are the tools with a release waiting for the person: in
// ask mode (auto installs them at once).
func toolUpdateItems(st UpdateStatus) []AttentionInput {
	if st.Mode != UpdateModeAsk {
		return nil
	}
	var items []AttentionInput
	for _, tool := range st.Tools {
		if !tool.Managed || tool.State != UpdateAvailable {
			continue
		}
		items = append(items, AttentionInput{
			SignalKind: AttentionSignalUpdateAvailable, SourceKey: toolUpdateSourcePrefix + tool.Name,
			SourceRevision: tool.Name + ":" + tool.Latest,
			Category:       "maintenance", Urgency: "normal",
			Navigation: AttentionNavigation{Kind: "settings", Target: "updates"},
			Title:      fmt.Sprintf("%s %s is available", tool.Name, tool.Latest),
			Reason:     fmt.Sprintf("This machine's %s is %s", tool.Name, tool.Version),
			Detail:     fmt.Sprintf("Updating runs %s update, which installs it in place. Nothing restarts.", tool.Name),
			Actions: []AttentionAction{
				{ID: "update", Label: "Update " + tool.Name, Kind: "execute", Intent: "primary"},
				{ID: "open", Label: "Open updates", Kind: "navigate"},
			},
		})
	}
	return items
}

// legatoUpdateItem is a release of Legato waiting for the person, if any.
func legatoUpdateItem(st UpdateStatus) (AttentionInput, bool) {
	if !st.Available {
		return AttentionInput{}, false
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
		return AttentionInput{}, false
	}
	item.Actions = append(item.Actions, AttentionAction{ID: "open", Label: "Open updates", Kind: "navigate"})
	return item, true
}

// Execute installs the update: the "update" action.
func (a *UpdateAttentionAdapter) Execute(ctx context.Context, req AttentionActionRequest) error {
	if req.ActionID != "update" {
		return fmt.Errorf("unknown update action %q", req.ActionID)
	}
	if tool, ok := strings.CutPrefix(req.SourceKey, toolUpdateSourcePrefix); ok {
		return a.updates.ApplyTool(ctx, tool)
	}
	return a.updates.Apply(ctx)
}
