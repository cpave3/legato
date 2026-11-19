package workflow_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func TestAddingATemplateWritesItUnderTheNameGivenAndNeverOverwrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "workflows")
	taken := func(name string) bool { return name == "review-pr" }
	if _, err := workflow.AddTemplate("review-pr", "", dir, taken); !errors.Is(err, workflow.ErrWorkflowNameTaken) {
		t.Fatalf("a taken name = %v", err)
	}
	path, err := workflow.AddTemplate("review-pr", "my-review", dir, taken)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "my-review.workflow.yaml") {
		t.Fatalf("path = %s", path)
	}
	data, _ := os.ReadFile(path)
	definition, err := workflow.Parse(path, data)
	if err != nil || definition.Name != "my-review" {
		t.Fatalf("written definition = %+v, %v", definition, err)
	}
	original, _ := workflow.ExampleNamed("review-pr")
	if strings.Replace(string(original.YAML), "name: review-pr\n", "name: my-review\n", 1) != string(data) {
		t.Fatal("only the name line may change")
	}
	if _, err := workflow.AddTemplate("review-pr", "my-review", dir, func(string) bool { return false }); !errors.Is(err, workflow.ErrWorkflowNameTaken) {
		t.Fatalf("an existing file = %v", err)
	}
	if _, err := workflow.AddTemplate("nope", "", dir, taken); err == nil {
		t.Fatal("added a template that does not exist")
	}
	if _, err := workflow.AddTemplate("triage", "../escape", dir, taken); err == nil {
		t.Fatal("took a name with a path in it")
	}
}
