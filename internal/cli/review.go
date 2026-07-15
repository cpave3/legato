package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/service"
)

// ReviewAnnotate records an agent annotation and notifies running instances.
// Returns the annotated/created step ID.
func ReviewAnnotate(svc *service.ReviewService, taskID string, args service.AnnotateArgs) (string, error) {
	stepID, err := svc.Annotate(context.Background(), taskID, args)
	if err != nil {
		return "", err
	}
	broadcastReviewChanged(taskID, stepID, "annotated")
	return stepID, nil
}

// ReviewAnswer records an agent's reply to a reviewer question.
func ReviewAnswer(svc *service.ReviewService, taskID, stepPrefix, text string) error {
	if err := svc.Answer(context.Background(), taskID, stepPrefix, text); err != nil {
		return err
	}
	broadcastReviewChanged(taskID, stepPrefix, "answer")
	return nil
}

// ReviewReady marks the tour ready for human review.
func ReviewReady(svc *service.ReviewService, taskID, summary string) error {
	if err := svc.Ready(context.Background(), taskID, summary); err != nil {
		return err
	}
	broadcastReviewChanged(taskID, "", "ready")
	return nil
}

// ReviewSync imports worktree commits into the tour.
func ReviewSync(svc *service.ReviewService, taskID string) error {
	if err := svc.Sync(context.Background(), taskID); err != nil {
		return err
	}
	broadcastReviewChanged(taskID, "", "synced")
	return nil
}

// ReviewShow writes the tour to w — human-readable by default, JSON with
// asJSON.
func ReviewShow(svc *service.ReviewService, taskID string, asJSON bool, w io.Writer) error {
	view, err := svc.Tour(context.Background(), taskID)
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(view)
	}

	fmt.Fprintf(w, "Review tour for %s — %s\n", view.Tour.TaskID, view.Tour.Status)
	if view.Tour.Summary != "" {
		fmt.Fprintf(w, "Summary: %s\n", view.Tour.Summary)
	}
	fmt.Fprintln(w)
	for i, s := range view.Steps {
		mark := "[ ]"
		if s.ReviewedAt != nil {
			mark = "[x]"
		}
		flags := ""
		if s.Risk != "" {
			flags += " !" + s.Risk
		}
		if s.OrphanedAt != nil {
			flags += " (orphaned)"
		}
		fmt.Fprintf(w, "%s %d. %s — %s%s\n", mark, i+1, s.ID, s.Title, flags)
		if s.Narration != "" {
			fmt.Fprintf(w, "      %s\n", strings.ReplaceAll(s.Narration, "\n", "\n      "))
		}
	}
	return nil
}

func broadcastReviewChanged(taskID, stepID, kind string) {
	ipc.Broadcast(ipc.Message{Type: "review_changed", TaskID: taskID, Status: stepID, Content: kind})
}
