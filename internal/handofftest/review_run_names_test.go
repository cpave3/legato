package handofftest

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func TestARunLoopsFeedbackNamesTheCardAndTheRunTask(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	view, _ := agentPassOne(t, r)

	feedback, err := service.ReviewFeedback(context.Background(), r.worker.Store, "board-1")
	if err != nil {
		t.Fatal(err)
	}
	if feedback.Card == nil || feedback.Card.TaskID != "board-1" || feedback.Card.Title != "Greeting" {
		t.Fatalf("card = %+v", feedback.Card)
	}
	if feedback.Run == nil || feedback.Run.TaskID != view.Run.TaskID || feedback.Run.RunID != view.Run.ID {
		t.Fatalf("run = %+v, want task %s of %s", feedback.Run, view.Run.TaskID, view.Run.ID)
	}
	for _, want := range []string{view.Run.TaskID, view.Run.ID, "card board-1 (Greeting)", "end your turn"} {
		if !strings.Contains(feedback.How, want) {
			t.Fatalf("how does not say %q: %s", want, feedback.How)
		}
	}
	if strings.Contains(feedback.How, "Done") {
		t.Fatalf("a run's loop does not move on with a column, but how says: %s", feedback.How)
	}
}

func TestARunReviewersBriefGivesTheCardAsTheTask(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	withBoardAuthor(t, r)
	card, err := r.worker.Store.GetTask(context.Background(), "board-1")
	if err != nil {
		t.Fatal(err)
	}
	card.DescriptionMD = "Greet people by name."
	if err := r.worker.Store.UpdateTask(context.Background(), *card); err != nil {
		t.Fatal(err)
	}
	view := r.start(t, agentAuthored)
	r.tick()
	brief := r.lastBrief(t)
	task := brief[strings.Index(brief, "## The task"):]
	for _, want := range []string{"# Greeting", "Greet people by name.", "Card board-1", view.Run.ID} {
		if !strings.Contains(task, want) {
			t.Fatalf("the task section does not say %q:\n%s", want, task)
		}
	}
}
