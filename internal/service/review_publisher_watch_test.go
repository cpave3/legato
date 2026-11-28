package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

// watchedPull is a pull request as the watch query reads it.
const watchedPull12 = `{"number":12,"state":"OPEN","isDraft":false,"headRefOid":"aaa111","headRefName":"feature","baseRefName":"main",
	"mergeable":"MERGEABLE","mergeStateStatus":"BLOCKED","reviewDecision":"REVIEW_REQUIRED","updatedAt":"2026-11-25T10:00:00Z",
	"commits":{"nodes":[{"commit":{"oid":"aaa111","statusCheckRollup":{"state":"FAILURE","contexts":{"pageInfo":{"hasNextPage":false},"nodes":[
		{"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"https://ci/b","databaseId":77,"isRequired":true},
		{"__typename":"CheckRun","name":"lint","status":"IN_PROGRESS","conclusion":null,"detailsUrl":"https://ci/l","databaseId":78,"isRequired":false},
		{"__typename":"StatusContext","context":"ci/legacy","state":"SUCCESS","targetUrl":"https://ci/s","isRequired":true}]}}}}]}}`

const watchedPull13 = `{"number":13,"state":"MERGED","isDraft":true,"headRefOid":"bbb222","headRefName":"feature-2","baseRefName":"feature",
	"mergeable":"UNKNOWN","mergeStateStatus":"UNKNOWN","reviewDecision":null,"updatedAt":"2026-11-25T11:00:00Z",
	"commits":{"nodes":[{"commit":{"oid":"bbb222","statusCheckRollup":null}}]}}`

// The steward's watch is one GraphQL read of every layer of a loop: head,
// state, mergeable, mergeStateStatus, reviewDecision and the check rollup
// (spec LEG-559 C7, AC 38).
func TestTheWatchReadsEveryLayerInOneGraphQLQuery(t *testing.T) {
	f := newPublisherFixture(t)
	f.github.watched = map[int]string{12: watchedPull12, 13: watchedPull13}
	ctx := context.Background()

	session := f.publisher.WatchSession("cpave3/legato")
	defer session.Close()
	layers, err := session.Watch(ctx, []int{12, 13})
	if err != nil {
		t.Fatal(err)
	}
	if f.github.watchReads != 1 {
		t.Fatalf("%d watch queries, want 1", f.github.watchReads)
	}
	if f.github.watchVars["owner"] != "cpave3" || f.github.watchVars["name"] != "legato" {
		t.Fatalf("variables %v", f.github.watchVars)
	}
	if len(layers) != 2 || layers[0].Number != 12 || layers[1].Number != 13 {
		t.Fatalf("layers %+v", layers)
	}
	a := layers[0]
	if a.State != "open" || a.Draft || a.HeadSHA != "aaa111" || a.HeadBranch != "feature" || a.BaseBranch != "main" ||
		a.Mergeable != "MERGEABLE" || a.MergeState != "BLOCKED" || a.ReviewDecision != "REVIEW_REQUIRED" || a.Rollup != CheckFailing ||
		!a.UpdatedAt.Equal(time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)) || a.ChecksTruncated {
		t.Fatalf("layer 12 %+v", a)
	}
	want := []WatchCheck{
		{Name: "build", State: CheckFailing, Conclusion: "FAILURE", Required: true, URL: "https://ci/b", RunID: 77},
		{Name: "lint", State: CheckPending, URL: "https://ci/l", RunID: 78},
		{Name: "ci/legacy", State: CheckPassing, Conclusion: "SUCCESS", Required: true, URL: "https://ci/s"},
	}
	if len(a.Checks) != len(want) {
		t.Fatalf("checks %+v", a.Checks)
	}
	for i := range want {
		if a.Checks[i] != want[i] {
			t.Fatalf("check %d: %+v, want %+v", i, a.Checks[i], want[i])
		}
	}
	b := layers[1]
	if b.State != "merged" || !b.Draft || b.HeadSHA != "bbb222" || b.ReviewDecision != "" || b.Rollup != "" || len(b.Checks) != 0 {
		t.Fatalf("layer 13 %+v", b)
	}

	// A pull request GitHub does not have is an error, never an empty layer.
	if _, err := session.Watch(ctx, []int{99}); err == nil {
		t.Fatal("watching a missing pull request succeeded")
	}
}

