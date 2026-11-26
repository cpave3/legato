package service

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// The steward (spec LEG-559 § The steward; LEG-590): an agent-authored
// loop opened with steward carries on after approval. Its watch
// (github.pr.review.wait, mode watch) reads the pull requests through the
// steward's watch read (review_watch.go) and, in the engine's tick:
//
//   - routes failing counted checks and new counted comments and reviews to
//     the author agent as feedback items, each naming the operation a fix
//     may need (nothing is granted ahead), tagged [legato steward];
//   - re-enters review on any push to a watched head: one re-entry, however
//     many layers moved, counted on the record against max_reentries; the
//     pass that follows is incremental, and a clean one is approved again
//     only once Confirm finds GitHub's heads at its pins;
//   - ends the loop as closed when a pull request closes unmerged.
//
// The operator holds it (Stop watching), lets it watch again (Watch again)
// or ends it (End, outcome ended); re-entries past max_reentries make it
// stuck, and seven quiet days end it idle (review_steward_hold.go).
//
// A reply alone starts no pass; a disputed blocking finding does. A
// re-entry pass waits only for answers to blocking findings, never to
// checks or comments.

// StewardTag opens every message the steward sends the author (spec LEG-559
// Q5): after approval, where a review pass's are [legato review pass].
const StewardTag = "[legato steward]"

// What woke a steward's watch (github.pr.review.wait mode watch, woke).
const (
	StewardWokePushed   = "pushed"
	StewardWokeDisputed = "disputed"
	StewardWokeMerged   = "merged"
	StewardWokeClosed   = "closed"
	StewardWokeIdle     = "idle"
	StewardWokeEnded    = "ended"
)

// ReviewStageClosed tells a caller that a pull request of its stewarded
// loop closed unmerged: the loop ends with outcome closed.
const ReviewStageClosed = "closed"

// ReviewStageMerged tells a caller that every pull request of its
// stewarded loop merged: the loop ends with outcome merged.
const ReviewStageMerged = "merged"

// ReviewOutcomeMerged is a stewarded loop's outcome once every layer
// merged.
const ReviewOutcomeMerged = StewardWokeMerged

// watchEnd is how a watch read ends a stewarded loop: closed when a layer
// closed unmerged, merged when every layer merged, "" while one is open.
func watchEnd(w ReviewLoopWatch) string {
	if len(w.Layers) == 0 {
		return ""
	}
	merged := true
	for _, layer := range w.Layers {
		switch layer.State {
		case "closed":
			return ReviewStageClosed
		case "merged":
		default:
			merged = false
		}
	}
	if merged {
		return ReviewStageMerged
	}
	return ""
}

// StewardIdleAfter is how long a steward watches with no push, comment,
// review or check change before its idle deadline (spec LEG-559: seven
// days, repository_cache.keep_idle).
const StewardIdleAfter = 7 * 24 * time.Hour

// The operations a fix of a steward's item may need (spec LEG-559 § The
// steward › Authority): the author's own credential rules decide each, and
// none is granted ahead.
const (
	stewardCheckOperation = "A fix is a commit pushed to the pull request's branch (git.push; git.push-workflows when it changes a file under .github/workflows). " +
		"If a job failed before its tests ran (a runner, network or setup failure), re-run it once instead: gh run rerun <run-id> --failed (actions.write). " +
		"Nothing is granted ahead: the operator's credential rules decide each."
	stewardCommentOperation = "Answer it with legato review-pass answer, which Legato posts as your reply. " +
		"A fix is a commit pushed to the pull request's branch (git.push; git.push-workflows when it changes a file under .github/workflows). " +
		"Nothing is granted ahead: the operator's credential rules decide each."
	stewardMergeabilityOperation = "A fix is merging the base into the pull request's branch, or restacking it onto the base (st restack, or git rebase --update-refs), " +
		"then pushing it (git.push, with --force-with-lease after a rebase; git.push-workflows when it changes a file under .github/workflows). " +
		"The item closes by itself once GitHub reports the pull request mergeable. Nothing is granted ahead: the operator's credential rules decide each."
	stewardMergedOperation = "A fix is restacking the layers above onto the merged layer's base (st restack, or git rebase --onto), " +
		"then pushing every branch that moved (git.push, with --force-with-lease). Nothing is granted ahead: the operator's credential rules decide each."
)

