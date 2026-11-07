package handofftest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

var (
	ownersReview = service.PRComment{Kind: "review", ID: 1, Author: "bob", Association: "OWNER", Body: "Please split the retry out.", State: "COMMENTED", URL: "r1"}
	botComment   = service.PRComment{Kind: "inline", ID: 501, ThreadRoot: 501, Author: "cubic-dev-ai[bot]", Bot: true, Association: "NONE", Body: "Nil check missing.", Path: "login.go", Line: 4, URL: "c501"}
	strangers    = service.PRComment{Kind: "issue", ID: 2000, Author: "stranger", Association: "NONE", Body: "+1", URL: "i2000"}
	legatos      = service.PRComment{Kind: "review", ID: 3, Author: "legato-reviewer[bot]", Bot: true, Association: "NONE", Body: "Fine.\n\n<!-- legato-review-pass x/pass-1/pr-7 -->", State: "APPROVED"}
	authorsReply = service.PRComment{Kind: "inline", ID: 504, ThreadRoot: 501, Author: "legato-author[bot]", Bot: true, Association: "NONE", Body: "Fixed."}
)

func (r reviewed) commentIDs(t *testing.T) []string {
	t.Helper()
	view, err := service.ReviewFeedback(context.Background(), r.worker.Store, r.card)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, item := range view.Items {
		if item.Kind == service.ItemComment {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func TestCountedCommentsAreBatchedAndTheRestSkipped(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.worker.ReviewGitHub.SetComments(7, ownersReview, botComment, strangers, legatos, authorsReply)

	r.tick()

	if got := strings.Join(r.commentIDs(t), ","); got != "c-r-1,c-i-501" {
		t.Fatalf("counted comments %s; want the owner's review and the listed bot's", got)
	}
	if loop := r.loop(t); loop.SkippedComments != 1 {
		t.Fatalf("skipped %d; want the stranger's", loop.SkippedComments)
	}
	// Nothing is told until quiet_time passes with nothing new.
	if strings.Contains(r.worker.typedTo("legato-"+r.card), "new comment") {
		t.Fatal("comments told before the quiet time")
	}
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	typed := r.worker.typedTo("legato-" + r.card)
	if !strings.Contains(typed, "[legato review pass] 2 new comments on your pull requests. Run legato review-pass feedback --json") ||
		strings.Contains(typed, "split the retry") {
		t.Fatalf("typed to the author: %q", typed)
	}
	// Told once, whatever restarts in between.
	r.worker.Restart()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	if n := strings.Count(r.worker.typedTo("legato-"+r.card), "new comments"); n != 1 {
		t.Fatalf("told %d times", n)
	}
	// Done is refused until they are answered.
	if err := r.worker.Board.MoveCard(context.Background(), r.card, "Done"); err == nil {
		t.Fatal("Done with comments unanswered")
	}
}

func TestEditsResolvesOutdatedAndDeletedCommentsAreFollowed(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	outdated := service.PRComment{Kind: "inline", ID: 502, ThreadRoot: 502, Author: "amy", Association: "MEMBER", Body: "Old line.", Path: "login.go", Outdated: true}
	r.worker.ReviewGitHub.SetComments(7, ownersReview, botComment, outdated)
	r.tick()
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	r.answer(t, "c-r-1", service.AnswerReplied, "Split in a follow-up.")

	edited := ownersReview
	edited.Body = "Please split the retry out, and name it."
	resolved := botComment
	resolved.Resolved = true
	r.worker.ReviewGitHub.SetComments(7, edited, resolved) // the outdated one is deleted
	r.worker.Clock.Advance(2 * time.Minute)
	r.tick()

	items, _ := r.worker.Store.ListHandoffReviewItems(context.Background(), r.card)
	byID := map[string]int{}
	for i, item := range items {
		byID[item.ID] = i
	}
	review := items[byID["c-r-1"]]
	if review.Version != 2 || review.Answer != "" || review.Body != edited.Body {
		t.Fatalf("edited review = %+v; want version 2 to answer again", review)
	}
	if bot := items[byID["c-i-501"]]; bot.Answer != "resolved" {
		t.Fatalf("resolved thread = %+v", bot)
	}
	if old := items[byID["c-i-502"]]; !old.Closed || !old.Outdated {
		t.Fatalf("deleted outdated comment = %+v", old)
	}
	if typed := r.worker.typedTo("legato-" + r.card); !strings.Contains(typed, "c-i-502 was deleted on GitHub") {
		t.Fatalf("the author was not told of the deletion: %q", typed)
	}
}

func TestACommentAfterACleanPassReopensTheHandoffUntilAnswered(t *testing.T) {
	r := newReviewed(t, ReviewPassOptions{})
	r.finish(t)
	if _, err := r.submit(t, service.ReviewSubmission{Summary: "Good."}); err != nil {
		t.Fatal(err)
	}
	if r.handoffState(t) != "done" {
		t.Fatalf("after a clean pass: %s", r.handoffState(t))
	}

	r.worker.Clock.Advance(2 * time.Minute)
	r.worker.ReviewGitHub.SetComments(7, ownersReview)
	r.tick()
	if r.loop(t).Phase != service.ReviewPhaseWaitingForAuthor || r.column(t) != "Doing" || r.handoffState(t) != "in_progress" {
		t.Fatalf("after a new comment: phase %q column %q", r.loop(t).Phase, r.column(t))
	}

	// The author only replies: done again, with no new pass.
	r.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	r.answer(t, "c-r-1", service.AnswerReplied, "Done in a follow-up PR.")
	r.finish(t)
	if loop := r.loop(t); loop.Phase != service.ReviewPhaseClean || loop.Pass != 1 || r.handoffState(t) != "done" {
		t.Fatalf("after a reply: %+v", loop)
	}
	if replies := r.worker.ReviewGitHub.Comments(); len(replies) != 1 || !strings.HasPrefix(replies[0], "author: ") {
		t.Fatalf("the author's answer on GitHub: %v", replies)
	}
}
