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

// pollChecks reads the counted checks of a reviewed loop's pull
// requests, at their heads on GitHub now, when it is due. Before the first
// pass there is nothing to wait for. Call with s.mu held.
func (s *ReviewPassService) pollChecks(ctx context.Context, l ReviewLoop) {
	loop, err := s.db.GetHandoffReview(ctx, l.TaskID)
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
	if l.caller == nil {
		return
	}
	open, err := l.caller.OpenPRs(ctx, l)
	if err != nil || len(open) == 0 {
		return
	}
	prs := make([]store.HandoffReviewPR, 0, len(open))
	for _, pr := range open {
		prs = append(prs, store.HandoffReviewPR{TaskID: l.TaskID, Repo: l.Repo, Number: pr.Number, Branch: pr.Branch, Base: pr.Base, URL: pr.URL, HeadSHA: pr.HeadSHA})
	}
	pending := loop.ChecksPending
	if s.readChecks(ctx, l, loop, prs) || pending != loop.ChecksPending {
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
func (s *ReviewPassService) readChecks(ctx context.Context, l ReviewLoop, loop *store.HandoffReview, prs []store.HandoffReviewPR) bool {
	interval := s.config().PollInterval
	if interval <= 0 {
		interval = time.Minute
	}
	items, err := s.db.ListHandoffReviewItems(ctx, l.TaskID)
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
		checks, err := s.opts.GitHub.Checks(ctx, l.Repo, pr.Base, pr.HeadSHA, s.config().RequiredChecks)
		if err != nil {
			var limit *GitHubRateLimit
			if errors.As(err, &limit) {
				loop.ChecksPolledAt = limit.RetryAt.UTC().Add(-interval).Format(time.RFC3339)
			} else {
				log.Printf("review passes: reading the checks of %s#%d: %v", l.Repo, pr.Number, err)
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
					item = store.HandoffReviewItem{ID: id, TaskID: l.TaskID, Kind: ItemCheck, Pass: loop.Pass, Repo: l.Repo, PR: pr.Number,
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
// the loop be done: read on the heads it reviewed, none pending, and
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
func (s *ReviewPassService) checksReopen(ctx context.Context, l ReviewLoop, loop *store.HandoffReview) error {
	pinned, err := s.db.ListHandoffReviewPRs(ctx, l.TaskID, loop.Pass)
	if err != nil || loop.ChecksHeads != checkHeads(pinned) {
		return err
	}
	heads := map[string]bool{}
	for _, pr := range pinned {
		heads[pr.HeadSHA] = true
	}
	items, err := s.db.ListHandoffReviewItems(ctx, l.TaskID)
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
	l.stage(ctx, ReviewStageAuthor)
	return s.saveChanged(ctx, loop)
}

// failingChecks says whether a counted check fails on a loop's pull
// requests and is not handled: not answered by a reply.
func (s *ReviewPassService) failingChecks(ctx context.Context, taskID string) bool {
	items, err := s.db.ListHandoffReviewItems(ctx, taskID)
	if err != nil {
		return false
	}
	for _, item := range items {
		if item.Kind == ItemCheck && !item.Closed && item.Answer != AnswerReplied {
			return true
		}
	}
	return false
}

// approveSettled approves, after a clean pass, each pull request the pass
// left at a comment review only for a failing counted check that is now
// handled: it passed, or the author answered it by a reply. A post that
// fails is an error: the loop is not approved until GitHub has the APPROVE
// (invariant 9), and the next try finds a review GitHub took by its marker.
func (s *ReviewPassService) approveSettled(ctx context.Context, l ReviewLoop, loop *store.HandoffReview) error {
	if loop.Phase != ReviewPhaseClean || loop.Pass == 0 {
		return nil
	}
	prs, err := s.db.ListHandoffReviewPRs(ctx, l.TaskID, loop.Pass)
	if err != nil {
		return err
	}
	findings, err := s.db.ListHandoffReviewFindings(ctx, l.TaskID)
	if err != nil {
		return err
	}
	items, err := s.db.ListHandoffReviewItems(ctx, l.TaskID)
	if err != nil {
		return err
	}
	pass, err := s.db.GetHandoffReviewPass(ctx, l.TaskID, loop.Pass)
	if err != nil {
		return err
	}
	settings := loopSettings(loop, s.config().Defaults)
	for _, pr := range prs {
		if pr.Event != ReviewComment || !prClean(loop.Pass, pr.Number, findings, items, true) {
			continue
		}
		if ok, err := s.mayWrite(ctx, l, 0); !ok {
			return err
		}
		posted, err := s.opts.GitHub.PostReview(ctx, ReviewPost{Repo: pr.Repo, Number: pr.Number, CommitID: pr.HeadSHA, Event: ReviewApprove,
			Summary: "Its failing checks are settled.", Footer: reviewFooter(settings, loop.Pass),
			Marker: laterReviewMarker(*pass, pr.Number, LaterApprove)})
		if err != nil {
			return fmt.Errorf("approving %s#%d once its checks settled, trying again: %w", pr.Repo, pr.Number, err)
		}
		if err := s.db.SetHandoffReviewPRPosted(ctx, l.TaskID, loop.Pass, pr.Number, posted.ID, posted.URL, ReviewApprove); err != nil {
			return err
		}
	}
	return nil
}
