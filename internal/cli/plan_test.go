package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestPlanShowReturnsSubmittedMermaidFenceComment(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Mermaid plan", Status: "Doing", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}

	fence := "```mermaid\ngraph TD\n  A --> B\n```"
	markdown := "# Dïagram\n\n" + fence + "\n"
	bundle := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundle, "plan.md"), []byte(markdown), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "plan.json"), []byte(`{"schema_version":1,"title":"Mermaid plan"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := service.NewPlanService(s, nil, nil)
	view, err := svc.Submit(ctx, "task-1", "diagrams", bundle)
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index([]byte(markdown), []byte(fence))
	end := start + len([]byte(fence))
	if _, err := svc.AddComment(ctx, view.Plan.ID, service.PlanCommentInput{
		Body: "Clarify this flow", SelectionStart: &start, SelectionEnd: &end, SelectedText: fence,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestChanges(ctx, view.Plan.ID); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := PlanShow(svc, view.Plan.ID, &output); err != nil {
		t.Fatal(err)
	}
	var feedback service.PlanView
	if err := json.Unmarshal(output.Bytes(), &feedback); err != nil {
		t.Fatal(err)
	}
	if len(feedback.Comments) != 1 {
		t.Fatalf("comments = %+v", feedback.Comments)
	}
	comment := feedback.Comments[0]
	if comment.SubmittedAt == nil || comment.SelectionStart == nil || comment.SelectionEnd == nil {
		t.Fatalf("comment was not submitted with an anchor: %+v", comment)
	}
	if *comment.SelectionStart != start || *comment.SelectionEnd != end || comment.SelectedText != fence {
		t.Fatalf("anchor = %d:%d %q, want %d:%d %q", *comment.SelectionStart, *comment.SelectionEnd, comment.SelectedText, start, end, fence)
	}
}