// stewardOperation is the operation a fix of a steward's item of kind may
// need; "" for a reviewer's finding.
func stewardOperation(kind string) string {
	switch kind {
	case ItemCheck:
		return stewardCheckOperation
	case ItemComment:
		return stewardCommentOperation
	case ItemMergeability:
		return stewardMergeabilityOperation
	case ItemMergedLayer:
		return stewardMergedOperation
	}
	return ""
}

// stewardHow is the feedback view's guidance while the steward watches.
const stewardHow = " The review approved these pull requests, and Legato's steward looks after them until they merge: " +
	"it sends you failing checks and new comments and reviews, each item naming the operation a fix may need. " +
	"Any push goes back to review (one more pass, and a fresh approval once it is clean at the new heads); " +
	"a reply alone starts no pass, unless it disputes a blocking finding. Never merge."

// stewarding says a loop's steward watches: a steward loop the review
// approved once, whose watch began (beginWatch set its idle deadline). It
// stays so through each re-entry's review until the loop closes.
func stewarding(rec *store.ReviewLoop) bool {
	return rec != nil && rec.State == store.ReviewLoopOpenState && rec.Steward && rec.IdleDeadline != ""
}

// stewardRecord is the loop record a task hosts when its steward watches,
// nil otherwise.
func (s *ReviewPassService) stewardRecord(ctx context.Context, taskID string) *store.ReviewLoop {
	rec, err := s.db.CurrentReviewLoop(ctx, taskID)
	if err != nil || !stewarding(rec) {
		return nil
	}
	return rec
}

// authorTag is the tag of what the engine tells a loop's author:
// [legato steward] while its steward watches, else [legato review pass].
func (s *ReviewPassService) authorTag(ctx context.Context, taskID string) string {
	if s.stewardRecord(ctx, taskID) != nil {
		return StewardTag
	}
	return ReviewTag
}

// beginWatch starts a steward loop's watch once the review approved it: its
// idle deadline, and a fresh watch read (one taken before the approval is
// never compared with the approved heads).
func (s *ReviewPassService) beginWatch(ctx context.Context, rec *store.ReviewLoop) error {
	if rec.IdleDeadline != "" {
		return nil
	}
	deadline := s.opts.Now().Add(StewardIdleAfter).UTC().Format(time.RFC3339)
	if _, err := s.db.UpdateReviewLoop(ctx, rec.ID, rec.Rev, func(t *store.ReviewLoopTx) error {
		t.Loop.IdleDeadline = deadline
		return nil
	}); err != nil {
		return err
	}
	s.forgetWatch(rec.ID)
	return nil
}

// forgetWatch drops a loop's last watch read and makes its next one due at
// once.
func (s *ReviewPassService) forgetWatch(loopID string) {
	s.watchMu.Lock()
	delete(s.watched, loopID)
	delete(s.watchRouted, loopID)
	s.watchMu.Unlock()
	s.watchSchedule.Forget(loopID)
}

// freshWatch is a stewarding loop's last watch read when it succeeded.
func (s *ReviewPassService) freshWatch(loopID string) (ReviewLoopWatch, bool) {
	w, ok := s.Watched(loopID)
	if !ok || w.ReadAt.IsZero() || w.Err != nil {
		return ReviewLoopWatch{}, false
	}
	return w, true
}

// routeWatch routes what a stewarding loop's newest watch read found (each
// read once): a pull request closed unmerged ends the loop as closed, and
// every one merged as merged (true); failing counted checks, new counted
// comments and reviews, mergeability problems and merged lower layers are
// items for the author. Call with s.mu held.
func (s *ReviewPassService) routeWatch(ctx context.Context, l ReviewLoop, rec *store.ReviewLoop) bool {
	w, ok := s.freshWatch(rec.ID)
	if !ok {
		return false
	}
	if end := watchEnd(w); end != "" {
		l.stage(ctx, end)
		return true
	}
	if s.stewardHolds(ctx, rec) {
		// Held or stuck: nothing is sent. What the reads found is routed
		// once the steward watches again.
		return false
	}
	s.watchMu.Lock()
	routed := s.watchRouted[rec.ID]
	s.watchMu.Unlock()
	if !w.ReadAt.After(routed) {
		return false
	}
	if err := s.routeItems(ctx, l, rec, w); err != nil {
		log.Printf("review passes: %s: routing what the steward read: %v", l.TaskID, err)
		return false
	}
	s.watchMu.Lock()
	if s.watchRouted == nil {
		s.watchRouted = map[string]time.Time{}
	}
	s.watchRouted[rec.ID] = w.ReadAt
	s.watchMu.Unlock()
	return false
}

