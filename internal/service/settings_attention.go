package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
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

// AttentionSignalSettingsMove is the offer, after the upgrade that brought
// settings into the database, to move the settings config.yaml pins there.
const AttentionSignalSettingsMove = "settings_move"

// SettingsMoveAttentionAdapter makes the one offer to move pinned settings
// into the database. Its revision never changes, so once dismissed it stays
// dismissed; each pinned setting keeps its own "Move to the database".
type SettingsMoveAttentionAdapter struct{ mover *SettingsMover }

func NewSettingsMoveAttentionAdapter(mover *SettingsMover) *SettingsMoveAttentionAdapter {
	return &SettingsMoveAttentionAdapter{mover: mover}
}

func (a *SettingsMoveAttentionAdapter) Producer() string { return "settings-move" }

func (a *SettingsMoveAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	movable, err := a.mover.Movable(ctx)
	if err != nil || movable.Empty() {
		return nil, err
	}
	n := len(movable.Settings) + len(movable.Entries)
	return []AttentionInput{{
		SignalKind: AttentionSignalSettingsMove, SourceKey: "settings-move", SourceRevision: "offer-1",
		Category: "maintenance", Urgency: "normal",
		Navigation: AttentionNavigation{Kind: "settings", Target: "move"},
		Title:      fmt.Sprintf("Move %d settings from config.yaml into the database", n),
		Reason:     "Settings kept in the database can be changed from Settings and by the Reeve. Untick any to keep pinned in config.yaml.",
		Detail:     strings.Join(movable.Settings, ", "),
		Actions:    []AttentionAction{{ID: "open", Label: "Choose what to move", Kind: "navigate", Intent: "primary"}},
	}}, nil
}

// AttentionSignalReevePeer is another instance's Reeve that wants to talk,
// with no session running here and reeve.peer set to ask.
const AttentionSignalReevePeer = "reeve_peer"

// ReevePeerAttentionAdapter asks the person to let another Reeve talk.
type ReevePeerAttentionAdapter struct {
	db    *store.Store
	reeve *ReeveService
}

func NewReevePeerAttentionAdapter(db *store.Store, reeve *ReeveService) *ReevePeerAttentionAdapter {
	return &ReevePeerAttentionAdapter{db: db, reeve: reeve}
}

func (a *ReevePeerAttentionAdapter) Producer() string { return "reeve-peer" }

func (a *ReevePeerAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	waiting, err := a.db.ListReeveInbox(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var items []AttentionInput
	for _, m := range waiting {
		if seen[m.InstanceID] {
			continue
		}
		seen[m.InstanceID] = true
		items = append(items, AttentionInput{
			SignalKind: AttentionSignalReevePeer, SourceKey: "reeve-peer:" + m.InstanceID, SourceRevision: fmt.Sprint(m.ID),
			Category: "maintenance", Urgency: "normal",
			Navigation: AttentionNavigation{Kind: "settings", Target: "reeve"},
			Title:      "The Reeve on " + m.Name + " wants to talk",
			Reason:     "No Reeve session runs here, and Settings › Reeve says to ask you first.",
			Detail:     m.Text,
			Actions: []AttentionAction{
				{ID: "talk", Label: "Let it talk", Kind: "execute", Intent: "primary"},
				{ID: "open", Label: "Open settings", Kind: "navigate"},
			},
		})
	}
	return items, nil
}

// Execute lets the peer talk: a session starts with its messages.
func (a *ReevePeerAttentionAdapter) Execute(ctx context.Context, req AttentionActionRequest) error {
	if req.ActionID != "talk" || a.reeve == nil {
		return fmt.Errorf("unknown action %q", req.ActionID)
	}
	return a.reeve.LetPeerTalk(ctx, strings.TrimPrefix(req.SourceKey, "reeve-peer:"))
}
