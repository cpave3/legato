package service

import (
	"context"
	"fmt"
	"strings"
)

// AttentionSignalSettingsRestart is settings changed since Legato started
// that apply only when it restarts.
const AttentionSignalSettingsRestart = "settings_restart"

// SettingsAttentionAdapter says when changed settings wait for a restart,
// with an action that restarts Legato.
type SettingsAttentionAdapter struct {
	settings *SettingsService
	restart  func()
}

func NewSettingsAttentionAdapter(settings *SettingsService, restart func()) *SettingsAttentionAdapter {
	return &SettingsAttentionAdapter{settings: settings, restart: restart}
}

func (a *SettingsAttentionAdapter) Producer() string { return "settings" }

func (a *SettingsAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	keys, err := a.settings.PendingRestart(ctx)
	if err != nil || len(keys) == 0 {
		return nil, err
	}
	title := fmt.Sprintf("Restart to apply %d changes", len(keys))
	if len(keys) == 1 {
		title = "Restart to apply 1 change"
	}
	item := AttentionInput{
		SignalKind: AttentionSignalSettingsRestart, SourceKey: "settings-restart", SourceRevision: strings.Join(keys, ","),
		Category: "maintenance", Urgency: "normal",
		Navigation: AttentionNavigation{Kind: "settings", Target: "settings"},
		Title:      title,
		Reason:     "Settings changed since Legato started take effect when it restarts.",
		Detail:     "Waiting: " + strings.Join(keys, ", "),
		Actions:    []AttentionAction{{ID: "open", Label: "Open settings", Kind: "navigate"}},
	}
	if a.restart != nil {
		item.Actions = append([]AttentionAction{{ID: "restart", Label: "Restart Legato", Kind: "execute", Intent: "primary"}}, item.Actions...)
	}
	return []AttentionInput{item}, nil
}

// Execute runs the restart action.
func (a *SettingsAttentionAdapter) Execute(_ context.Context, req AttentionActionRequest) error {
	if req.ActionID != "restart" || a.restart == nil {
		return fmt.Errorf("unknown action %q", req.ActionID)
	}
	a.restart()
	return nil
}
