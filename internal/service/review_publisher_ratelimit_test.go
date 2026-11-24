package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func secondaryLimit(retryAfter string) *fakeLimit {
	l := &fakeLimit{status: 403, message: "You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}
	if retryAfter != "" {
		l.headers = map[string]string{"retry-after": retryAfter}
	}
	return l
}

func TestASecondaryLimitWaitsForRetryAfterElseAMinute(t *testing.T) {
	for _, tc := range []struct {
		name       string
		retryAfter string
		wait       time.Duration
	}{
		{"with retry-after", "30", 30 * time.Second},
		{"without it", "", time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublisherFixture(t)
			ctx := context.Background()
			f.github.setLimit(secondaryLimit(tc.retryAfter))
			start := f.clock.Now()

			_, err := f.publisher.OpenPulls(ctx, "cpave3/legato", PullQuery{Base: "main"})

			var limit *GitHubRateLimit
			if !errors.As(err, &limit) {
				t.Fatalf("err = %#v; want a rate limit", err)
			}
			if !limit.Secondary || limit.Installation != "reviewer-app@cpave3" || limit.RetryAfter != tc.wait {
				t.Fatalf("limit = %+v", limit)
			}
			if got := limit.RetryAt.Sub(start); got < tc.wait || got > tc.wait+5*time.Second {
				t.Fatalf("retry at %v after the limit; want %v", got, tc.wait)
			}

			// GitHub would answer now, but the installation backs off
			// until then: nothing reaches GitHub.
			f.github.setLimit(nil)
			hits := f.github.hits("cpave3/legato")
			f.clock.Advance(tc.wait - 2*time.Second)
			if _, err := f.publisher.OpenPulls(ctx, "cpave3/legato", PullQuery{Base: "main"}); !errors.As(err, &limit) {
				t.Fatalf("before the wait is over: %v", err)
			}
			if f.github.hits("cpave3/legato") != hits {
				t.Fatal("a call reached GitHub while the installation backs off")
			}
			f.clock.Advance(5 * time.Second)
			if _, err := f.publisher.OpenPulls(ctx, "cpave3/legato", PullQuery{Base: "main"}); err != nil {
				t.Fatalf("after the wait: %v", err)
			}
			if f.github.hits("cpave3/legato") != hits+1 {
				t.Fatal("the call after the wait did not reach GitHub")
			}
		})
	}
}

// listsOf gives a pull request's three comment lists one empty page each.
func listsOf(repo string, number int) map[string][]string {
	return map[string][]string{
		fmt.Sprintf("/repos/%s/pulls/%d/reviews", repo, number):   {"[]"},
		fmt.Sprintf("/repos/%s/pulls/%d/comments", repo, number):  {"[]"},
		fmt.Sprintf("/repos/%s/issues/%d/comments", repo, number): {"[]"},
	}
}

func TestAGraphQLRateLimitIsTypedAndTheInstallationWaitsForTheReset(t *testing.T) {
	f := newPublisherFixture(t)
	ctx := context.Background()
	f.github.lists = map[string][]string{
		"/repos/cpave3/legato/pulls/12/reviews":   {"[]"},
		"/repos/cpave3/legato/pulls/12/comments":  {`[{"id":501,"user":{"login":"amy","type":"User"},"author_association":"MEMBER","body":"Nil check.","path":"a.go","line":4,"html_url":"c501"}]`},
		"/repos/cpave3/legato/issues/12/comments": {"[]"},
	}
	reset := time.Unix(f.clock.Now().Add(10*time.Minute).Unix(), 0)
	f.github.graphqlLimit = reset

	// The lists changed, so the threads are read: GraphQL answers 200 with
	// a RATE_LIMITED error.
	_, err := f.publisher.PollComments(ctx, "cpave3/legato", 12, memoryPollPages{})

	var limit *GitHubRateLimit
	if !errors.As(err, &limit) {
		t.Fatalf("err = %#v; want a rate limit", err)
	}
	if limit.Secondary || limit.Installation != "reviewer-app@cpave3" || !limit.Reset.Equal(reset) || !limit.RetryAt.Equal(reset) {
		t.Fatalf("limit = %+v; want a primary limit until %v", limit, reset)
	}

	// Until the reset, polls of the installation's pull requests reach no
	// GitHub endpoint.
	f.github.mu.Lock()
	f.github.graphqlLimit = time.Time{}
	f.github.mu.Unlock()
	rest, graphql := f.github.hits("cpave3/legato"), f.github.hits("graphql")
	f.clock.Advance(9 * time.Minute)
	if _, err := f.publisher.PollComments(ctx, "cpave3/legato", 12, memoryPollPages{}); !errors.As(err, &limit) {
		t.Fatalf("before the reset: %v", err)
	}
	if f.github.hits("cpave3/legato") != rest || f.github.hits("graphql") != graphql {
		t.Fatal("a poll reached GitHub before the reset")
	}
	f.clock.Advance(2 * time.Minute)
	poll, err := f.publisher.PollComments(ctx, "cpave3/legato", 12, memoryPollPages{})
	if err != nil || len(poll.Comments) != 1 {
		t.Fatalf("after the reset: %+v, %v", poll, err)
	}
}

func TestALimitOnOneInstallationDoesNotHoldAnother(t *testing.T) {
	f := newPublisherFixture(t)
	ctx := context.Background()
	f.github.lists = listsOf("acme/widgets", 3)
	limit := secondaryLimit("120")
	limit.owner = "cpave3"
	f.github.setLimit(limit)

	if _, err := f.publisher.OpenPulls(ctx, "cpave3/legato", PullQuery{Base: "main"}); !errors.As(err, new(*GitHubRateLimit)) {
		t.Fatalf("cpave3: %v", err)
	}
	// acme is another installation of the same App: it carries on.
	if _, err := f.publisher.PollComments(ctx, "acme/widgets", 3, memoryPollPages{}); err != nil {
		t.Fatalf("acme: %v", err)
	}
	if f.github.hits("acme/widgets") != 3 {
		t.Fatalf("acme's lists reached GitHub %d times; want 3", f.github.hits("acme/widgets"))
	}
	// The author App is another App: its installation on cpave3 is not
	// held, while the reviewer App's still is.
	f.github.setLimit(nil)
	if _, err := f.publisher.PostIssueComment(ctx, AsAuthor, "cpave3/legato", 12, "Fixed.", answerKey("t1/rl-1/n1")); err != nil {
		t.Fatalf("the author App on cpave3: %v", err)
	}
	if _, err := f.publisher.PostIssueComment(ctx, AsReviewer, "cpave3/legato", 12, "x", answerKey("t1/rl-2/n2")); !errors.As(err, new(*GitHubRateLimit)) {
		t.Fatalf("the reviewer App on cpave3 again: %v", err)
	}
}

func TestAServerErrorFromGraphQLIsAGitHubError(t *testing.T) {
	f := newPublisherFixture(t)
	f.github.setLimit(&fakeLimit{status: 502, message: "Bad Gateway"})

	err := f.publisher.ResolveThread(context.Background(), "cpave3/legato", 12, 555)

	var gh *GitHubError
	if !errors.As(err, &gh) || gh.Status != 502 || errors.As(err, new(*GitHubRateLimit)) {
		t.Fatalf("err = %#v", err)
	}
}
