package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestCollectWorkflowOutputsCopiesArtifactAndKeepsNativeValues(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedWorkflowOutputAttempt(t, db)
	workDir := t.TempDir()
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "review.md"), []byte("# Review\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	collector := NewWorkflowOutputCollector(db, dataDir)
	outputs, err := collector.Collect(context.Background(), WorkflowOutputRequest{
		RunID: "wr", StepID: "review", AttemptID: "wa", TaskID: "task", WorkingDir: workDir,
		Declared: map[string]workflowengine.Output{"review": {Type: "file", Path: "review.md", MediaType: "text/markdown"}, "count": {Type: "integer"}, "findings": {Type: "json"}},
		Values:   map[string]string{"count": "3", "findings": `[{"severity":"high"}]`}, Artifacts: map[string]string{"review": "review.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outputs["count"] != int64(3) {
		t.Fatalf("count = %#v", outputs["count"])
	}
	artifact := outputs["review"].(map[string]any)
	if _, err := os.Stat(artifact["path"].(string)); err != nil {
		t.Fatalf("managed artifact: %v", err)
	}
}

func TestCollectWorkflowOutputsInfersMarkdownMediaType(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedWorkflowOutputAttempt(t, db)
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "report.md"), []byte("# Report\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	outputs, err := NewWorkflowOutputCollector(db, t.TempDir()).Collect(context.Background(), WorkflowOutputRequest{
		RunID: "wr", StepID: "review", AttemptID: "wa", TaskID: "task", WorkingDir: workDir,
		Declared:  map[string]workflowengine.Output{"report": {Type: "file", Path: "report.md"}},
		Artifacts: map[string]string{"report": "report.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := outputs["report"].(map[string]any)["media_type"]; got != "text/markdown" {
		t.Fatalf("returned media type = %q, want text/markdown", got)
	}
	artifact, err := db.GetWorkflowArtifactByAttemptOutput(context.Background(), "wa", "report")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.MediaType != "text/markdown" {
		t.Fatalf("persisted media type = %q, want text/markdown", artifact.MediaType)
	}
}

func TestCollectWorkflowOutputsResolvesFileMediaTypes(t *testing.T) {
	tests := []struct {
		name, path, declared, want string
	}{
		{name: "long markdown extension", path: "report.markdown", want: "text/markdown"},
		{name: "explicit type overrides extension", path: "report.md", declared: "application/vnd.legato.report", want: "application/vnd.legato.report"},
		{name: "known text extension is normalized", path: "report.txt", want: "text/plain"},
		{name: "unknown extension stays binary", path: "report.legato-unknown", want: "application/octet-stream"},
		{name: "extensionless file stays binary", path: "report", want: "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			seedWorkflowOutputAttempt(t, db)
			workDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(workDir, tt.path), []byte("report"), 0o600); err != nil {
				t.Fatal(err)
			}
			outputs, err := NewWorkflowOutputCollector(db, t.TempDir()).Collect(context.Background(), WorkflowOutputRequest{
				RunID: "wr", StepID: "review", AttemptID: "wa", TaskID: "task", WorkingDir: workDir,
				Declared:  map[string]workflowengine.Output{"report": {Type: "file", Path: tt.path, MediaType: tt.declared}},
				Artifacts: map[string]string{"report": tt.path},
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := outputs["report"].(map[string]any)["media_type"]; got != tt.want {
				t.Fatalf("media type = %q, want %q", got, tt.want)
			}
		})
	}
}

func seedWorkflowOutputAttempt(t *testing.T, db *store.Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := db.CreateTask(ctx, store.Task{ID: "task", Title: "Workflow", Status: "Backlog", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	run := store.WorkflowRun{ID: "wr", WorkflowName: "test", WorkflowTitle: "Test", DefinitionSource: "test", DefinitionDigest: "x", DefinitionJSON: `{}`, InputsJSON: `{}`, Status: "running", CurrentStepID: "review", TaskID: "task"}
	steps := []store.WorkflowStepRun{{RunID: "wr", StepID: "review", Title: "Review", ExecutorType: "agent.run", DefinitionJSON: `{}`, Status: "pending"}}
	if err := db.CreateWorkflowRun(ctx, run, steps); err != nil {
		t.Fatal(err)
	}
	if err := db.StartWorkflowStepAttempt(ctx, store.WorkflowStepAttempt{ID: "wa", RunID: "wr", StepID: "review", AttemptNumber: 1, ExecutorType: "agent.run", TaskID: "task", Status: "running"}, `{}`); err != nil {
		t.Fatal(err)
	}
}

func TestCollectWorkflowOutputsReusesMatchingManagedArtifact(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	seedWorkflowOutputAttempt(t, db)
	workDir, dataDir := t.TempDir(), t.TempDir()
	source := filepath.Join(workDir, "review.md")
	_ = os.WriteFile(source, []byte("# Review\n"), 0600)
	collector := NewWorkflowOutputCollector(db, dataDir)
	req := WorkflowOutputRequest{RunID: "wr", StepID: "review", AttemptID: "wa", TaskID: "task", WorkingDir: workDir, Declared: map[string]workflowengine.Output{"review": {Type: "file", Path: "review.md"}}, Artifacts: map[string]string{"review": "review.md"}}
	first, err := collector.Collect(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := collector.Collect(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first["review"].(map[string]any)["artifact_id"] != second["review"].(map[string]any)["artifact_id"] {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
	if got := second["review"].(map[string]any)["media_type"]; got != "text/markdown" {
		t.Fatalf("replayed media type = %q, want text/markdown", got)
	}
}

func TestCollectWorkflowOutputsRejectsUndeclaredFilePath(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	seedWorkflowOutputAttempt(t, db)
	workDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(workDir, "other.md"), []byte("other"), 0o600)
	_, err := NewWorkflowOutputCollector(db, t.TempDir()).Collect(context.Background(), WorkflowOutputRequest{RunID: "wr", StepID: "review", AttemptID: "wa", WorkingDir: workDir, Declared: map[string]workflowengine.Output{"review": {Type: "file", Path: "review.md"}}, Artifacts: map[string]string{"review": "other.md"}})
	if err == nil {
		t.Fatal("collector accepted a path other than the declared output")
	}
}

func TestCollectWorkflowOutputsRejectsSymlinkEscape(t *testing.T) {
	db, _ := store.New(filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()
	workDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.md")
	_ = os.WriteFile(outside, []byte("secret"), 0o600)
	_ = os.Symlink(outside, filepath.Join(workDir, "review.md"))
	_, err := NewWorkflowOutputCollector(db, t.TempDir()).Collect(context.Background(), WorkflowOutputRequest{RunID: "wr", StepID: "s", AttemptID: "a", WorkingDir: workDir, Declared: map[string]workflowengine.Output{"review": {Type: "file", Path: "review.md"}}, Artifacts: map[string]string{"review": "review.md"}})
	if err == nil {
		t.Fatal("symlink escape accepted")
	}
}
