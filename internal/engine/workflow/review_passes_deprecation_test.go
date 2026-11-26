package workflow

import (
	"strings"
	"testing"
)

// The inline review step is deprecated (spec LEG-559 § Deprecation, M2, M3,
// A1; LEG-599): it still loads and runs, with a warning, and the upgrade
// (legato workflow upgrade and the editor's quick fix) rewrites it to a
// workflow.run of the built-in review-loop.

const deprecatedReviewPasses = "github.pr.review_passes is deprecated: call the built-in review-loop with workflow.run instead. `legato workflow upgrade <file>` makes the change."

func deprecation(t *testing.T, diagnostics []Diagnostic) Diagnostic {
	t.Helper()
	var found []Diagnostic
	for _, d := range diagnostics {
		if strings.Contains(d.Message, "github.pr.review_passes is deprecated") {
			found = append(found, d)
		}
	}
	if len(found) != 1 {
		t.Fatalf("deprecation warnings %+v in %+v", found, diagnostics)
	}
	return found[0]
}

func TestTheInlineReviewStepWarnsThatItIsDeprecatedWithTheUpgradeOnAVersion1File(t *testing.T) {
	old := OldReviewPassesCopy()
	definition, diagnostics := Check("review-pr-passes.workflow.yaml", old.YAML)
	if definition == nil {
		t.Fatalf("an old copy no longer loads: %+v", diagnostics)
	}
	d := deprecation(t, diagnostics)
	want := strings.TrimSuffix(deprecatedReviewPasses, ".") + " (it upgrades the file to v2)."
	if d.Message != want || d.Severity != SeverityWarning || d.Path != "steps[0].type" || d.Fix != FixUpgrade {
		t.Fatalf("diagnostic = %+v\nwant %q", d, want)
	}
	// "    type: github.pr.review_passes" is the old copy's line 38.
	if d.Line != 38 || d.Column != 11 {
		t.Fatalf("at %d:%d", d.Line, d.Column)
	}
}

func TestTheInlineReviewStepWarnsOnAVersion2FileWithoutTheVersionNote(t *testing.T) {
	file := `schema_version: 2
name: r
title: R
steps:
  - id: each
    title: Each
    loop: {over: [12, 13]}
    steps:
      - id: review
        title: Review
        type: github.pr.review_passes
        with: {repo: acme/widget, pr_number: "${{ loop.item }}"}
`
	definition, diagnostics := Check("r.workflow.yaml", []byte(file))
	if definition == nil {
		t.Fatalf("does not load: %+v", diagnostics)
	}
	d := deprecation(t, diagnostics)
	if d.Message != deprecatedReviewPasses || d.Path != "steps[0].steps[0].type" || d.Line != 11 || d.Fix != FixUpgrade {
		t.Fatalf("diagnostic = %+v", d)
	}
}

// The old review-pr-passes copy upgrades to a valid version 2 file that
// calls review-loop with the same inputs; the note step reads the outputs
// review-loop has under the same names.
func TestUpgradeRewritesTheOldCopyToCallReviewLoop(t *testing.T) {
	result, err := Upgrade("review-pr-passes.workflow.yaml", OldReviewPassesCopy().YAML)
	if err != nil {
		t.Fatal(err)
	}
	text := string(result.YAML)
	if len(result.Notes) != 0 || !result.Changed {
		t.Fatalf("notes %+v, changed %v\n%s", result.Notes, result.Changed, text)
	}
	want := `  - id: review
    title: Review in passes
    type: workflow.run
    with:
      workflow: review-loop
      inputs:
        repo: ${{ workflow.inputs.repo }}
        pr_number: ${{ workflow.inputs.pr_number }}
        max_passes: ${{ workflow.inputs.max_passes }}
        reviewer: ${{ workflow.inputs.reviewer }}
        agent_author: ${{ workflow.inputs.agent_author }}
        scope: ${{ workflow.inputs.scope }}
  - id: note
`
	if !strings.Contains(text, want) {
		t.Fatalf("upgraded file has no\n%s\nin\n%s", want, text)
	}
	if !strings.HasPrefix(text, "schema_version: 2\n") || !strings.Contains(text, "task: ${{ steps.review.outputs.linked_task }}") || !strings.Contains(text, "body_path: ${{ steps.review.outputs.summary_path }}") {
		t.Fatalf("upgraded file:\n%s", text)
	}
	definition, diagnostics := Check("review-pr-passes.workflow.yaml", result.YAML)
	if definition == nil || definition.SchemaVersion != SchemaVersion2 {
		t.Fatalf("the upgraded file does not load as v2: %+v", diagnostics)
	}
	for _, d := range diagnostics {
		t.Errorf("upgraded file: %s", d)
	}
	if problems := CallDiagnostics(result.YAML, definition, []*Definition{BuiltInReviewLoop()}); len(problems) != 0 {
		t.Fatalf("the call does not fit review-loop: %+v", problems)
	}
}

