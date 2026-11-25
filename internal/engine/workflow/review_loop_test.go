package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// reviewLoopFile is a v2 workflow that opens a review loop and closes it,
// with OPEN's with: lines in the open step.
const reviewLoopFile = `schema_version: 2
name: my-review
title: My review
steps:
  - id: open
    title: Open
    type: github.pr.review.open
    with:
      repo: acme/widget
OPEN
  - id: close
    title: Close
    type: github.pr.review.close
    with:
      loop: ${{ steps.open.outputs.loop_id }}
`

func reviewLoopYAML(open string) []byte {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(open), "\n") {
		lines = append(lines, "      "+line)
	}
	return []byte(strings.Replace(reviewLoopFile, "OPEN", strings.Join(lines, "\n"), 1))
}

func checkErrors(t *testing.T, data []byte) []workflow.Diagnostic {
	t.Helper()
	_, diagnostics := workflow.Check("my-review.workflow.yaml", data)
	var errs []workflow.Diagnostic
	for _, d := range diagnostics {
		if d.Severity == workflow.SeverityError {
			errs = append(errs, d)
		}
	}
	return errs
}

func TestAReviewLoopOpenedAndClosedByStepsLoads(t *testing.T) {
	data := reviewLoopYAML("pr_number: 12\nagent_author: true\nsteward: true\nready: true\nmax_reentries: 3")
	if errs := checkErrors(t, data); len(errs) > 0 {
		t.Fatalf("errors = %v", errs)
	}
	definition, err := workflow.Parse("my-review.workflow.yaml", data)
	if err != nil {
		t.Fatal(err)
	}
	if definition.Steps[0].Type != workflow.ReviewOpenStepType || definition.Steps[1].Type != workflow.ReviewCloseStepType {
		t.Fatalf("steps = %+v", definition.Steps)
	}
	spec, ok := workflow.LookupExecutor(workflow.ReviewOpenStepType)
	if !ok {
		t.Fatal("the open step is not in the catalog")
	}
	for _, name := range []string{"loop_id", "linked_task", "prs"} {
		if _, ok := spec.Output(name); !ok {
			t.Fatalf("the open step has no output %q", name)
		}
	}
	spec, _ = workflow.LookupExecutor(workflow.ReviewCloseStepType)
	for _, name := range []string{"outcome", "passes", "clean_pass", "confirmed_at", "findings", "prs", "summary_path"} {
		if _, ok := spec.Output(name); !ok {
			t.Fatalf("the close step has no output %q", name)
		}
	}
}

func TestAReviewLoopOpenStepThatAWorkflowFileMayNotWriteIsRefused(t *testing.T) {
	for _, tc := range []struct{ name, with, want string }{
		{"the handoff binding", "pr_number: 12\nhandoff: {id: h-1}", "handoff"},
		{"steward without agent_author", "pr_number: 12\nsteward: true", "steward needs agent_author"},
		{"steward with agent_author false", "pr_number: 12\nsteward: true\nagent_author: false", "steward needs agent_author"},
		{"both subjects", "pr_number: 12\nprs: [12, 13]", "pr_number"},
		{"no subject", "agent_author: true", "pr_number"},
		{"too many pull requests", "prs: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21]", "at most 20"},
		{"scope with prs", "prs: [12, 13]\nscope: pr", "scope"},
		{"an unknown scope", "pr_number: 12\nscope: everything", "scope"},
		{"negative max_passes", "pr_number: 12\nmax_passes: -1", "max_passes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := checkErrors(t, reviewLoopYAML(tc.with))
			if len(errs) == 0 || !strings.Contains(errs[0].Message, tc.want) {
				t.Fatalf("errors = %v, want one naming %q", errs, tc.want)
			}
			if errs[0].Line < 8 {
				t.Fatalf("error at line %d, want it in the open step", errs[0].Line)
			}
		})
	}
}

func TestARaisedMaxReentriesIsAnErrorAtItsLine(t *testing.T) {
	data := reviewLoopYAML("pr_number: 12\nagent_author: true\nsteward: true\nmax_reentries: 9")
	definition, diagnostics := workflow.Check("my-review.workflow.yaml", data)
	if definition == nil {
		t.Fatalf("the file itself loads: %v", diagnostics)
	}
	got := workflow.ReviewLoopDiagnostics(data, definition, 5)
	if len(got) != 1 || got[0].Severity != workflow.SeverityError || !strings.Contains(got[0].Message, "max_reentries") || got[0].Line != 13 {
		t.Fatalf("diagnostics = %+v", got)
	}
	for _, n := range []string{"5", "2", "0"} {
		data := reviewLoopYAML("pr_number: 12\nmax_reentries: " + n)
		definition, err := workflow.Parse("my-review.workflow.yaml", data)
		if err != nil {
			t.Fatal(err)
		}
		if got := workflow.ReviewLoopDiagnostics(data, definition, 5); len(got) != 0 {
			t.Fatalf("max_reentries %s is not raised: %+v", n, got)
		}
	}
}
