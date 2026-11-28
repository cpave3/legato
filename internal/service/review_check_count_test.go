package service

import (
	"slices"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func checkStates(checks []WatchCheck) map[string]string {
	out := map[string]string{}
	for _, c := range checks {
		out[c.Name] = c.State
	}
	return out
}

func neverOnBase(string) bool { return false }

// With no branch protection and nothing configured, every check on the
// head counts: a failing one is routed (LEG-711, PR #458's go job).
func TestEveryCheckCountsWithoutProtectionOrConfiguration(t *testing.T) {
	head := []WatchCheck{
		{Name: "go", State: CheckFailing, Conclusion: "failure", RunID: 10},
		{Name: "web", State: CheckPassing, Conclusion: "success", RunID: 11},
		{Name: "docs", State: CheckPending, RunID: 12},
	}
	got := checkStates(countChecks(head, nil, false, neverOnBase))
	want := map[string]string{"go": CheckFailing, "web": CheckPassing, "docs": CheckPending}
	if len(got) != len(want) {
		t.Fatalf("counted %v, want %v", got, want)
	}
	for name, state := range want {
		if got[name] != state {
			t.Fatalf("counted %v, want %v", got, want)
		}
	}
}

// A failing check that also fails on the base is noted, not routed; one
// that fails on the head only is routed.
func TestAFailingCheckAlsoFailingOnTheBaseIsNotRouted(t *testing.T) {
	head := []WatchCheck{
		{Name: "go", State: CheckFailing, Conclusion: "failure", RunID: 1},
		{Name: "lint", State: CheckFailing, Conclusion: "failure", RunID: 2},
	}
	onBase := func(name string) bool { return name == "lint" }
	got := checkStates(countChecks(head, nil, false, onBase))
	if got["go"] != CheckFailing || got["lint"] != CheckFailingOnBase {
		t.Fatalf("counted %v", got)
	}
}

// A check still running is pending, never failing; skipped and neutral
// pass; a cancelled one does not count, and a cancelled run a newer run of
// the same check superseded is ignored.
func TestRunningSkippedAndCancelledChecksDoNotFail(t *testing.T) {
	head := []WatchCheck{
		{Name: "go", State: CheckPending, RunID: 5},
		{Name: "skipped", State: CheckPassing, Conclusion: "SKIPPED", RunID: 6},
		{Name: "neutral", State: CheckPassing, Conclusion: "NEUTRAL", RunID: 7},
		{Name: "cancelled", State: CheckFailing, Conclusion: "CANCELLED", RunID: 8},
		{Name: "web", State: CheckFailing, Conclusion: "CANCELLED", RunID: 20},
		{Name: "web", State: CheckPassing, Conclusion: "SUCCESS", RunID: 19},
		{Name: "e2e", State: CheckFailing, Conclusion: "FAILURE", RunID: 30},
		{Name: "e2e", State: CheckPassing, Conclusion: "SUCCESS", RunID: 31},
	}
	got := checkStates(countChecks(head, nil, false, neverOnBase))
	want := map[string]string{"go": CheckPending, "skipped": CheckPassing, "neutral": CheckPassing, "web": CheckPassing, "e2e": CheckPassing}
	if len(got) != len(want) {
		t.Fatalf("counted %v, want %v", got, want)
	}
	for name, state := range want {
		if got[name] != state {
			t.Fatalf("counted %v, want %v", got, want)
		}
	}
}

// Branch protection's required checks count as before, failing even when
// the base fails them too, and absent ones are pending; every other failing
// check counts as well, unless the base fails it.
func TestRequiredChecksCountAndSoDoesAnyOtherFailingCheck(t *testing.T) {
	head := []WatchCheck{
		{Name: "extra", State: CheckFailing, Conclusion: "failure", RunID: 3},
		{Name: "build", State: CheckFailing, Conclusion: "failure", RunID: 1, Required: true},
		{Name: "flaky-on-main", State: CheckFailing, Conclusion: "failure", RunID: 4},
		{Name: "cancelled-required", State: CheckFailing, Conclusion: "cancelled", RunID: 5, Required: true},
	}
	onBase := func(name string) bool { return name == "build" || name == "flaky-on-main" }
	explicit := []string{"build", "cancelled-required", "lint"}
	counted := countChecks(head, explicit, true, onBase)
	var names []string
	for _, c := range counted {
		names = append(names, c.Name)
	}
	// The explicit ones first, in their order, then the rest as the head has
	// them.
	if want := []string{"build", "cancelled-required", "lint", "extra", "flaky-on-main"}; !slices.Equal(names, want) {
		t.Fatalf("order %v, want %v", names, want)
	}
	got := checkStates(counted)
	if got["build"] != CheckFailing || got["cancelled-required"] != CheckPending || got["lint"] != CheckPending ||
		got["extra"] != CheckFailing || got["flaky-on-main"] != CheckFailingOnBase {
		t.Fatalf("counted %v", got)
	}
}

func TestExplicitChecksAreTheRequiredElseTheConfigured(t *testing.T) {
	head := []WatchCheck{{Name: "a", Required: true}, {Name: "b"}, {Name: "a", Required: true}}
	if got := explicitChecks(head, []string{"c"}); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("with protection: %v", got)
	}
	if got := explicitChecks([]WatchCheck{{Name: "b"}}, []string{"c"}); !slices.Equal(got, []string{"c"}) {
		t.Fatalf("without: %v", got)
	}
}

