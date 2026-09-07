package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestHandoffCommandDiscovery(t *testing.T) {
	for _, verb := range []string{"submit", "show", "export"} {
		out := captureStdout(t, func() int { return runCLI([]string{"review", "handoff", verb, "--help"}) })
		if !strings.Contains(out, "--task") {
			t.Fatalf("%s help: %s", verb, out)
		}
	}
}

func TestHandoffShowDoesNotCreateMissingTour(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	if err := s.CreateTask(context.Background(), store.Task{ID: "handoff-task", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	configureReviewCLITest(t, dbPath)
	t.Setenv("LEGATO_TASK_ID", "handoff-task")
	t.Setenv("LEGATO_REVIEW_NAME", "absent")
	_, code := captureStderrWithCode(t, func() int { return runCLI([]string{"review", "handoff", "show", "--json"}) })
	if code != exitNotFound {
		t.Fatalf("code=%d", code)
	}
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tours, err := s.ListReviewToursByTask(context.Background(), "handoff-task")
	if err != nil || len(tours) != 0 {
		t.Fatalf("created tour: %+v, %v", tours, err)
	}
}

func TestHandoffPublicCLIFileStdinSelectionAndConflict(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	repo := initReviewCLIRepo(t)
	if err := s.CreateTask(ctx, store.Task{ID: "handoff-task", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskWorktree(ctx, "handoff-task", &store.TaskWorktree{PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	tour, err := s.EnsureReviewTour(ctx, "handoff-task", "named")
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewReviewService(s, nil, nil)
	if err := svc.Ready(ctx, tour.ID, "Ready"); err != nil {
		t.Fatal(err)
	}
	view, err := svc.Tour(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := service.HandoffInput{SchemaVersion: 1, PassID: view.Passes[len(view.Passes)-1].Pass.ID, BaseSHA: view.Tour.BaseSHA, HeadSHA: view.Tour.HeadSHA,
		Overview: service.HandoffOverview{Before: "No chapter package", After: "Chapter package", Why: "A package", NonGoals: "Runtime behavior", Decision: "Small package", Alternative: "No package", Risk: "Untested runtime", VerificationGaps: "No tests reported", ReviewQuestion: "Is the declaration correct?", Rollback: "Revert addition"},
		Concepts: []service.HandoffConcept{{ID: "first", Title: "Selected concept", Claim: "Package declared", Flow: []string{"Load chapter.go"}, Invariant: "Valid Go", FailureCase: "Invalid syntax", ChangeGuide: "Start at declaration", SourceIDs: []string{"source"}, Routes: []string{"reviewer"}}, {ID: "second", Title: "Omitted concept", Claim: "Other context", Flow: []string{"Inspect source"}, Invariant: "Pinned revision", FailureCase: "Drift", ChangeGuide: "Check revision", SourceIDs: []string{"source"}}},
		Sources:  []service.HandoffSourceInput{{ID: "source", Path: "chapter.go", Side: "head", LineStart: 1, LineEnd: 1}}}
	s.Close()
	configureReviewCLITest(t, dbPath)
	t.Setenv("LEGATO_TASK_ID", "handoff-task")
	t.Setenv("LEGATO_REVIEW_NAME", "named")
	templateJSON := captureStdout(t, func() int { return runCLI([]string{"review", "handoff", "template"}) })
	template, err := service.DecodeHandoff(strings.NewReader(templateJSON))
	if err != nil || template.PassID != input.PassID || template.BaseSHA != input.BaseSHA || template.HeadSHA != input.HeadSHA || len(template.Concepts) == 0 || len(template.Sources) == 0 {
		t.Fatalf("template=%s err=%v", templateJSON, err)
	}
	data, _ := json.Marshal(input)
	packetPath := filepath.Join(t.TempDir(), "packet.json")
	if err := os.WriteFile(packetPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() int { return runCLI([]string{"review", "handoff", "submit", "--file", packetPath, "--json"}) })
	var envelope struct {
		Data service.HandoffPacket `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || envelope.Data.Revision != 1 {
		t.Fatalf("submit: %s %v", out, err)
	}
	packetID := envelope.Data.ID
	out = captureStdout(t, func() int { return runCLI([]string{"review", "handoff", "show", "--json"}) })
	if !strings.Contains(out, packetID) {
		t.Fatal(out)
	}
	out = captureStdout(t, func() int {
		return runCLI([]string{"review", "handoff", "export", "--packet", packetID, "--format", "maintenance", "--concept", "first", "--include-sources", "--json"})
	})
	var exported service.HandoffExport
	if err := json.Unmarshal([]byte(out), &exported); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exported.Markdown, "Selected concept") || strings.Contains(exported.Markdown, "Omitted concept") || !strings.Contains(exported.Markdown, "package chapter") {
		t.Fatal(out)
	}
	out = captureStdout(t, func() int {
		return runCLI([]string{"review", "handoff", "export", "--packet", packetID, "--format", "github", "--concept", "first", "--concept", "second"})
	})
	if !strings.Contains(out, "Omitted concept") {
		t.Fatal(out)
	}
	_, code := captureStderrWithCode(t, func() int { return runCLI([]string{"review", "handoff", "submit", "--file", packetPath, "--json"}) })
	if code != exitUsage {
		t.Fatalf("stale submit code=%d", code)
	}
	input.ExpectedRevision = 1
	data, _ = json.Marshal(input)
	withReviewStdin(t, string(data), func() {
		out = captureStdout(t, func() int { return runCLI([]string{"review", "handoff", "submit", "--file", "-"}) })
	})
	if !strings.Contains(out, "revision 2") {
		t.Fatal(out)
	}
}
