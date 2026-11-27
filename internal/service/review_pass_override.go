package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

// OnlyOff says whether the override only turns review passes off.
func (o ReviewPassOverride) OnlyOff() bool {
	return o.Enabled != nil && !*o.Enabled && o.Agent == "" && o.Model == "" && o.Tier == "" && o.Skill == "" && o.MaxPasses == 0
}

// Apply makes the settings of one handoff: the worker's defaults with the
// override on them. An override naming a tool, tier or skill this worker
// does not have is an error, as is a pass limit below one.
func (a ReviewPassesAdvert) Apply(o ReviewPassOverride) (ReviewPassSettings, error) {
	settings := a.Defaults
	settings.ReviewerOverride = &ReviewPassOverride{Agent: o.Agent, Model: o.Model, Tier: o.Tier}
	if o.Enabled != nil {
		settings.Enabled = *o.Enabled
	}
	if !settings.Enabled {
		return settings, nil
	}
	if o.Agent != "" && o.Agent != settings.Agent {
		settings.Agent, settings.Model, settings.Tier, settings.Account = o.Agent, "", "", ""
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
		// Named plainly, or as Claude Code names a managed skill.
		name := strings.TrimPrefix(o.Skill, managedSkillPrefix)
		found := false
		for _, s := range a.Skills {
			found = found || s == name
		}
		if !found {
			return ReviewPassSettings{}, fmt.Errorf("this worker has no skill %q", o.Skill)
		}
		settings.Skill = name
		settings.SkillManaged = slices.Contains(a.ManagedSkills, name)
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

// AnotherPass is Run another pass on the loop a task hosts (a handed-off
// card's), by its loop ID (AnotherPassOn).
func (s *ReviewPassService) AnotherPass(ctx context.Context, taskID string) error {
	l, err := s.db.CurrentReviewLoop(ctx, taskID)
	if err != nil {
		return err
	}
	if l == nil || l.State != store.ReviewLoopOpenState {
		return ErrReviewNotStuck
	}
	return s.AnotherPassOn(ctx, l.ID)
}

// RecordAnotherPass is anotherPass for a process that runs no callers,
// such as the operator's `legato review-pass another-pass --loop` on the
// host: it writes the stuck loop's record only, and the Legato that runs
// the engine starts the pass on its next tick.
func (s *ReviewPassService) RecordAnotherPass(ctx context.Context, taskID string) error {
	return s.anotherPass(ctx, taskID)
}

// anotherPass runs one more pass of a stuck review: past its limit, or
// again after a reviewer that could not review or a sandbox that failed.
// It only writes the record; the tick starts the pass.
func (s *ReviewPassService) anotherPass(ctx context.Context, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	loop, err := s.db.GetHandoffReview(ctx, taskID)
	if err != nil {
		return err
	}
	if loop == nil || loop.Phase != ReviewPhaseStuck {
		return ErrReviewNotStuck
	}
	if pass, err := s.db.GetHandoffReviewPass(ctx, taskID, loop.Pass); err == nil && pass.State == ReviewPassSubmitted {
		// Its post was never confirmed: GitHub may have its reviews. The
		// same pass is posted again, its markers finding what GitHub took,
		// with a fresh budget of tries; never a second pass while it is
		// in flight. (A pass GitHub refused ended unable.)
		if err := s.db.SetHandoffReviewPassPosting(ctx, taskID, pass.Pass, s.now(), 0); err != nil {
			return err
		}
		loop.Phase, loop.Reason = ReviewPhaseReviewing, ""
		return s.saveChanged(ctx, loop)
	}
	// One more pass in the current window: its start moves on, and the
	// limit stays, so the next window has the limit it always had.
	settings := loopSettings(loop, s.config().Defaults)
	if ws := s.windowStart(ctx, loop.LoopID); settings.MaxPasses > 0 && loop.Pass-ws >= settings.MaxPasses {
		if _, err := s.db.UpdateHostedReviewLoop(ctx, taskID, func(t *store.ReviewLoopTx) error {
			t.Loop.WindowStart = loop.Pass - settings.MaxPasses + 1
			return nil
		}); err != nil {
			return err
		}
	}
	loop.Phase, loop.Reason, loop.Mismatches, loop.AuthorBusySinceNudge, loop.NudgedAt = ReviewPhaseReady, "", 0, false, ""
	return s.saveChanged(ctx, loop)
}

// MainTag opens every message the main instance's person sends the author.
const MainTag = "[legato main]"

// MessageAuthor sends the author of the loop a task hosts (a handed-off
// card's) the person's words, by its loop ID (MessageAuthorOn).
func (s *ReviewPassService) MessageAuthor(ctx context.Context, taskID, text string) error {
	l, err := s.db.CurrentReviewLoop(ctx, taskID)
	if err != nil {
		return err
	}
	if l == nil || l.State != store.ReviewLoopOpenState {
		return errors.New("no review loop runs on " + taskID)
	}
	return s.MessageAuthorOn(ctx, l.ID, text)
}

// messageAuthor sends the author the person's words from the main, tagged,
// queued as feedback is (no attachments, and the tag keeps it from reading
// as a slash command). A dead author is resumed first.
func (s *ReviewPassService) messageAuthor(ctx context.Context, taskID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("a message needs text")
	}
	l, err := s.loop(ctx, taskID)
	if err != nil {
		return err
	}
	if l.AuthorTaskID == "" {
		return ErrReviewAuthorNotAgent
	}
	if err := s.authorRunning(ctx, l); err != nil {
		return err
	}
	return s.tellAuthor(ctx, l, MainTag+" "+text)
}

// ErrReviewNotWaiting refuses another pass to a loop that does not wait for
// its author.
var ErrReviewNotWaiting = errors.New("the review loop is not waiting for its author")

// NextPass starts the next pass of a loop that waits for its author now,
// whether or not the author pushed: Run another pass on a run's loop. Its
// maximum passes still hold, so a loop at its limit goes stuck. It only
// writes the record; the tick starts the pass.
func (s *ReviewPassService) NextPass(ctx context.Context, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	loop, err := s.db.GetHandoffReview(ctx, taskID)
	if err != nil {
		return err
	}
	if loop == nil || loop.Phase != ReviewPhaseWaitingForAuthor {
		return ErrReviewNotWaiting
	}
	loop.Phase, loop.Reason = ReviewPhaseReady, ""
	return s.saveChanged(ctx, loop)
}