// The base fails a check when the newest completed run of it among the
// base's newest commits failed: a run still going, or a cancelled one, is
// passed over for an older commit's.
func TestFailingOnBaseReadsTheNewestCompletedRun(t *testing.T) {
	history := [][]WatchCheck{
		{{Name: "go", State: CheckPending}, {Name: "web", State: CheckPassing, Conclusion: "SUCCESS"}, {Name: "docs", State: CheckFailing, Conclusion: "CANCELLED"}},
		{{Name: "go", State: CheckFailing, Conclusion: "FAILURE"}, {Name: "web", State: CheckFailing, Conclusion: "FAILURE"}, {Name: "docs", State: CheckFailing, Conclusion: "FAILURE"}},
	}
	for name, want := range map[string]bool{"go": true, "web": false, "docs": true, "missing": false} {
		if got := failingOnBase(history, name); got != want {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
}

// A check failing on the base too closes its item as noted on the base,
// never handled by the author; it opens again when only the head fails it,
// and a pass clears the note.
func TestACheckFailingOnTheBaseIsNotedOnAClosedItem(t *testing.T) {
	s := &ReviewPassService{opts: ReviewPassOptions{}}
	s.opts.Now = func() time.Time { return time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC) }
	l := ReviewLoop{TaskID: "T-1", Repo: "cpave3/legato"}
	loop := &store.HandoffReview{TaskID: "T-1", Pass: 2}

	item, write, fresh := s.applyCheck(l, loop, 458, "head1", CheckResult{Name: "go", State: CheckFailingOnBase, Conclusion: "failure", URL: "u"}, store.HandoffReviewItem{}, false)
	if !write || fresh || !item.Closed || item.Answer != AnswerOnBase || item.Kind != ItemCheck || item.ID != "k-458-go" || !item.Delivered {
		t.Fatalf("new on-base item %+v, write %v, fresh %v", item, write, fresh)
	}
	if _, write, _ := s.applyCheck(l, loop, 458, "head1", CheckResult{Name: "go", State: CheckFailingOnBase}, item, true); write {
		t.Fatal("rewrote an unchanged on-base item")
	}

	// Main is green again and the head still fails: the author's.
	again, write, fresh := s.applyCheck(l, loop, 458, "head1", CheckResult{Name: "go", State: CheckFailing, Conclusion: "failure"}, item, true)
	if !write || !fresh || again.Closed || again.Answer != "" || again.Version != item.Version+1 {
		t.Fatalf("reopened %+v, write %v, fresh %v", again, write, fresh)
	}

	// An open item whose check the base now fails too is closed, noted.
	closed, write, _ := s.applyCheck(l, loop, 458, "head2", CheckResult{Name: "go", State: CheckFailingOnBase}, again, true)
	if !write || !closed.Closed || closed.Answer != AnswerOnBase {
		t.Fatalf("closed %+v", closed)
	}

	// It passes: the note goes.
	passed, write, _ := s.applyCheck(l, loop, 458, "head3", CheckResult{Name: "go", State: CheckPassing}, closed, true)
	if !write || !passed.Closed || passed.Answer != "" {
		t.Fatalf("passed %+v", passed)
	}
}

// An open check item whose check the new head does not run at all is
// closed; one read on this head, or on a head with no checks yet, stays.
func TestACheckGoneFromTheNewHeadClosesItsItem(t *testing.T) {
	items := []store.HandoffReviewItem{
		{ID: "k-7-docs", Kind: ItemCheck, PR: 7, SourceID: "old"},
		{ID: "k-7-go", Kind: ItemCheck, PR: 7, SourceID: "old"},
		{ID: "k-7-web", Kind: ItemCheck, PR: 7, SourceID: "new"},
		{ID: "k-8-docs", Kind: ItemCheck, PR: 8, SourceID: "old"},
		{ID: "k-7-lint", Kind: ItemCheck, PR: 7, SourceID: "old", Closed: true},
		{ID: "c-7-1", Kind: ItemComment, PR: 7, SourceID: "old"},
	}
	closed := goneChecks(items, 7, "new", []string{"go"})
	if len(closed) != 1 || closed[0].ID != "k-7-docs" || !closed[0].Closed {
		t.Fatalf("closed %+v", closed)
	}
	if closed := goneChecks(items, 7, "new", nil); len(closed) != 0 {
		t.Fatalf("closed on a head with no checks yet: %+v", closed)
	}
}
