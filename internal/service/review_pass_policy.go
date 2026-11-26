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
		return settings, nil
	}
	defaults := s.config().Defaults
	if s.opts.ReviewerDefaults != nil {
		defaults, err = s.opts.ReviewerDefaults(ctx, defaults)
		if err != nil {
			return ReviewPassSettings{}, err
		}
	}
	settings.Agent, settings.Model, settings.Tier = defaults.Agent, defaults.Model, defaults.Tier
	o := settings.ReviewerOverride
	if o.Agent != "" {
		settings.Agent, settings.Model, settings.Tier = o.Agent, "", ""
	}
	if o.Model != "" {
		settings.Model, settings.Tier = o.Model, ""
	}
	if o.Tier != "" {
		settings.Tier, settings.Model = o.Tier, ""
	}
	return settings, nil
}

// passSettings falls back only for passes written before snapshots existed.
func passSettings(pass store.HandoffReviewPass, legacy ReviewPassSettings) (ReviewPassSettings, error) {
	if pass.ReviewerSettings == "" {
		return legacy, nil
	}
	var settings ReviewPassSettings
	if err := json.Unmarshal([]byte(pass.ReviewerSettings), &settings); err != nil {
		return ReviewPassSettings{}, fmt.Errorf("reading reviewer settings for pass %d: %w", pass.Pass, err)
	}
	return settings, nil
}
