package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// checkHeads names the heads checks were read on: "number:head" for each
// pull request, in order.
func checkHeads(prs []store.HandoffReviewPR) string {
	parts := make([]string, 0, len(prs))
	for _, pr := range prs {
		parts = append(parts, fmt.Sprintf("%d:%s", pr.Number, pr.HeadSHA))
	}
	return strings.Join(parts, ",")
}

// pollChecks reads the counted checks of a reviewed handoff's pull
// requests, at their heads on GitHub now, when it is due. Before the first
// pass there is nothing to wait for. Call with s.mu held.
func (s *ReviewPassService) pollChecks(ctx context.Context, h *store.Handoff) {
	loop, err := s.db.GetHandoffReview(ctx, h.TaskID)
	if err != nil || loop == nil || loop.Pass == 0 || loop.Phase == ReviewPhaseStuck {
		return
	}
	interval := s.config().PollInterval
	if interval <= 0 {
		interval = time.Minute
	}
	if polled, err := time.Parse(time.RFC3339, loop.ChecksPolledAt); err == nil && s.opts.Now().Before(polled.Add(interval)) {
		return
	}
	_, branches, err := s.opts.Checkout.Branches(ctx, h.TaskID, h.Repo)
	if err != nil {
		return
	}
	open, err := s.opts.GitHub.PullRequests(ctx, h.Repo, branches)
	if err != nil || len(open) == 0 {
		return
	}
	prs := make([]store.HandoffReviewPR, 0, len(open))
	for _, pr := range open {
		prs = append(prs, store.HandoffReviewPR{TaskID: h.TaskID, Repo: h.Repo, Number: pr.Number, Branch: pr.Branch, Base: pr.Base, URL: pr.URL, HeadSHA: pr.HeadSHA})
	}
	pending := loop.ChecksPending
	if s.readChecks(ctx, h, loop, prs) || pending != loop.ChecksPending {
		err = s.saveChanged(ctx, loop)
	} else {
		err = s.save(ctx, loop)
	}
	if err != nil {
		log.Printf("review passes: %v", err)
	}
}

// readChecks reads the counted checks of prs' heads into loop and the
// items: a failing check is an item (a new version when it fails again on
// a later head after --fixed), a passing one closes its item, a pending one
// is counted. It says whether an item is new. The caller saves loop.
func (s *ReviewPassService) readChecks(ctx context.Context, h *store.Handoff, loop *store.HandoffReview, prs []store.HandoffReviewPR) bool {
	interval := s.config().PollInterval
	if interval <= 0 {
		interval = time.Minute
	}
	items, err := s.db.ListHandoffReviewItems(ctx, h.TaskID)
	if err != nil {
		return false
	}
	byID := map[string]store.HandoffReviewItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	pending, fresh := 0, 0
	var changed []store.HandoffReviewItem
	for _, pr := range prs {
		checks, err := s.opts.GitHub.Checks(ctx, h.Repo, pr.Base, pr.HeadSHA, s.config().RequiredChecks)
		if err != nil {
			var gh *GitHubError
			if errors.As(err, &gh) && gh.RateLimited {
				loop.ChecksPolledAt = gh.RetryAt.UTC().Add(-interval).Format(time.RFC3339)
			} else {
				log.Printf("review passes: reading the checks of %s#%d: %v", h.Repo, pr.Number, err)
			}
			return false
		}
		for _, c := range checks {
			id := fmt.Sprintf("k-%d-%s", pr.Number, c.Name)
			item, known := byID[id]
			switch c.State {
			case CheckPending:
				pending++
			case CheckPassing:
				if known && !item.Closed && item.Answer != AnswerReplied {
					item.Closed = true
					changed = append(changed, item)
				}
			case CheckFailing:
				switch {
				case !known:
					item = store.HandoffReviewItem{ID: id, TaskID: h.TaskID, Kind: ItemCheck, Pass: loop.Pass, Repo: h.Repo, PR: pr.Number,
						Version: 1, Author: "GitHub", CreatedAt: s.now()}
					fresh++
				case item.Closed, item.SourceID != pr.HeadSHA && item.Answer == AnswerFixed:
					// Failing again, or still failing on the commit
					// that was to fix it: a new version to handle.
					item.Version++
					item.Closed, item.Pass = false, loop.Pass
					item.Answer, item.AnswerNote, item.AnsweredAt, item.Posted, item.Delivered = "", "", "", false, false
					fresh++
				}
				item.SourceID, item.URL = pr.HeadSHA, c.URL
				item.Title = fmt.Sprintf("Check %s: %s on %s", c.Name, c.Conclusion, short(pr.HeadSHA))
				item.Body = c.Summary
				if c.Excerpt != "" {
					item.Body = strings.TrimSpace(item.Body + "\n\n" + c.Excerpt)
				}
				changed = append(changed, item)
			}
		}
	}
	for _, item := range changed {
		if err := s.db.UpsertHandoffReviewItem(ctx, item); err != nil {
			log.Printf("review passes: %v", err)
		}
	}
	loop.ChecksPolledAt, loop.ChecksHeads, loop.ChecksPending = s.now(), checkHeads(prs), pending
	if fresh > 0 {
		loop.LastNewAt = s.now()
	}
	return fresh > 0
}

// checksHandled says whether the counted checks of a loop's last pass let
// the handoff be done: read on the heads it reviewed, none pending, and
// each failing one answered by a reply.
func (s *ReviewPassService) checksHandled(ctx context.Context, loop *store.HandoffReview) bool {
	pinned, err := s.db.ListHandoffReviewPRs(ctx, loop.TaskID, loop.Pass)
	if err != nil || loop.ChecksHeads != checkHeads(pinned) || loop.ChecksPending > 0 {
		return false
	}
	items, err := s.db.ListHandoffReviewItems(ctx, loop.TaskID)
	if err != nil {
		return false
	}
	for _, item := range items {
		if item.Kind == ItemCheck && !item.Closed && item.Answer != AnswerReplied {
			return false
		}
	}
	return true
}

// checksReopen sends a clean loop back to its author when a counted check
// fails on the heads it reviewed: a check not answered, or answered
// --fixed with nothing pushed (which alone does not handle it).
func (s *ReviewPassService) checksReopen(ctx context.Context, h *store.Handoff, loop *store.HandoffReview) error {
	pinned, err := s.db.ListHandoffReviewPRs(ctx, h.TaskID, loop.Pass)
	if err != nil || loop.ChecksHeads != checkHeads(pinned) {
		return err
	}
	heads := map[string]bool{}
	for _, pr := range pinned {
		heads[pr.HeadSHA] = true
	}
	items, err := s.db.ListHandoffReviewItems(ctx, h.TaskID)
	if err != nil {
		return err
	}
	open := false
	for _, item := range items {
		if item.Kind != ItemCheck || item.Closed || item.Answer == AnswerReplied {
			continue
		}
		if item.Answer == AnswerFixed {
			if !heads[item.SourceID] {
				continue // the push is not read yet
			}
			item.Answer, item.AnswerNote, item.AnsweredAt, item.Posted, item.Delivered = "", "", "", false, false
			if err := s.db.UpsertHandoffReviewItem(ctx, item); err != nil {
				return err
			}
		}
		open = true
	}
	if !open {
		return nil
	}
	loop.Phase, loop.DeliveredPass = ReviewPhaseWaitingForAuthor, loop.Pass
	if err := s.opts.Board.MoveCard(ctx, h.TaskID, s.config().WorkColumn); err != nil {
		log.Printf("review passes: moving %s to %s: %v", h.TaskID, s.config().WorkColumn, err)
	}
	return s.saveChanged(ctx, loop)
}