// findings, an integer on the inline step, is review-loop's findings.total;
// reviews has no equal, so each reference to it is an error at its place.
func TestUpgradeMapsFindingsToFindingsTotalAndFlagsReviews(t *testing.T) {
	in := `schema_version: 1
name: r
title: R
steps:
  - id: review
    title: Review
    type: github.pr.review_passes
    with:
      repo: acme/widget
      pr_number: 12
      # Only this pull request.
      scope: pr
      colour: blue
    outputs:
      findings: {type: integer}
  - id: note
    title: Note
    type: task.note
    with:
      task: ${{ steps.review.outputs.linked_task }}
      optional: true
      body: |
        ${{ steps.review.outputs.findings }} findings in ${{ steps.review.outputs.passes }} passes.
        Reviews: ${{ steps.review.outputs.reviews }}
`
	result, err := Upgrade("r.workflow.yaml", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	text := string(result.YAML)
	for _, want := range []string{
		"    type: workflow.run\n    with:\n      workflow: review-loop\n      inputs:\n        repo: acme/widget\n        pr_number: 12\n        # Only this pull request.\n        scope: pr\n  - id: note\n",
		"${{ steps.review.outputs.findings.total }} findings in ${{ steps.review.outputs.passes }} passes.",
		"Reviews: ${{ steps.review.outputs.reviews }}",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("upgraded file has no %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "colour") || strings.Contains(text, "outputs:\n      findings") {
		t.Errorf("kept what review-loop does not take:\n%s", text)
	}
	var reviews, colour *Diagnostic
	for i, note := range result.Notes {
		switch {
		case strings.Contains(note.Message, "steps.review.outputs.reviews"):
			reviews = &result.Notes[i]
		case strings.Contains(note.Message, "colour"):
			colour = &result.Notes[i]
		}
	}
	if reviews == nil || reviews.Severity != SeverityError || !strings.Contains(reviews.Message, "review-loop has no reviews") {
		t.Fatalf("reviews flagged as %+v in %+v", reviews, result.Notes)
	}
	// "        Reviews: ${{ steps.review.outputs.reviews }}" is line 23 of
	// the upgraded file; the reference starts at column 22.
	if reviews.Line != 23 || reviews.Column != 22 || reviews.EndColumn != 22+len("steps.review.outputs.reviews") {
		t.Fatalf("reviews flagged at %d:%d-%d\n%s", reviews.Line, reviews.Column, reviews.EndColumn, text)
	}
	if colour == nil || colour.Severity != SeverityWarning || colour.Line != 7 {
		t.Fatalf("the dropped input noted as %+v", colour)
	}
}

func TestUpgradeMapsFindingsInAnIfToo(t *testing.T) {
	in := `schema_version: 2
name: r
title: R
steps:
  - id: review
    title: Review
    type: github.pr.review_passes
    with: {repo: acme/widget, pr_number: 12}
  - id: note
    title: Note
    if: {field: steps.review.outputs.findings, equals: 0}
    type: task.note
    with:
      task: ${{ steps.review.outputs.linked_task }}
      optional: true
      body: Clean.
`
	result, err := Upgrade("r.workflow.yaml", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	text := string(result.YAML)
	if !strings.Contains(text, "    type: workflow.run\n    with:\n      workflow: review-loop\n      inputs: {repo: acme/widget, pr_number: 12}\n") ||
		!strings.Contains(text, "if: {field: steps.review.outputs.findings.total, equals: 0}") || len(result.Notes) != 0 {
		t.Fatalf("upgraded file:\n%s\nnotes %+v", text, result.Notes)
	}
}

// A version 2 file has nothing of version 1 to change, but its inline
// review steps are rewritten, a loop's body's included.
func TestUpgradeRewritesTheInlineStepInAVersion2File(t *testing.T) {
	in := `schema_version: 2
name: r
title: R
steps:
  - id: each
    title: Each
    loop: {over: [12, 13]}
    steps:
      - id: review
        title: Review
        type: github.pr.review_passes
        with:
          repo: acme/widget
          pr_number: ${{ loop.item }}
`
	result, err := Upgrade("r.workflow.yaml", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	text := string(result.YAML)
	want := "      - id: review\n        title: Review\n        type: workflow.run\n        with:\n          workflow: review-loop\n          inputs:\n            repo: acme/widget\n            pr_number: ${{ loop.item }}\n"
	if !result.Changed || !strings.HasPrefix(text, "schema_version: 2\n") || !strings.HasSuffix(text, want) || len(result.Notes) != 0 {
		t.Fatalf("upgraded file:\n%s\nnotes %+v", text, result.Notes)
	}
	if _, diagnostics := Check("r.workflow.yaml", result.YAML); len(diagnostics) != 0 {
		t.Fatalf("upgraded file: %+v", diagnostics)
	}
	again, err := Upgrade("r.workflow.yaml", result.YAML)
	if err != nil || again.Changed {
		t.Fatalf("upgraded twice: %v, %s", err, again.YAML)
	}
}

// review-pr-passes is no longer a template legato workflow add offers, but
// an old copy is still the file it wrote, so it runs unchanged (AC 23).
func TestReviewPRPassesIsWithdrawnFromTheTemplates(t *testing.T) {
	if _, ok := ExampleNamed(ReviewPassesWorkflowName); ok {
		t.Fatal("review-pr-passes is still a template")
	}
	old := OldReviewPassesCopy()
	definition, err := Parse("review-pr-passes.workflow.yaml", old.YAML)
	if err != nil || definition.Name != ReviewPassesWorkflowName || definition.SchemaVersion != SchemaVersion || definition.Steps[0].Type != "github.pr.review_passes" {
		t.Fatalf("old copy = %+v, %v", definition, err)
	}
}

// A reference inside an inline step's own with, to an earlier inline step's
// output, is mapped in the rewritten inputs: the with's rewrite and the
// reference's are one edit, and the file stays valid YAML.
func TestUpgradeMapsAReferenceInsideARewrittenWith(t *testing.T) {
	in := `schema_version: 2
name: r
title: R
steps:
  - id: a
    title: A
    type: github.pr.review_passes
    with:
      repo: acme/widget
      pr_number: 12
  - id: b
    title: B
    type: github.pr.review_passes
    with:
      repo: acme/widget
      pr_number: 13
      max_passes: ${{ steps.a.outputs.findings }}
`
	result, err := Upgrade("r.workflow.yaml", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	text := string(result.YAML)
	want := "    with:\n      workflow: review-loop\n      inputs:\n        repo: acme/widget\n        pr_number: 13\n        max_passes: ${{ steps.a.outputs.findings.total }}\n"
	if !strings.HasSuffix(text, want) || len(result.Notes) != 0 {
		t.Fatalf("upgraded file:\n%s\nnotes %+v", text, result.Notes)
	}
	if _, diagnostics := Check("r.workflow.yaml", result.YAML); len(diagnostics) != 0 {
		t.Fatalf("upgraded file: %+v", diagnostics)
	}
}

// A reference resolves to the nearest step of its ID in its own scope, as
// the engine reads it: a later step outside a loop that reuses the loop
// body's inline step's ID is another step, and its outputs are not mapped.
func TestUpgradeMapsOnlyTheReferencesThatReadTheRewrittenStep(t *testing.T) {
	in := `schema_version: 2
name: r
title: R
steps:
  - id: each
    title: Each
    loop: {over: [12, 13]}
    steps:
      - id: review
        title: Review
        type: github.pr.review_passes
        with:
          repo: acme/widget
          pr_number: ${{ loop.item }}
      - id: tell
        title: Tell
        type: task.note
        with:
          text: "${{ steps.review.outputs.findings }} findings"
  - id: review
    title: Count
    type: agent.run
    with:
      prompt: Count them.
    outputs:
      findings:
        type: integer
  - id: note
    title: Note
    type: task.note
    with:
      text: "${{ steps.review.outputs.findings }} counted"
`
	result, err := Upgrade("r.workflow.yaml", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	text := string(result.YAML)
	if !strings.Contains(text, `text: "${{ steps.review.outputs.findings.total }} findings"`) ||
		!strings.Contains(text, `text: "${{ steps.review.outputs.findings }} counted"`) || len(result.Notes) != 0 {
		t.Fatalf("upgraded file:\n%s\nnotes %+v", text, result.Notes)
	}
}

// The workflow's own outputs, written after its steps, read the steps'
// last scope: a read of the inline step's findings there is mapped too.
func TestUpgradeMapsTheWorkflowsOutputsAfterItsSteps(t *testing.T) {
	in := `schema_version: 2
name: r
title: R
steps:
  - id: review
    title: Review
    type: github.pr.review_passes
    with:
      repo: acme/widget
      pr_number: 12
outputs:
  count:
    type: integer
    value: ${{ steps.review.outputs.findings }}
`
	result, err := Upgrade("r.workflow.yaml", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(result.YAML); !strings.Contains(text, "value: ${{ steps.review.outputs.findings.total }}") {
		t.Fatalf("upgraded file:\n%s\nnotes %+v", text, result.Notes)
	}
}
