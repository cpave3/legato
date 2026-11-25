package service

import (
	"context"
	"errors"
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
