package workflow_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func TestDiscoverPrefersProjectDefinitionsAndReportsInvalidFiles(t *testing.T) {
	global := t.TempDir()
	project := t.TempDir()
	writeWorkflow(t, global, "review.yaml", "global title")
	writeWorkflow(t, project, "review.yaml", "project title")
	if err := os.WriteFile(filepath.Join(global, "broken.yaml"), []byte("schema_version: nope"), 0o600); err != nil {
		t.Fatal(err)
	}

	result := workflow.Discover(global, project)
	if len(result.Definitions) != 1 {
		t.Fatalf("definitions = %d, want 1", len(result.Definitions))
	}
	if got := result.Definitions[0].Title; got != "project title" {
		t.Fatalf("title = %q, want project override", got)
	}
	if len(result.Errors) != 1 || result.Errors[0].Path != filepath.Join(global, "broken.yaml") {
		t.Fatalf("errors = %+v", result.Errors)
	}
}

func TestParseRejectsForwardOutputReference(t *testing.T) {
	_, err := workflow.Parse("bad.yaml", []byte(`
schema_version: 1
name: bad
title: Bad
steps:
  - id: first
    title: First
    type: agent.run
    with:
      prompt: ${{ steps.second.outputs.value }}
  - id: second
    title: Second
    type: agent.run
    with: {prompt: hello}
    outputs:
      value: {type: string}
`))
	if err == nil {
		t.Fatal("Parse succeeded with a forward reference")
	}
}

func writeWorkflow(t *testing.T, dir, name, title string) {
	t.Helper()
	data := "schema_version: 1\nname: review\ntitle: " + title + "\nsteps:\n  - id: run\n    title: Run\n    type: agent.run\n    with: {prompt: review}\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
