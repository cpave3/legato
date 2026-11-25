package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// LEG-561: a post whose outcome is unknown (a 5xx, a timeout) is found by
// its marker, with the inline comments GitHub made of it, or posted again
// once with the same marker.

// twoFindings has one finding in the diff and one outside it: GitHub makes
// one inline comment of the review, for the first.
func twoFindings() ReviewPost {
	post := cleanReview()
	post.Event, post.Marker = ReviewComment, "t1/pass-1/pr-12"
	post.Findings = []ReviewPostFinding{
		{Title: "Outside the diff", Body: "b0", Place: &ReviewPlace{Path: "main.go", Line: 10}},
		{Title: "Name the import", Body: "Prefer a named import.", Place: &ReviewPlace{Path: "main.go", Line: 2}},
	}
	return post
}

func TestAReviewGitHubTookBeforeA5xxIsFoundWithItsInlineComments(t *testing.T) {
	f := newPublisherFixture(t)
	f.github.acceptThenFail = 502

	posted, err := f.publisher.PostReview(context.Background(), twoFindings())

	if err != nil || len(f.github.reviews) != 1 || posted.ID != 100 || !posted.Existed {
		t.Fatalf("posted %+v, %v; reviews %d", posted, err, len(f.github.reviews))
	}
	if !slices.Equal(posted.Inline, []int{1}) || !slices.Equal(posted.CommentIDs, []int64{5000}) {
		t.Fatalf("inline %v, comment IDs %v", posted.Inline, posted.CommentIDs)
	}

	// The next try (Legato restarted): the review is found by its marker,
	// with its thread, and nothing is posted.
	f.github.acceptThenFail = 0
	found, ok, err := f.publisher.FindReview(context.Background(), twoFindings())
	if err != nil || !ok || found.ID != 100 || !slices.Equal(found.Inline, []int{1}) || !slices.Equal(found.CommentIDs, []int64{5000}) {
		t.Fatalf("found %+v, %v, %v", found, ok, err)
	}
	if len(f.github.reviews) != 1 || f.github.posts != 1 {
		t.Fatalf("reviews %d, posts %d", len(f.github.reviews), f.github.posts)
	}
}

func TestAReviewGitHubDidNotTakeIsPostedOnceOnTheRetryWithTheSameMarker(t *testing.T) {
	f := newPublisherFixture(t)
	f.github.fail, f.github.failBy = 502, `{"message":"Bad Gateway"}`

	_, err := f.publisher.PostReview(context.Background(), twoFindings())
	var gh *GitHubError
	if !errors.As(err, &gh) || gh.Status != 502 || len(f.github.reviews) != 0 {
		t.Fatalf("err = %v; reviews %d", err, len(f.github.reviews))
	}

	f.github.fail = 0
	if _, ok, err := f.publisher.FindReview(context.Background(), twoFindings()); ok || err != nil {
		t.Fatalf("found a review GitHub never took: %v, %v", ok, err)
	}
	posted, err := f.publisher.PostReview(context.Background(), twoFindings())
	if err != nil || posted.Existed || !slices.Equal(posted.CommentIDs, []int64{5000}) {
		t.Fatalf("posted %+v, %v", posted, err)
	}
	if len(f.github.reviews) != 1 || !strings.Contains(f.github.reviews[0]["body"].(string), "<!-- legato-review-pass t1/pass-1/pr-12 -->") {
		t.Fatalf("reviews = %+v", f.github.reviews)
	}
}

// The pass's review, a later APPROVE and a later held COMMENT each have a
// marker of their own: one on GitHub is never taken for another.
func TestEachKindOfReviewHasAMarkerOfItsOwn(t *testing.T) {
	f := newPublisherFixture(t)
	approve := twoFindings()
	approve.Event, approve.Findings, approve.Marker = ReviewApprove, nil, "t1/pass-1/pr-12/checks"
	if _, err := f.publisher.PostReview(context.Background(), approve); err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"t1/pass-1/pr-12", "t1/pass-1/pr-12/held"} {
		post := twoFindings()
		post.Marker = marker
		if _, ok, err := f.publisher.FindReview(context.Background(), post); ok || err != nil {
			t.Fatalf("%s: the later APPROVE was taken for it (%v)", marker, err)
		}
	}
}
