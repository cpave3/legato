package store

import (
	"context"
	"testing"
)

func TestUpdatePRMeta(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	prMeta := `{"branch":"feature/auth","pr_number":42}`
	if err := s.UpdatePRMeta(ctx, "task1", &prMeta); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTask(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	if got.PRMeta == nil || *got.PRMeta != prMeta {
		t.Errorf("PRMeta = %v, want %q", got.PRMeta, prMeta)
	}
}

func TestUpdatePRMetaClear(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	// Set then clear
	prMeta := `{"branch":"feature/auth"}`
	if err := s.UpdatePRMeta(ctx, "task1", &prMeta); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePRMeta(ctx, "task1", nil); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetTask(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	if got.PRMeta != nil {
		t.Errorf("PRMeta = %v, want nil", got.PRMeta)
	}
}

func TestUpdatePRMetaNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	prMeta := `{"branch":"feature/auth"}`
	err := s.UpdatePRMeta(ctx, "nonexistent", &prMeta)
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListPRTrackedTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")
	createTestTask(t, s, "task2")
	createTestTask(t, s, "task3")

	// Link branches to task1 and task3
	pr1 := `{"branch":"feature/auth"}`
	pr3 := `{"branch":"feature/api"}`
	if err := s.UpdatePRMeta(ctx, "task1", &pr1); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePRMeta(ctx, "task3", &pr3); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListPRTrackedTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d tasks, want 2", len(got))
	}
	// Ordered by id
	if got[0].ID != "task1" || got[1].ID != "task3" {
		t.Errorf("ids = [%s, %s], want [task1, task3]", got[0].ID, got[1].ID)
	}
}

func TestListPRTrackedTasksEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")

	got, err := s.ListPRTrackedTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("got %d tasks, want 0", len(got))
	}
}

func TestListPRTrackedTasksSkipsArchived(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task1")
	createTestTask(t, s, "task2")

	pr1 := `{"branch":"feature/auth"}`
	pr2 := `{"branch":"feature/api"}`
	if err := s.UpdatePRMeta(ctx, "task1", &pr1); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdatePRMeta(ctx, "task2", &pr2); err != nil {
		t.Fatal(err)
	}

	// Archive task1
	if err := s.ArchiveTask(ctx, "task1"); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListPRTrackedTasks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d tasks, want 1", len(got))
	}
	if got[0].ID != "task2" {
		t.Errorf("id = %s, want task2", got[0].ID)
	}
}

func TestPRMetaSerializationRoundTrip(t *testing.T) {
	original := &PRMeta{
		Branch:         "feature/auth",
		PRNumber:       42,
		PRURL:          "https://github.com/owner/repo/pull/42",
		State:          "OPEN",
		IsDraft:        false,
		ReviewDecision: "APPROVED",
		CheckStatus:    "pass",
		CommentCount:   3,
		UpdatedAt:      "2026-03-20T10:00:00Z",
	}

	raw, err := MarshalPRMeta(original)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := ParsePRMeta(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Branch != original.Branch {
		t.Errorf("Branch = %q, want %q", parsed.Branch, original.Branch)
	}
	if parsed.PRNumber != original.PRNumber {
		t.Errorf("PRNumber = %d, want %d", parsed.PRNumber, original.PRNumber)
	}
	if parsed.ReviewDecision != original.ReviewDecision {
		t.Errorf("ReviewDecision = %q, want %q", parsed.ReviewDecision, original.ReviewDecision)
	}
	if parsed.CheckStatus != original.CheckStatus {
		t.Errorf("CheckStatus = %q, want %q", parsed.CheckStatus, original.CheckStatus)
	}
}

func TestParsePRMetaNil(t *testing.T) {
	got, err := ParsePRMeta(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestMarshalPRMetaNil(t *testing.T) {
	got, err := MarshalPRMeta(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}