// Each context of a check rollup becomes a check by its kind: a check run
// passes on SUCCESS, NEUTRAL or SKIPPED, fails on any other conclusion and
// is pending until it completes with one; a status context takes its
// state; any other kind of context is left out.
func TestTheWatchMapsEachCheckContext(t *testing.T) {
	read := `{"number":7,"state":"OPEN","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"PENDING","contexts":{"pageInfo":{"hasNextPage":true},"nodes":[
		{"__typename":"CheckRun","name":"neutral","status":"COMPLETED","conclusion":"NEUTRAL","databaseId":1},
		{"__typename":"CheckRun","name":"skipped","status":"COMPLETED","conclusion":"SKIPPED","databaseId":2},
		{"__typename":"CheckRun","name":"cancelled","status":"COMPLETED","conclusion":"CANCELLED","databaseId":3},
		{"__typename":"CheckRun","name":"no-conclusion","status":"COMPLETED","conclusion":null,"databaseId":4},
		{"__typename":"CheckRun","name":"queued","status":"QUEUED","conclusion":"SUCCESS","databaseId":5},
		{"__typename":"SomethingNew","name":"ignored"},
		{"__typename":"StatusContext","context":"errored","state":"ERROR","targetUrl":"https://ci/e"},
		{"__typename":"StatusContext","context":"expected","state":"EXPECTED","isRequired":true}]}}}}]}}`
	var pull watchedPull
	if err := json.Unmarshal([]byte(read), &pull); err != nil {
		t.Fatal(err)
	}
	w := pull.watch(7)
	if w.Rollup != CheckPending || !w.ChecksTruncated {
		t.Fatalf("rollup %q, truncated %v", w.Rollup, w.ChecksTruncated)
	}
	want := []WatchCheck{
		{Name: "neutral", State: CheckPassing, Conclusion: "NEUTRAL", RunID: 1},
		{Name: "skipped", State: CheckPassing, Conclusion: "SKIPPED", RunID: 2},
		{Name: "cancelled", State: CheckFailing, Conclusion: "CANCELLED", RunID: 3},
		{Name: "no-conclusion", State: CheckPending, RunID: 4},
		{Name: "queued", State: CheckPending, RunID: 5},
		{Name: "errored", State: CheckFailing, Conclusion: "ERROR", URL: "https://ci/e"},
		{Name: "expected", State: CheckPending, Conclusion: "EXPECTED", Required: true},
	}
	if !slices.Equal(w.Checks, want) {
		t.Fatalf("checks\n%+v\nwant\n%+v", w.Checks, want)
	}
}

// The watch reads the reviews a person dismissed, so the steward can tell
// a dismissed approval with no push from one a push dismissed (spec
// LEG-559 § Approval after re-entry, AC 36).
func TestTheWatchReadsTheDismissedReviews(t *testing.T) {
	f := newPublisherFixture(t)
	dismissed := strings.Replace(watchedPull13, `"updatedAt"`, `"reviews":{"nodes":[{"fullDatabaseId":"901"},{"fullDatabaseId":"902"}]},"updatedAt"`, 1)
	f.github.watched = map[int]string{12: watchedPull12, 13: dismissed}
	session := f.publisher.WatchSession("cpave3/legato")
	defer session.Close()
	layers, err := session.Watch(context.Background(), []int{12, 13})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.github.watchQuery, "states: [DISMISSED]") {
		t.Fatalf("the watch asks no dismissed reviews: %s", f.github.watchQuery)
	}
	if len(layers[0].DismissedReviews) != 0 || !slices.Equal(layers[1].DismissedReviews, []int64{901, 902}) {
		t.Fatalf("dismissed %v and %v", layers[0].DismissedReviews, layers[1].DismissedReviews)
	}
}

