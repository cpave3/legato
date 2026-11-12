package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func setGroup(t *testing.T, s *Store, taskID string, group *string) {
	t.Helper()
	ctx := context.Background()
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	task.Group = group
	if err := s.UpdateTask(ctx, *task); err != nil {
		t.Fatal(err)
	}
}

func groupOf(t *testing.T, s *Store, taskID string) *string {
	t.Helper()
	task, err := s.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	return task.Group
}

func TestListTaskGroupsNamesEachGroupInUseOnce(t *testing.T) {
	s := newTestStore(t)
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		createTestTask(t, s, id)
	}
	tooling, autopilot, empty := "Tooling", "Autopilot", ""
	setGroup(t, s, "a", &tooling)
	setGroup(t, s, "b", &autopilot)
	setGroup(t, s, "c", &tooling)
	setGroup(t, s, "d", &empty)
	if err := s.ArchiveTask(context.Background(), "c"); err != nil {
		t.Fatal(err)
	}
	archivedOnly := "Old"
	setGroup(t, s, "c", &archivedOnly)

	got, err := s.ListTaskGroups(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Autopilot", "Tooling"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("groups = %v, want %v", got, want)
	}
}

func TestRenameTaskGroupMovesEveryTaskAndNilClearsThem(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		createTestTask(t, s, id)
	}
	tooling, other := "Tooling", "Other"
	setGroup(t, s, "a", &tooling)
	setGroup(t, s, "b", &tooling)
	setGroup(t, s, "c", &other)

	renamed := "Infra"
	n, err := s.RenameTaskGroup(ctx, "Tooling", &renamed)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("renamed %d tasks, want 2", n)
	}
	for _, id := range []string{"a", "b"} {
		if g := groupOf(t, s, id); g == nil || *g != "Infra" {
			t.Fatalf("task %s group = %v, want Infra", id, g)
		}
	}
	if g := groupOf(t, s, "c"); g == nil || *g != "Other" {
		t.Fatalf("task c group = %v, want Other untouched", g)
	}

	if _, err := s.RenameTaskGroup(ctx, "Infra", nil); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if g := groupOf(t, s, id); g != nil {
			t.Fatalf("task %s group = %q after delete, want none", id, *g)
		}
	}
}

// SetTaskGroup writes the group alone, so it never puts back what another
// writer changed since the task was read.
func TestSetTaskGroupWritesOnlyTheGroup(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "G-1")
	tooling := "Tooling"
	if err := s.SetTaskGroup(ctx, "G-1", &tooling); err != nil {
		t.Fatal(err)
	}
	if g := groupOf(t, s, "G-1"); g == nil || *g != "Tooling" {
		t.Fatalf("group = %v", g)
	}
	if err := s.SetTaskGroup(ctx, "G-1", nil); err != nil {
		t.Fatal(err)
	}
	if g := groupOf(t, s, "G-1"); g != nil {
		t.Fatalf("group = %q after clearing", *g)
	}
	if err := s.SetTaskGroup(ctx, "NOPE", &tooling); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown task: %v", err)
	}
}
