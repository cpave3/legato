package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/mcpserver"
	"github.com/cpave3/legato/internal/service"
)

// MCPReviews takes a reviewer's result for its card's review pass
// (ReviewPassService).
type MCPReviews interface {
	Submit(ctx context.Context, taskID, executionID string, sub service.ReviewSubmission) (service.ReviewSubmitResult, error)
}

type reviewPlaceInput struct {
	Path      string `json:"path" jsonschema:"the file, relative to the repository's root"`
	Line      int    `json:"line" jsonschema:"the line; for a range, its last line"`
	StartLine int    `json:"start_line,omitempty" jsonschema:"the first line of a range"`
	Side      string `json:"side,omitempty" jsonschema:"RIGHT (the default) for a line of the pull request's head, LEFT for a deleted line"`
}

type reviewFindingInput struct {
	PR       int               `json:"pr" jsonschema:"the pull request's number"`
	Severity string            `json:"severity" jsonschema:"blocking (must be fixed before merging) or nit (optional)"`
	Title    string            `json:"title" jsonschema:"one line"`
	Body     string            `json:"body" jsonschema:"the finding, as Markdown"`
	Place    *reviewPlaceInput `json:"place,omitempty" jsonschema:"where it is, when it is about particular lines"`
}

type reviewRulingInput struct {
	Finding string `json:"finding" jsonschema:"the ID of an earlier blocking finding, as the brief lists it (f-…)"`
	Status  string `json:"status" jsonschema:"resolved (fixed, or you accept the author's answer) or open"`
	Reason  string `json:"reason,omitempty" jsonschema:"why; required when open"`
}

type submitReviewInput struct {
	Summary  string               `json:"summary,omitempty" jsonschema:"your overview of the pass, as Markdown"`
	Findings []reviewFindingInput `json:"findings,omitempty" jsonschema:"every problem found, each on one pull request"`
	Rulings  []reviewRulingInput  `json:"rulings,omitempty" jsonschema:"a ruling on every earlier blocking finding the brief lists"`
	Unable   string               `json:"unable,omitempty" jsonschema:"instead of all the rest: why you cannot review"`
}

func (m *mcpTools) submitReview() mcpserver.Tool {
	return mcpserver.NewTool(mcpserver.Definition{
		Name:  "submit_review",
		Title: "Submit the review",
		Description: "Hands your review pass to Legato, which posts it to GitHub; you never post yourself. " +
			"Give a summary and findings: each with the pull request's number, severity blocking or nit, a one-line title, a body, " +
			"and a place (path and line at the pinned head, optional start_line, side RIGHT or LEFT) when it is about particular lines. " +
			"On a later pass, give a ruling on every earlier blocking finding the brief lists: resolved, or open with a reason. " +
			"The verdict follows from them: any blocking finding or open ruling means changes, otherwise clean. " +
			"If you cannot review, give only unable and the reason. If Legato lists problems, fix them and call it again.",
	}, func(ctx context.Context, in submitReviewInput) (mcpserver.Result, error) {
		session, err := m.session(ctx)
		if err != nil {
			return mcpserver.Result{}, err
		}
		if session.Slot != service.SlotReviewer {
			return mcpserver.Result{}, fmt.Errorf("only a review pass's reviewer submits a review")
		}
		sub := service.ReviewSubmission{Summary: in.Summary, Unable: in.Unable}
		for _, f := range in.Findings {
			finding := service.SubmittedFinding{PR: f.PR, Severity: f.Severity, Title: f.Title, Body: f.Body}
			if f.Place != nil {
				finding.Place = &service.ReviewPlace{Path: f.Place.Path, Line: f.Place.Line, StartLine: f.Place.StartLine, Side: strings.ToUpper(f.Place.Side)}
			}
			sub.Findings = append(sub.Findings, finding)
		}
		for _, r := range in.Rulings {
			sub.Rulings = append(sub.Rulings, service.SubmittedRuling{Finding: r.Finding, Status: r.Status, Reason: r.Reason})
		}
		result, err := m.opts.Reviews.Submit(ctx, m.opts.TaskID, session.ExecutionID, sub)
		if err != nil {
			return mcpserver.Result{}, err
		}
		return mcpserver.Result{Text: result.Message, Data: result}, nil
	})
}

// reviewerSession says whether the server's session is a reviewer's: only
// it is offered submit_review.
func (m *mcpTools) reviewerSession() bool {
	if m.opts.Reviews == nil || m.opts.Store == nil {
		return false
	}
	session, err := m.session(context.Background())
	return err == nil && session.Slot == service.SlotReviewer
}