// One session is one loop's reads in one tick: one token for the watch
// and every comment read, and the comments read by ETag (AC 38).
func TestAWatchSessionUsesOneTokenAndReadsCommentsByETag(t *testing.T) {
	f := newPublisherFixture(t)
	f.github.watched = map[int]string{12: watchedPull12}
	f.github.lists = listsOf("cpave3/legato", 12)
	ctx := context.Background()
	pages := memoryPollPages{}

	minted := len(f.github.tokens) + len(f.github.revoked)
	session := f.publisher.WatchSession("cpave3/legato")
	if _, err := session.Watch(ctx, []int{12}); err != nil {
		t.Fatal(err)
	}
	poll, err := session.PollComments(ctx, 12, pages)
	if err != nil || !poll.Changed {
		t.Fatalf("first comment read: %+v, %v", poll, err)
	}
	session.Close()
	if got := len(f.github.tokens) + len(f.github.revoked) - minted; got != 1 {
		t.Fatalf("%d tokens made for one session, want 1", got)
	}
	used := map[string]bool{}
	for _, b := range f.github.bearers {
		used[b] = true
	}
	if len(used) != 1 {
		t.Fatalf("the session's requests carried %d tokens, want 1: %v", len(used), f.github.bearers)
	}

	// The next tick's session: nothing changed, every list page is a 304.
	before := f.github.notModified
	session = f.publisher.WatchSession("cpave3/legato")
	defer session.Close()
	poll, err = session.PollComments(ctx, 12, pages)
	if err != nil || poll.Changed || f.github.notModified-before != 3 {
		t.Fatalf("second comment read: changed %v, 304s %d, %v", poll.Changed, f.github.notModified-before, err)
	}
}

// A rate limit on the watch is typed, and the session's later reads wait
// it out without asking GitHub (LEG-564).
func TestARateLimitedWatchIsTypedAndTheSessionWaits(t *testing.T) {
	f := newPublisherFixture(t)
	f.github.watched = map[int]string{12: watchedPull12}
	f.github.lists = listsOf("cpave3/legato", 12)
	f.github.graphqlLimit = time.Now().Add(time.Hour)
	ctx := context.Background()

	session := f.publisher.WatchSession("cpave3/legato")
	defer session.Close()
	_, err := session.Watch(ctx, []int{12})
	var limit *GitHubRateLimit
	if !errors.As(err, &limit) || limit.Installation != "reviewer-app@cpave3" {
		t.Fatalf("rate-limited watch: %v", err)
	}
	hits := f.github.hits("cpave3/legato")
	if _, err := session.PollComments(ctx, 12, memoryPollPages{}); !errors.As(err, new(*GitHubRateLimit)) {
		t.Fatalf("a read after the limit: %v", err)
	}
	if f.github.hits("cpave3/legato") != hits {
		t.Fatal("a read during the back-off reached GitHub")
	}
}

// The watch reads the base branch's newest commits' checks in the same
// query, and the steward counts every check on the head: a failing one the
// base fails too is noted, not routed (LEG-711).
func TestTheWatchReadsTheBasesChecksAndCountsEveryCheck(t *testing.T) {
	read := `{"number":458,"state":"OPEN","headRefOid":"aaa","baseRefName":"main",
	"baseRef":{"target":{"history":{"nodes":[
		{"statusCheckRollup":{"contexts":{"nodes":[{"__typename":"CheckRun","name":"go","status":"IN_PROGRESS","conclusion":null},{"__typename":"StatusContext","context":"ci/stack","state":"FAILURE"}]}}},
		{"statusCheckRollup":{"contexts":{"nodes":[{"__typename":"CheckRun","name":"go","status":"COMPLETED","conclusion":"SUCCESS"},{"__typename":"CheckRun","name":"docs","status":"COMPLETED","conclusion":"FAILURE"}]}}},
		{"statusCheckRollup":null}]}}},
	"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE","contexts":{"pageInfo":{"hasNextPage":false},"nodes":[
		{"__typename":"CheckRun","name":"go","status":"COMPLETED","conclusion":"FAILURE","databaseId":1},
		{"__typename":"CheckRun","name":"docs","status":"COMPLETED","conclusion":"FAILURE","databaseId":2},
		{"__typename":"StatusContext","context":"ci/stack","state":"FAILURE"},
		{"__typename":"CheckRun","name":"web","status":"COMPLETED","conclusion":"CANCELLED","databaseId":3}]}}}}]}}`
	var pull watchedPull
	if err := json.Unmarshal([]byte(read), &pull); err != nil {
		t.Fatal(err)
	}
	w := pull.watch(458)
	if len(w.BaseHistory) != 3 || len(w.BaseHistory[0]) != 2 || len(w.BaseHistory[2]) != 0 {
		t.Fatalf("base history %+v", w.BaseHistory)
	}
	got := map[string]string{}
	for _, c := range countedWatchChecks(w, nil) {
		got[c.Name] = c.State
	}
	if len(got) != 3 || got["go"] != CheckFailing || got["docs"] != CheckFailingOnBase || got["ci/stack"] != CheckFailingOnBase {
		t.Fatalf("counted %v", got)
	}
	if !strings.Contains(watchFields, "baseRef") {
		t.Fatal("the watch query reads nothing of the base")
	}
}
