package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// ReviewPassOverride is what a main's Hand off dialog changes of a worker's
// review settings for one handoff; a field left empty keeps the worker's.
type ReviewPassOverride struct {
	Enabled   *bool  `json:"enabled,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Model     string `json:"model,omitempty"`
	Tier      string `json:"tier,omitempty"`
	Skill     string `json:"skill,omitempty"`
	MaxPasses int    `json:"max_passes,omitempty"`
}

// Apply makes the settings of one handoff: the worker's defaults with the
// override on them. An override naming a tool, tier or skill this worker
// does not have is an error, as is a pass limit below one.
func (a ReviewPassesAdvert) Apply(o ReviewPassOverride) (ReviewPassSettings, error) {
	settings := a.Defaults
	if o.Enabled != nil {
		settings.Enabled = *o.Enabled
	}
	if !settings.Enabled {
		return settings, nil
	}
	if o.Agent != "" && o.Agent != settings.Agent {
		settings.Agent, settings.Model, settings.Tier = o.Agent, "", ""
	}
	var adapter *ReviewAdapter
	for i := range a.Adapters {
		if a.Adapters[i].Name == settings.Agent {
			adapter = &a.Adapters[i]
		}
	}
	if adapter == nil {
		return ReviewPassSettings{}, fmt.Errorf("this worker has no reviewer tool %q", settings.Agent)
	}
	if o.Model != "" {
		settings.Model, settings.Tier = o.Model, ""
	}
	if o.Tier != "" {
		found := false
		for _, t := range adapter.Tiers {
			found = found || t == o.Tier
		}
		if !found {
			return ReviewPassSettings{}, fmt.Errorf("this worker's %s has no tier %q", settings.Agent, o.Tier)
		}
		settings.Tier, settings.Model = o.Tier, ""
	}
	if o.Skill != "" {
		found := false
		for _, s := range a.Skills {
			found = found || s == o.Skill
		}
		if !found {
			return ReviewPassSettings{}, fmt.Errorf("this worker has no skill %q", o.Skill)
		}
		settings.Skill = o.Skill
	}
	if o.MaxPasses < 0 {
		return ReviewPassSettings{}, errors.New("max passes must be at least 1")
	}
	if o.MaxPasses > 0 {
		settings.MaxPasses = o.MaxPasses
	}
	return settings, nil
}

// Override checks a main's override against this worker's review passes
// and returns the handoff's settings.
func (s *ReviewPassService) Override(o ReviewPassOverride) (ReviewPassSettings, error) {
	if s.opts.Advert == nil {
		return ReviewPassSettings{}, errors.New("this worker does not say what its review passes can do")
	}
	advert := s.opts.Advert()
	if advert == nil {
		return ReviewPassSettings{}, errors.New("this worker does not run review passes")
	}
	return advert.Apply(o)
}

// ErrReviewNotStuck refuses Run another pass to a review that is not stuck.
var ErrReviewNotStuck = errors.New("the review is not stuck")

// AnotherPass runs one more pass of a stuck review: past its limit, or
// again after a reviewer that could not review or a sandbox that failed.
func (s *ReviewPassService) AnotherPass(ctx context.Context, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	loop, err := s.db.GetHandoffReview(ctx, taskID)
	if err != nil {
		return err
	}
	if loop == nil || loop.Phase != ReviewPhaseStuck {
		return ErrReviewNotStuck
	}
	settings := loopSettings(loop, s.config().Defaults)
	if settings.MaxPasses > 0 && loop.Pass >= settings.MaxPasses {
		settings.MaxPasses = loop.Pass + 1
		if err := s.setSettings(ctx, loop, settings); err != nil {
			return err
		}
	}
	loop.Phase, loop.Reason, loop.Mismatches, loop.AuthorBusySinceNudge, loop.NudgedAt = ReviewPhaseReady, "", 0, false, ""
	return s.saveChanged(ctx, loop)
}

// MainTag opens every message the main instance's person sends the author.
const MainTag = "[legato main]"

// MessageAuthor sends the author the person's words from the main, tagged,
// queued as feedback is (no attachments, and the tag keeps it from reading
// as a slash command). A dead author is resumed first.
func (s *ReviewPassService) MessageAuthor(ctx context.Context, taskID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("a message needs text")
	}
	if _, err := s.opts.Agents.GetAgentSession(ctx, taskID); err != nil {
		if s.opts.ResumeAuthor == nil {
			return errors.New("the author is not running, and this worker cannot resume it")
		}
		if err := s.opts.ResumeAuthor(ctx, taskID); err != nil {
			return err
		}
	}
	return s.opts.Agents.QueueMessage(ctx, taskID, MainTag+" "+text)
}

// setSettings writes a loop's settings.
func (s *ReviewPassService) setSettings(ctx context.Context, loop *store.HandoffReview, settings ReviewPassSettings) error {
	data, err := encodeJSON(settings)
	if err != nil {
		return err
	}
	loop.Settings = data
	return s.save(ctx, loop)
}