// routeItems turns a watch read's failing counted checks and counted
// comments of the open layers into items, as the review's polls do; a
// comment no longer there is closed (the author told when it had it).
func (s *ReviewPassService) routeItems(ctx context.Context, l ReviewLoop, rec *store.ReviewLoop, w ReviewLoopWatch) error {
	loop, err := s.db.GetHandoffReview(ctx, l.TaskID)
	if err != nil || loop == nil {
		return err
	}
	items, err := s.db.ListHandoffReviewItems(ctx, l.TaskID)
	if err != nil {
		return err
	}
	byID := map[string]store.HandoffReviewItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	authors := map[int]string{}
	if layers, err := s.db.ListReviewLoopPRs(ctx, rec.ID); err == nil {
		for _, layer := range layers {
			authors[layer.Number] = layer.Author
		}
	}
	configured := s.config().RequiredChecks
	seen, read := map[string]bool{}, map[int]bool{}
	fresh, skipped := 0, 0
	for _, layer := range w.Layers {
		item, write, isNew := s.applyMergeability(l, loop, rec.Repo, layer, byID)
		if write {
			if isNew {
				fresh++
			}
			if err := s.db.UpsertHandoffReviewItem(ctx, item); err != nil {
				return err
			}
		}
		if layer.State != "open" {
			continue
		}
		if poll, ok := w.Comments[layer.Number]; ok {
			read[layer.Number] = true
			f, sk := s.countPolled(ctx, l, loop, rec.Repo, layer.Number, authors[layer.Number], poll, byID, seen)
			fresh, skipped = fresh+f, skipped+sk
		}
		for _, c := range countedWatchChecks(layer, configured) {
			if c.Conclusion == "" {
				c.Conclusion = c.State
			}
			id := fmt.Sprintf("k-%d-%s", layer.Number, c.Name)
			before, known := byID[id]
			item, write, isNew := s.applyCheck(l, loop, layer.Number, layer.HeadSHA, c, before, known)
			if !write || (known && item == before) {
				continue
			}
			if isNew {
				fresh++
			}
			if err := s.db.UpsertHandoffReviewItem(ctx, item); err != nil {
				return err
			}
		}
	}
	for i, layer := range w.Layers {
		if layer.State != "merged" {
			continue
		}
		var above []PRWatch
		for _, up := range w.Layers[i+1:] {
			if up.State == "open" {
				above = append(above, up)
			}
		}
		item, write, isNew := s.applyMergedLayer(l, loop, rec.Repo, layer, above, byID)
		if !write {
			continue
		}
		if isNew {
			fresh++
		}
		if err := s.db.UpsertHandoffReviewItem(ctx, item); err != nil {
			return err
		}
	}
	gone := s.closeUnseen(ctx, items, seen, func(item store.HandoffReviewItem) bool {
		return read[item.PR] && strings.EqualFold(item.Repo, rec.Repo)
	})
	loop.SkippedComments = skipped
	if fresh > 0 {
		loop.LastNewAt = s.now()
	}
	if err := s.save(ctx, loop); err != nil {
		return err
	}
	if fresh > 0 {
		s.stillActive(ctx, rec.ID)
		s.changed(l.TaskID)
	}
	if len(gone) > 0 {
		_ = s.tellAuthor(ctx, l, fmt.Sprintf("%s %s was deleted on GitHub; you need not answer it.", StewardTag, strings.Join(gone, ", ")))
	}
	return nil
}

// GitHub's words for whether a pull request can merge (PRWatch.Mergeable,
// MergeState).
const (
	githubMergeable    = "MERGEABLE"
	githubConflicting  = "CONFLICTING"
	githubMergeUnknown = "UNKNOWN"
	githubMergeBehind  = "BEHIND"
	mergeabilityPrefix = "m-"
	mergedLayerPrefix  = "ml-"
)

