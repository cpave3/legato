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

func TestAFileNamedReviewLoopIsAnErrorAtItsName(t *testing.T) {
	data := []byte("schema_version: 2\nname: review-loop\ntitle: My own\nsteps:\n  - {id: note, title: Note, type: task.note, with: {task: x, body: y}}\n")
	_, diagnostics := workflow.Check("review-loop.workflow.yaml", data)
	if len(diagnostics) == 0 || diagnostics[0].Severity != workflow.SeverityError || diagnostics[0].Line != 2 ||
		!strings.Contains(diagnostics[0].Message, "review-loop is Legato's built-in") {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if _, err := workflow.Parse("review-loop.workflow.yaml", data); err == nil {
		t.Fatal("a file named review-loop loaded")
	}
}

func TestTheBuiltInReviewLoopIsAV2WorkflowOfTheReviewSteps(t *testing.T) {
	d := workflow.BuiltInReviewLoop()
	if d.Name != workflow.ReviewLoopWorkflowName || d.SchemaVersion != workflow.SchemaVersion2 || !strings.HasPrefix(d.Source, "builtin:") {
		t.Fatalf("definition = %s v%d from %s", d.Name, d.SchemaVersion, d.Source)
	}
	var types []string
	for _, step := range d.Steps {
		types = append(types, step.Type)
		for _, inner := range step.Steps {
			types = append(types, inner.Type)
		}
	}
	if got := strings.Join(types, ","); got != "github.pr.review.open,,github.pr.review.pass,github.pr.review.confirm,github.pr.review.wait,github.pr.ready,,github.pr.review.wait,github.pr.review.pass,github.pr.review.confirm,github.pr.ready,github.pr.review.close" {
		t.Fatalf("steps = %s", got)
	}
	for _, name := range []string{"outcome", "loop_id", "passes", "clean_pass", "confirmed_at", "findings", "prs", "summary_path", "linked_task", "ready"} {
		if _, ok := d.Outputs[name]; !ok {
			t.Fatalf("no output %q", name)
		}
	}
	if d.Outputs["findings"].Type != "json" {
		t.Fatalf("findings is %s", d.Outputs["findings"].Type)
	}
	for _, name := range []string{"repo", "pr_number", "prs", "scope", "agent_author", "reviewer", "skill", "max_passes", "max_reentries", "ready", "steward"} {
		if _, ok := d.Inputs[name]; !ok {
			t.Fatalf("no input %q", name)
		}
	}
}

func TestAWrittenReviewLoopNeverReplacesTheBuiltIn(t *testing.T) {
	mine := &workflow.Definition{Name: workflow.ReviewLoopWorkflowName, Title: "Mine", Source: "/home/me/review-loop.workflow.yaml"}
	result := workflow.WithBuiltIns(workflow.DiscoveryResult{Definitions: []*workflow.Definition{mine}}, workflow.BuiltInReviewLoop())
	if len(result.Definitions) != 1 || !strings.HasPrefix(result.Definitions[0].Source, "builtin:") {
		t.Fatalf("definitions = %+v", result.Definitions)
	}
}

// The steward waits in mode watch (spec LEG-559 C1): a file may write it,
// and no other mode.
func TestAWaitStepTakesModeWatchAndNoOther(t *testing.T) {
	wait := func(mode string) []byte {
		return []byte(strings.Replace(string(reviewLoopYAML("pr_number: 12")), "  - id: close\n", `  - id: watch
    title: Watch
    type: github.pr.review.wait
    with:
      loop: ${{ steps.open.outputs.loop_id }}
      mode: `+mode+`
  - id: close
`, 1))
	}
	if errs := checkErrors(t, wait("watch")); len(errs) != 0 {
		t.Fatalf("mode watch: %+v", errs)
	}
	if errs := checkErrors(t, wait("nap")); len(errs) == 0 {
		t.Fatal("mode nap loaded")
	}
}

// prs is a list of 1 to 20 positive pull request numbers, each a number or
// an expression, or one expression for the whole list.
func TestAReviewLoopOpenStepsPRsMustBeAListOfNumbers(t *testing.T) {
	twenty := "prs: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20]"
	for _, tc := range []struct {
		name, with string
		ok         bool
	}{
		{"an empty list", "prs: []", false},
		{"a zero", "prs: [12, 0]", false},
		{"a negative number", "prs: [-3]", false},
		{"a word", "prs: [12, twelve]", false},
		{"a single number", "prs: 12", false},
		{"a map", "prs: {a: 1}", false},
		{"twenty numbers", twenty, true},
		{"an expression among numbers", "prs: [12, '${{ workflow.inputs.pr }}']", true},
		{"one expression for the list", "prs: '${{ workflow.inputs.prs }}'", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := checkErrors(t, reviewLoopYAML(tc.with))
			named := false
			for _, e := range errs {
				named = named || strings.Contains(e.Message, "with prs must be a list of at most 20 pull request numbers")
			}
			if tc.ok && named {
				t.Fatalf("errors = %v, want no prs refusal", errs)
			}
			if !tc.ok && !named {
				t.Fatalf("errors = %v, want the prs refusal", errs)
			}
		})
	}
}
