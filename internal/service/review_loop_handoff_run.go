package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// A worker's make-a-change handoff with review passes runs the built-in
// review-loop (spec LEG-559 § Handoffs, D10, Q1; LEG-600). The handoff
// service starts it when it accepts the handoff, with the internal handoff
// binding: a loop record the run owns, hosted by the card, that names the
// handoff and the card's columns. Only this path writes the binding: the
// open step refuses a handoff input from a file, a launcher or the API
// (handoff_input_refused), and adopts the loop the binding opened. Its
// passes then run as a handoff's do (HandoffReviewCaller): the card in Done
// is the author's signal, a pass moves the card, Done waits for a clean
// review. Recall, Drop, Leave, unpair and an allowed Return cancel the run;
// it ends at approved and lets go of its pull requests. See
// docs/ai/integrations.md § Review passes › Handoffs run review-loop.

// HandoffReviewLoopStart is a handoff's review-loop run, as the handoff
// service starts it.
type HandoffReviewLoopStart struct {
	// Repo is the handoff's repository (owner/name).
	Repo string
	// Card is the handed-off card: the run is related to it.
	Card string
	// Ready marks the approved stack ready: the worker's draft rule for
	// the repository has ready_after_review (LEG-456). github.pr.ready then
	// flips each open draft layer bottom up as the author App; a layer
	// that will not flip pauses the run. Legato never merges.
	Ready bool
	// Steward is refused: a handoff's loop ends at approval
	// (handoff_input_refused).
	Steward bool
	// Bind writes the handoff binding (the loop record the run's open
	// step adopts) once the run is recorded and before its first step
	// starts. A run it fails for is cancelled.
	Bind func(ctx context.Context, runID string) error
}

// ReviewLoopOpenStepID is the built-in review-loop's open step: the step a
// handoff binding's loop record names.
const ReviewLoopOpenStepID = "open"

// StartHandoffReviewLoop starts a top-level review-loop run for a handoff
// (HandoffReviewLoopStart). A steward is refused with handoff_input_refused,
// and nothing is recorded.
func (s *WorkflowService) StartHandoffReviewLoop(ctx context.Context, req HandoffReviewLoopStart) (*store.WorkflowRunView, error) {
	if req.Steward {
		return nil, reviewRefused(ReviewRefusalHandoffInput, "a handed-off task's review loop ends at approval: its handoff binding takes no steward")
	}
	if req.Bind == nil {
		return nil, fmt.Errorf("a handoff's review loop needs its binding")
	}
	definition := workflowengine.BuiltInReviewLoop()
	inputs, err := definition.NormalizeInputs(map[string]string{"repo": req.Repo, "ready": strconv.FormatBool(req.Ready)})
	if err != nil {
		return nil, err
	}
	created, err := s.createRun(ctx, definition, inputs, store.WorkflowRun{RelatedTaskID: req.Card})
	if err != nil {
		return nil, err
	}
	if err := req.Bind(ctx, created.RunID); err != nil {
		if _, cancelErr := s.Cancel(ctx, created.RunID, "its handoff binding could not be written: "+err.Error()); cancelErr != nil {
			log.Printf("workflows: cancelling handoff review run %s: %v", created.RunID, cancelErr)
		}
		return nil, err
	}
	if err := s.advance(ctx, created.RunID, "", false); err != nil {
		if view, loadErr := s.store.GetWorkflowRun(ctx, created.RunID); loadErr == nil {
			return view, nil
		}
		return nil, err
	}
	return s.store.GetWorkflowRun(ctx, created.RunID)
}

// reviewHandoffColumns are a handoff binding's card columns: where a pass,
// the author's turn and a clean pass put the card.
type reviewHandoffColumns struct {
	Work   string `json:"work"`
	Review string `json:"review"`
	Done   string `json:"done"`
}

// handoffColumnsJSON is the binding's columns from this Legato's review-pass
// configuration.
func handoffColumnsJSON(cfg ReviewPassConfig) string {
	data, _ := json.Marshal(reviewHandoffColumns{Work: cfg.WorkColumn, Review: cfg.ReviewColumn, Done: cfg.DoneColumn})
	return string(data)
}

// handoffColumns are the card columns of a loop: its binding's, else this
// Legato's configuration (a loop the handoff itself owns).
func handoffColumns(raw string, cfg ReviewPassConfig) reviewHandoffColumns {
	out := reviewHandoffColumns{Work: cfg.WorkColumn, Review: cfg.ReviewColumn, Done: cfg.DoneColumn}
	var bound reviewHandoffColumns
	if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &bound) != nil {
		return out
	}
	if bound.Work != "" {
		out.Work = bound.Work
	}
	if bound.Review != "" {
		out.Review = bound.Review
	}
	if bound.Done != "" {
		out.Done = bound.Done
	}
	return out
}

// handoffBound says a loop record carries a handoff binding and belongs to
// a review-loop run: a handoff's loop the run's steps observe and the
// handoff caller moves on.
func handoffBound(l *store.ReviewLoop) bool {
	return l != nil && l.OwnerKind == store.ReviewLoopOwnerRun && l.HandoffID != ""
}