// applyMergeability is what a watched layer makes of its one mergeability
// item (spec LEG-559 D14, Q6): CONFLICTING raises it (merge the base or
// restack), and so does a mergeStateStatus of BEHIND (no branch protection
// read: GitHub says BEHIND only when the base requires an up-to-date
// branch); any change to the problem, the head or the base raises it again
// as a new version. MERGEABLE and not BEHIND closes it by itself, as does
// the layer merging or closing. UNKNOWN changes nothing: GitHub is still
// working it out, and the watch reads it again within a minute. It says
// whether the item is to be written and whether it is new to tell.
func (s *ReviewPassService) applyMergeability(l ReviewLoop, loop *store.HandoffReview, repo string, layer PRWatch,
	byID map[string]store.HandoffReviewItem) (store.HandoffReviewItem, bool, bool) {
	id := fmt.Sprintf("%s%d", mergeabilityPrefix, layer.Number)
	item, known := byID[id]
	problem := ""
	switch {
	case layer.State != "open":
	case layer.Mergeable == githubConflicting:
		problem = githubConflicting
	case mergeUnknown(layer):
		return item, false, false
	case layer.MergeState == githubMergeBehind:
		problem = githubMergeBehind
	}
	if problem == "" {
		if known && !item.Closed {
			item.Closed = true
			return item, true, false
		}
		return item, false, false
	}
	source := problem + "@" + layer.HeadSHA + "@" + layer.BaseBranch
	if known && !item.Closed && item.SourceID == source {
		return item, false, false
	}
	if !known {
		item = store.HandoffReviewItem{ID: id, TaskID: l.TaskID, Kind: ItemMergeability, Repo: repo, PR: layer.Number, Author: "GitHub", CreatedAt: s.now()}
	}
	item.Version++
	item.Closed, item.Pass, item.SourceID = false, loop.Pass, source
	item.Answer, item.AnswerNote, item.AnsweredAt, item.Posted, item.Delivered = "", "", "", false, false
	item.URL = fmt.Sprintf("https://github.com/%s/pull/%d", repo, layer.Number)
	if problem == githubConflicting {
		item.Title = fmt.Sprintf("#%d conflicts with its base %s at %s: merge %s into it, or restack it", layer.Number, layer.BaseBranch, short(layer.HeadSHA), layer.BaseBranch)
		item.Body = "GitHub reports this pull request CONFLICTING: it cannot be merged until the conflicts with its base are resolved."
	} else {
		item.Title = fmt.Sprintf("#%d is behind its base %s at %s: merge %s into it, or restack it", layer.Number, layer.BaseBranch, short(layer.HeadSHA), layer.BaseBranch)
		item.Body = "GitHub reports this pull request BEHIND: its base requires branches to be up to date before merging."
	}
	return item, true, true
}

// applyMergedLayer is what a merged layer with open layers above it makes
// of its item (spec LEG-559 § The steward): raised once, asking the author
// to restack the rest onto the merged layer's base, and closed once every
// open layer above has a new head (the restack) or none is left open.
func (s *ReviewPassService) applyMergedLayer(l ReviewLoop, loop *store.HandoffReview, repo string, layer PRWatch, above []PRWatch,
	byID map[string]store.HandoffReviewItem) (store.HandoffReviewItem, bool, bool) {
	id := fmt.Sprintf("%s%d", mergedLayerPrefix, layer.Number)
	item, known := byID[id]
	heads := make([]string, 0, len(above))
	for _, up := range above {
		heads = append(heads, fmt.Sprintf("%d:%s", up.Number, up.HeadSHA))
	}
	switch {
	case known && item.Closed:
		return item, false, false
	case known:
		restacked := true
		for _, head := range heads {
			restacked = restacked && !slices.Contains(strings.Split(item.SourceID, ","), head)
		}
		if restacked {
			item.Closed = true
			return item, true, false
		}
		return item, false, false
	case len(above) == 0:
		return item, false, false
	}
	names := make([]string, 0, len(above))
	for _, up := range above {
		names = append(names, fmt.Sprintf("#%d", up.Number))
	}
	item = store.HandoffReviewItem{ID: id, TaskID: l.TaskID, Kind: ItemMergedLayer, Pass: loop.Pass, Repo: repo, PR: layer.Number, Version: 1,
		Author: "GitHub", CreatedAt: s.now(), SourceID: strings.Join(heads, ","),
		URL:   fmt.Sprintf("https://github.com/%s/pull/%d", repo, layer.Number),
		Title: fmt.Sprintf("#%d merged into %s: restack the rest (%s) onto %s", layer.Number, layer.BaseBranch, joinAnd(names), layer.BaseBranch),
		Body:  fmt.Sprintf("A lower layer of the stack merged. Restack the layers above it onto %s and push each one that moved; Legato reviews them again after the push.", layer.BaseBranch)}
	return item, true, true
}

// countedWatchChecks are a watched layer's counted checks: those branch
// protection requires, else the configured ones (as Checks counts them).
func countedWatchChecks(w PRWatch, configured []string) []CheckResult {
	required := false
	for _, c := range w.Checks {
		required = required || c.Required
	}
	var out []CheckResult
	for _, c := range w.Checks {
		if (required && !c.Required) || (!required && !slices.Contains(configured, c.Name)) {
			continue
		}
		out = append(out, CheckResult{Name: c.Name, State: c.State, Conclusion: c.Conclusion, URL: c.URL})
	}
	return out
}

