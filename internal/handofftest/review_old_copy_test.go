package handofftest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// An old review-pr-passes copy, the file legato workflow add wrote before
// review-loop replaced it, still runs unchanged through v1 (spec LEG-559 AC
// 23; LEG-599): found in the operator's folder as discovery finds it, with
// the deprecation warning but loading, it reviews and approves with the
// inline step's own outputs (findings an integer, reviews the list).
func TestAnOldReviewPRPassesCopyStillRunsUnchanged(t *testing.T) {
	r := newReviewRun(t, ReviewPassOptions{})
	ctx := context.Background()
	dir := t.TempDir()
	copied := workflowengine.OldReviewPassesCopy().YAML
	path := filepath.Join(dir, "review-pr-passes.workflow.yaml")
	if err := os.WriteFile(path, copied, 0o600); err != nil {
		t.Fatal(err)
	}
	found := workflowengine.Discover(dir, "")
	if len(found.Errors) != 0 || len(found.Definitions) != 1 {
		t.Fatalf("discovery = %+v", found)
	}
	definition := found.Definitions[0]
	if definition.SchemaVersion != workflowengine.SchemaVersion || definition.Steps[0].Type != workflowengine.ReviewPassesStepType {
		t.Fatalf("the copy changed: %+v", definition)
	}
	if loaded, diagnostics := workflowengine.Check(path, copied); loaded == nil || len(diagnostics) != 1 || diagnostics[0].Severity != workflowengine.SeverityWarning || !strings.Contains(diagnostics[0].Message, "github.pr.review_passes is deprecated") {
		t.Fatalf("check = %+v", diagnostics)
	}

	view, err := r.runs.Workflows.Start(ctx, definition, pr12)
	if err != nil || view.Run.Status != "running" {
		t.Fatalf("start = %+v, %v", view, err)
	}
	r.runs.Open(ctx)
	r.tick()
	r.submit(t, view.Run.TaskID, service.ReviewSubmission{Summary: "Looks right.", Findings: []service.SubmittedFinding{
		{PR: 12, Severity: "nit", Title: "Name it", Body: "Call it name.", Place: &service.ReviewPlace{Path: "greet.go", Line: 2}},
	}})

	view = r.show(t, view.Run.ID)
	if view.Run.Status != "succeeded" || view.Steps[0].ExecutorType != workflowengine.ReviewPassesStepType {
		t.Fatalf("run %q (%s), steps %+v", view.Run.Status, view.Run.Error, view.Steps)
	}
	out := outputs(t, view, "review")
	if out["outcome"] != "approved" || out["passes"] != float64(1) || out["findings"] != float64(1) || out["approval_withheld"] != false {
		t.Fatalf("outputs = %+v", out)
	}
	if reviews, _ := out["reviews"].([]any); len(reviews) != 1 {
		t.Fatalf("reviews = %+v", out["reviews"])
	}
	if _, err := os.Stat(out["summary_path"].(string)); err != nil {
		t.Fatalf("summary: %v", err)
	}
	if note := outputs(t, view, "note"); note["skipped"] != true {
		t.Fatalf("note = %+v", note)
	}
	if posted := r.worker.ReviewGitHub.Posted(); len(posted) != 1 || posted[0].Event != service.ReviewApprove {
		t.Fatalf("posted = %+v", posted)
	}
}
