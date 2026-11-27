package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cpave3/legato/internal/engine/store"
)

// newPassSettings resolves policy once, before the pass and its pins are
// persisted. A discarded pass pinned again keeps its original selection.
func (s *ReviewPassService) newPassSettings(ctx context.Context, loop *store.HandoffReview, number int) (ReviewPassSettings, error) {
	settings := loopSettings(loop, s.config().Defaults)
	previous, err := s.db.GetHandoffReviewPass(ctx, loop.TaskID, number)
	if err == nil && previous != nil {
		return passSettings(*previous, settings)
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return ReviewPassSettings{}, err
	}
	if settings.ReviewerOverride == nil {
		return s.freezePassAccount(ctx, settings)
	}
	defaults := s.config().Defaults
	if s.opts.ReviewerDefaults != nil {
		defaults, err = s.opts.ReviewerDefaults(ctx, defaults)
		if err != nil {
			return ReviewPassSettings{}, err
		}
	}
	settings.Agent, settings.Model, settings.Tier, settings.Account = defaults.Agent, defaults.Model, defaults.Tier, defaults.Account
	o := settings.ReviewerOverride
	if o.Agent != "" && o.Agent != settings.Agent {
		settings.Agent, settings.Model, settings.Tier, settings.Account = o.Agent, "", "", ""
	}
	if o.Model != "" {
		settings.Model, settings.Tier = o.Model, ""
	}
	if o.Tier != "" {
		settings.Tier, settings.Model = o.Tier, ""
	}
	return s.freezePassAccount(ctx, settings)
}

// passSettings falls back only for passes written before snapshots existed.
func passSettings(pass store.HandoffReviewPass, legacy ReviewPassSettings) (ReviewPassSettings, error) {
	if pass.ReviewerSettings == "" {
		return legacy, nil
	}
	var snapshot reviewPassSnapshot
	if err := json.Unmarshal([]byte(pass.ReviewerSettings), &snapshot); err != nil {
		return ReviewPassSettings{}, fmt.Errorf("reading reviewer settings for pass %d: %w", pass.Pass, err)
	}
	snapshot.ReviewPassSettings.accountIdentity = snapshot.AccountIdentity
	return snapshot.ReviewPassSettings, nil
}

// Only pass snapshots carry host paths; settings and adverts carry names.
type reviewPassSnapshot struct {
	ReviewPassSettings
	AccountIdentity *store.AgentAccountIdentity `json:"account_identity,omitempty"`
}

func encodePassSettings(settings ReviewPassSettings) (string, error) {
	return encodeJSON(reviewPassSnapshot{ReviewPassSettings: settings, AccountIdentity: settings.accountIdentity})
}

func (s *ReviewPassService) freezePassAccount(ctx context.Context, settings ReviewPassSettings) (ReviewPassSettings, error) {
	if resolver, ok := s.opts.Agents.(interface {
		resolveClaudeAccount(context.Context, AgentSpawnOptions, string) (*store.AgentAccountIdentity, error)
	}); ok {
		identity, err := resolver.resolveClaudeAccount(ctx, AgentSpawnOptions{AgentKind: settings.Agent, Account: settings.Account}, settings.Agent)
		if err != nil {
			return ReviewPassSettings{}, err
		}
		settings.accountIdentity = identity
	}
	return settings, nil
}
