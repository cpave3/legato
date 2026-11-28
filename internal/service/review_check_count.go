package service

import (
	"slices"
	"strconv"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// CheckFailingOnBase is a check failing on a pull request's head that also
// fails on its base branch: not the pull request's doing, so it is noted
// on the loop and never routed to the author (LEG-711).
const CheckFailingOnBase = "failing_on_base"

// AnswerOnBase marks a closed check item whose check fails on the base
// too: the Shipping and run pages list it as "also failing on <base>".
const AnswerOnBase = "on_base"

// baseHistoryDepth is how many of the base's newest commits are read for
// the newest completed run of a check (the newest may still be running).
const baseHistoryDepth = 3

// explicitChecks are the checks that count whatever the base does: those
// branch protection requires (the head says so), else the configured ones.
func explicitChecks(head []WatchCheck, configured []string) []string {
	var required []string
	for _, c := range head {
		if c.Required && !slices.Contains(required, c.Name) {
			required = append(required, c.Name)
		}
	}
	if len(required) > 0 {
		return required
	}
	return configured
}

// countChecks are the checks of a pull request's head that count (LEG-711):
// every one, each name once (latestRun). The explicit ones come first, in
// their order, and count as they stand, a cancelled one pending a re-run
// (with missingPending, one the head lacks is pending too). Every other
// check follows in the head's order: a cancelled one is left out, and a
// failing one the base fails too (onBase) is CheckFailingOnBase.
func countChecks(head []WatchCheck, explicit []string, missingPending bool, onBase func(name string) bool) []WatchCheck {
	var names []string
	for _, c := range head {
		if !slices.Contains(names, c.Name) {
			names = append(names, c.Name)
		}
	}
	var out []WatchCheck
	for _, name := range explicit {
		c, ok := latestRun(head, name)
		switch {
		case !ok && missingPending:
			out = append(out, WatchCheck{Name: name, State: CheckPending})
		case !ok:
		case cancelled(c):
			c.State = CheckPending
			out = append(out, c)
		default:
			out = append(out, c)
		}
	}
	for _, name := range names {
		if slices.Contains(explicit, name) {
			continue
		}
		c, _ := latestRun(head, name)
		if cancelled(c) {
			continue
		}
		if c.State == CheckFailing && onBase(name) {
			c.State = CheckFailingOnBase
		}
		out = append(out, c)
	}
	return out
}

// latestRun is the run of a check that stands among several of the same
// name: one not cancelled over a cancelled one (a newer push or a re-run
// superseded it), then the newest (the larger run ID).
func latestRun(checks []WatchCheck, name string) (WatchCheck, bool) {
	var best WatchCheck
	found := false
	for _, c := range checks {
		if c.Name != name {
			continue
		}
		switch {
		case !found:
		case cancelled(best) != cancelled(c):
			if cancelled(c) {
				continue
			}
		case c.RunID <= best.RunID:
			continue
		}
		best, found = c, true
	}
	return best, found
}

// cancelled says a check run was cancelled: GitHub's REST and GraphQL
// spell it in different cases.
func cancelled(c WatchCheck) bool {
	return strings.EqualFold(c.Conclusion, "cancelled")
}

// failingOnBase says whether the base fails a check: the newest completed,
// not cancelled run of it among the base's commits (newest first) failed.
// A check the base never finished is not the base's.
func failingOnBase(history [][]WatchCheck, name string) bool {
	for _, commit := range history {
		c, ok := latestRun(commit, name)
		if !ok || c.State == CheckPending || cancelled(c) {
			continue
		}
		return c.State == CheckFailing
	}
	return false
}

// goneChecks are the open check items of a pull request read on an
// earlier head whose check the new head does not run at all (names are
// every check on it): closed, as nothing will report them again. A head
// with no checks yet closes nothing, as its CI may not have started.
func goneChecks(items []store.HandoffReviewItem, number int, head string, names []string) []store.HandoffReviewItem {
	if len(names) == 0 {
		return nil
	}
	var out []store.HandoffReviewItem
	for _, item := range items {
		if item.Kind != ItemCheck || item.PR != number || item.Closed || item.SourceID == head {
			continue
		}
		name, ok := strings.CutPrefix(item.ID, "k-"+strconv.Itoa(number)+"-")
		if !ok || slices.Contains(names, name) {
			continue
		}
		item.Closed = true
		out = append(out, item)
	}
	return out
}
