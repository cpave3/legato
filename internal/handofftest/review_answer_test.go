package handofftest

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// --fixed names the commit the author pushed, not the head the reviewer's
// clone was reset to for the pass (S1, LEG-571's probe: before the fix, the
// reply read "Fixed in bbb222", the reviewed head).
func TestAFixedAnswerNamesTheAuthorsPushedTip(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	ctx := context.Background()
	_, finding := agentPassOne(t, r)

	if err := service.AnswerReviewItem(ctx, r.worker.Store, "board-1", finding, service.AnswerFixed, "Guarded.", r.worker.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	r.worker.ReviewGitHub.Push(12, "fff666")
	r.tick()
	replies := r.worker.ReviewGitHub.Replies()
	if len(replies) != 1 || !strings.Contains(replies[0], "Fixed in fff666: Guarded.") {
		t.Fatalf("replies = %v", replies)
	}
}
