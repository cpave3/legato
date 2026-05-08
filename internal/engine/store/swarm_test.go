package store

import (
	"context"
	"reflect"
	"testing"
)

func TestMarshalScopeGlobsRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   []string
	}{
		{"empty", []string{}},
		{"nil", nil},
		{"single", []string{"api/**"}},
		{"multi", []string{"api/**", "go.mod"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := MarshalScopeGlobs(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ParseScopeGlobs(raw)
			if err != nil {
				t.Fatal(err)
			}
			want := tc.in
			if want == nil {
				want = []string{}
			}
			if got == nil {
				got = []string{}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("roundtrip = %v, want %v", got, want)
			}
		})
	}
}

func TestParseScopeGlobsInvalidJSON(t *testing.T) {
	if _, err := ParseScopeGlobs("not json"); err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

func TestSwarmSubtasksTableExists(t *testing.T) {
	s := newTestStore(t)
	var tables []string
	err := s.db.Select(&tables, "SELECT name FROM sqlite_master WHERE type='table' AND name='swarm_subtasks'")
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 {
		t.Fatal("swarm_subtasks table missing")
	}
}

func TestCreateAndGetSubtask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "parent-1")

	scope, _ := MarshalScopeGlobs([]string{"api/**", "go.mod"})
	st := Subtask{
		ID:           "sub-1",
		ParentTaskID: "parent-1",
		Title:        "API",
		Description:  "Build the API",
		ScopeGlobs:   scope,
		Role:         "builder",
		Status:       "queued",
	}
	if err := s.CreateSubtask(ctx, st); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetSubtask(ctx, "sub-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "API" {
		t.Errorf("Title = %q, want API", got.Title)
	}
	if got.Status != "queued" {
		t.Errorf("Status = %q, want queued", got.Status)
	}
	globs, err := ParseScopeGlobs(got.ScopeGlobs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(globs, []string{"api/**", "go.mod"}) {
		t.Errorf("globs = %v, want [api/** go.mod]", globs)
	}
}

func TestListSubtasksByParent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "parent-1")
	createTestTask(t, s, "parent-2")

	for _, st := range []Subtask{
		{ID: "a", ParentTaskID: "parent-1", Title: "A", Role: "builder", Status: "queued", ScopeGlobs: "[]"},
		{ID: "b", ParentTaskID: "parent-1", Title: "B", Role: "builder", Status: "queued", ScopeGlobs: "[]"},
		{ID: "c", ParentTaskID: "parent-2", Title: "C", Role: "builder", Status: "queued", ScopeGlobs: "[]"},
	} {
		if err := s.CreateSubtask(ctx, st); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.ListSubtasksByParent(ctx, "parent-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
}

func TestUpdateSubtaskStatusTransitions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "parent-1")

	st := Subtask{ID: "sub-1", ParentTaskID: "parent-1", Title: "x", Role: "builder", Status: "queued", ScopeGlobs: "[]"}
	if err := s.CreateSubtask(ctx, st); err != nil {
		t.Fatal(err)
	}

	if err := s.SetSubtaskDispatched(ctx, "sub-1"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSubtask(ctx, "sub-1")
	if got.Status != "dispatched" {
		t.Fatalf("status = %q, want dispatched", got.Status)
	}
	if got.DispatchedAt == nil {
		t.Error("dispatched_at should be set after SetSubtaskDispatched")
	}

	if err := s.UpdateSubtaskStatus(ctx, "sub-1", "in_progress"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSubtask(ctx, "sub-1")
	if got.Status != "in_progress" {
		t.Fatalf("status = %q, want in_progress", got.Status)
	}
	if got.StartedAt == nil {
		t.Error("started_at should be set on transition to in_progress")
	}

	if err := s.UpdateSubtaskStatus(ctx, "sub-1", "reporting"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSubtask(ctx, "sub-1")
	if got.Status != "reporting" {
		t.Fatalf("status = %q, want reporting", got.Status)
	}

	if err := s.UpdateSubtaskStatus(ctx, "sub-1", "done"); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSubtask(ctx, "sub-1")
	if got.Status != "done" {
		t.Fatalf("status = %q", got.Status)
	}
	if got.CompletedAt == nil {
		t.Error("completed_at should be set on transition to done")
	}
}

func TestUpdateSubtaskStatusCancelledSetsCompletedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "parent-1")
	st := Subtask{ID: "sub-1", ParentTaskID: "parent-1", Title: "x", Role: "builder", Status: "queued", ScopeGlobs: "[]"}
	if err := s.CreateSubtask(ctx, st); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateSubtaskStatus(ctx, "sub-1", "cancelled"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSubtask(ctx, "sub-1")
	if got.Status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", got.Status)
	}
	if got.CompletedAt == nil {
		t.Error("completed_at should be set on transition to cancelled")
	}
}

func TestUpdateSubtaskStatusNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.UpdateSubtaskStatus(ctx, "nope", "in_progress"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSetSubtaskDispatchedNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.SetSubtaskDispatched(ctx, "nope"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestCreateSubtaskPersistsAgentKindAndPrompt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "parent-1")
	st := Subtask{
		ID: "sub-1", ParentTaskID: "parent-1", Title: "API", Role: "backend",
		AgentKind: "claude-code", Prompt: "do the thing", ScopeGlobs: "[\"api/**\"]",
	}
	if err := s.CreateSubtask(ctx, st); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSubtask(ctx, "sub-1")
	if got.AgentKind != "claude-code" {
		t.Errorf("AgentKind = %q", got.AgentKind)
	}
	if got.Prompt != "do the thing" {
		t.Errorf("Prompt = %q", got.Prompt)
	}
	if got.Status != "queued" {
		t.Errorf("Status default = %q, want queued", got.Status)
	}
}

func TestSetTaskSwarmWorkingDir(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "parent-1")

	wd := "/home/me/projects/foo"
	if err := s.SetTaskSwarmWorkingDir(ctx, "parent-1", &wd); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTask(ctx, "parent-1")
	if got.SwarmWorkingDir == nil || *got.SwarmWorkingDir != wd {
		t.Errorf("SwarmWorkingDir = %v, want %s", got.SwarmWorkingDir, wd)
	}

	if err := s.SetTaskSwarmWorkingDir(ctx, "parent-1", nil); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetTask(ctx, "parent-1")
	if got.SwarmWorkingDir != nil {
		t.Errorf("SwarmWorkingDir = %v, want nil", *got.SwarmWorkingDir)
	}
}

func TestSetTaskSwarmWorkingDirNotFound(t *testing.T) {
	s := newTestStore(t)
	wd := "/x"
	if err := s.SetTaskSwarmWorkingDir(context.Background(), "nope", &wd); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSetSubtaskAgents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "parent-1")
	st := Subtask{ID: "sub-1", ParentTaskID: "parent-1", Title: "x", Role: "builder", Status: "queued", ScopeGlobs: "[]"}
	if err := s.CreateSubtask(ctx, st); err != nil {
		t.Fatal(err)
	}

	id := 42
	if err := s.SetSubtaskBuilderAgent(ctx, "sub-1", &id); err != nil {
		t.Fatal(err)
	}
	id2 := 99
	if err := s.SetSubtaskReviewerAgent(ctx, "sub-1", &id2); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSubtask(ctx, "sub-1")
	if got.BuilderAgentID == nil || *got.BuilderAgentID != 42 {
		t.Errorf("BuilderAgentID = %v, want 42", got.BuilderAgentID)
	}
	if got.ReviewerAgentID == nil || *got.ReviewerAgentID != 99 {
		t.Errorf("ReviewerAgentID = %v, want 99", got.ReviewerAgentID)
	}
}

func TestDeleteSubtask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "parent-1")
	st := Subtask{ID: "sub-1", ParentTaskID: "parent-1", Title: "x", Role: "builder", Status: "queued", ScopeGlobs: "[]"}
	if err := s.CreateSubtask(ctx, st); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtask(ctx, "sub-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSubtask(ctx, "sub-1"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAgentSessionRoleColumns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "parent-1")
	createTestTask(t, s, "child-1")

	parentID := "parent-1"
	subtaskID := "sub-1"
	id, err := s.InsertAgentSessionReturningID(ctx, AgentSession{
		TaskID:       "child-1",
		TmuxSession:  "legato-child-1",
		Command:      "shell",
		Status:       "running",
		Role:         "builder",
		ParentTaskID: &parentID,
		SubtaskID:    &subtaskID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == 0 {
		t.Error("expected non-zero agent id")
	}

	got, err := s.GetAgentSessionByTaskID(ctx, "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != "builder" {
		t.Errorf("Role = %q, want builder", got.Role)
	}
	if got.ParentTaskID == nil || *got.ParentTaskID != "parent-1" {
		t.Errorf("ParentTaskID = %v, want parent-1", got.ParentTaskID)
	}
	if got.SubtaskID == nil || *got.SubtaskID != "sub-1" {
		t.Errorf("SubtaskID = %v, want sub-1", got.SubtaskID)
	}
}