// stillActive moves a stewarding loop's idle deadline on: a push, a
// comment, a review or a check change.
func (s *ReviewPassService) stillActive(ctx context.Context, loopID string) {
	rec, err := s.db.GetReviewLoop(ctx, loopID)
	if err != nil || !stewarding(rec) {
		return
	}
	deadline := s.opts.Now().Add(StewardIdleAfter).UTC().Format(time.RFC3339)
	if _, err := s.db.UpdateReviewLoop(ctx, rec.ID, rec.Rev, func(t *store.ReviewLoopTx) error {
		t.Loop.IdleDeadline = deadline
		return nil
	}); err != nil {
		log.Printf("review passes: %s: %v", loopID, err)
	}
}

// watching moves an approved, stewarding loop on: a later APPROVE for a
// layer whose checks settled, the routed items told to the author once it
// is idle, and a push to any watched head re-enters review.
func (s *ReviewPassService) watching(ctx context.Context, l ReviewLoop, loop *store.HandoffReview, rec *store.ReviewLoop) error {
	if rec.Held {
		// Stop watching: nothing is sent and nothing is reviewed; a push
		// made meanwhile re-enters once Watch again lets it.
		return nil
	}
	if err := s.approveSettled(ctx, l, loop); err != nil {
		log.Printf("review passes: %s: %v", l.TaskID, err)
	}
	if err := s.deliverComments(ctx, l, loop); err != nil {
		return err
	}
	w, ok := s.freshWatch(rec.ID)
	if !ok || !s.movedSinceApproval(ctx, rec, w) {
		return nil
	}
	if rec.MaxReentries > 0 && rec.Reentries >= rec.MaxReentries {
		// The re-entry budget is spent: the operator decides (Watch
		// again allows one more), and no pass starts.
		return s.stuck(ctx, l, loop, fmt.Sprintf(reentryBudgetReason, rec.Reentries, rec.MaxReentries))
	}
	return s.reenter(ctx, l)
}

// movedSinceApproval says whether a watch read finds an open layer's head
// past the head Confirm approved it at.
func (s *ReviewPassService) movedSinceApproval(ctx context.Context, rec *store.ReviewLoop, w ReviewLoopWatch) bool {
	layers, err := s.db.ListReviewLoopPRs(ctx, rec.ID)
	if err != nil {
		return false
	}
	approved := map[int]string{}
	for _, layer := range layers {
		approved[layer.Number] = layer.CleanHead
	}
	for _, layer := range w.Layers {
		if head := approved[layer.Number]; layer.State == "open" && head != "" && layer.HeadSHA != head {
			return true
		}
	}
	return false
}

// reenter sends an approved, stewarding loop back to review after a push:
// one re-entry, however many layers moved (the pass that follows pins every
// head when the author is idle, and pushes during it fold in). The tick
// starts the pass.
func (s *ReviewPassService) reenter(ctx context.Context, l ReviewLoop) error {
	now := s.opts.Now().UTC()
	if _, err := s.db.UpdateHostedReviewLoop(ctx, l.TaskID, func(t *store.ReviewLoopTx) error {
		t.Loop.Phase, t.Loop.Reason = ReviewPhaseReady, ""
		t.Loop.Reentries++
		t.Loop.LastWake, t.Loop.LastWakeAt = StewardWokePushed, now.Format(time.RFC3339)
		t.Loop.IdleDeadline = now.Add(StewardIdleAfter).Format(time.RFC3339)
		return nil
	}); err != nil {
		return err
	}
	s.changed(l.TaskID)
	return nil
}

// woke records what woke a stewarding loop's watch, for one waiting for its
// author after a re-entry's pass: a push or a dispute.
func (s *ReviewPassService) woke(ctx context.Context, taskID, wake string) {
	now := s.opts.Now().UTC()
	if _, err := s.db.UpdateHostedReviewLoop(ctx, taskID, func(t *store.ReviewLoopTx) error {
		t.Loop.LastWake, t.Loop.LastWakeAt = wake, now.Format(time.RFC3339)
		if stewarding(t.Loop) {
			t.Loop.IdleDeadline = now.Add(StewardIdleAfter).Format(time.RFC3339)
		}
		return nil
	}); err != nil {
		log.Printf("review passes: %s: %v", taskID, err)
	}
}
