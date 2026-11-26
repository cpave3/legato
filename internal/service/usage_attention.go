package service

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

const AttentionSignalUsageProfileSwitched = "usage_profile_switched"

type UsageAttentionAdapter struct{ db *store.Store }

func NewUsageAttentionAdapter(db *store.Store) *UsageAttentionAdapter {
	return &UsageAttentionAdapter{db: db}
}
func (a *UsageAttentionAdapter) Producer() string { return "usage" }
func (a *UsageAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	switches, err := a.db.UsageProfileSwitches(ctx, 200)
	if err != nil {
		return nil, err
	}
	out := []AttentionInput{}
	for _, change := range switches {
		since, err := time.Parse(time.RFC3339Nano, change.Since)
		if err != nil {
			return nil, err
		}
		if time.Since(since) > 7*24*time.Hour {
			continue
		}
		name := change.To
		if name == "" {
			name = "configured defaults"
		}
		out = append(out, AttentionInput{SignalKind: AttentionSignalUsageProfileSwitched, SourceKey: fmt.Sprint(change.ID), SourceRevision: change.To + ":" + change.Since,
			Category: "maintenance", Urgency: "info", Title: "Usage profile switched to " + name, Reason: change.Reason,
			Navigation: AttentionNavigation{Kind: "agents", Target: "usage"},
			Actions:    []AttentionAction{{ID: "open", Label: "Open Usage", Kind: "navigate", Href: "/agents?usage=open"}, {ID: "pin", Label: "Pin profile", Kind: "navigate", Href: "/agents?usage=open&pin=" + url.QueryEscape(change.To)}}})
	}
	return out, nil
}

// NotifyUsageProfileSwitch emits one logical event to each configured transport.
// A distinct switch ID prevents the return switch from hitting the rate limit.
func NotifyUsageProfileSwitch(state UsageProfileState, id int64, enabled bool, notifiers ...Notifier) {
	if !enabled {
		return
	}
	name := state.Profile.Name
	if name == "" {
		name = "configured defaults"
	}
	for _, n := range notifiers {
		if n != nil && n.Configured() && n.CanNotify(fmt.Sprintf("usage-switch:%d", id)) {
			if err := n.Notify("Usage profile: "+name, state.Reason.Text); err != nil {
				log.Printf("usage notification: %v", err)
			}
		}
	}
}
