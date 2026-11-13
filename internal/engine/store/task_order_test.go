package store

import (
	"context"
	"errors"
	"testing"
)

func seedColumnOrder(t *testing.T, s *Store, column string, ids ...string) {
	t.Helper()
	for i, id := range ids {
		if err := s.CreateTask(context.Background(), Task{ID: id, Title: id, Status: column, SortOrder: i, UpdatedAt: "2026-01-01T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
	}
}

func columnOrder(t *testing.T, s *Store, column string) []string {
	t.Helper()
	tasks, err := s.ListTasksByStatus(context.Background(), column)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(tasks))
	for i, task := range tasks {
		ids[i] = task.ID
	}
	return ids
}

func assertOrder(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestPlaceTaskPutsItBeforeItsNewNeighbour(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedColumnOrder(t, s, "Doing", "A", "B", "C", "D")
	seedColumnOrder(t, s, "Backlog", "X", "Y")

	if err := s.PlaceTask(ctx, "D", "B"); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, columnOrder(t, s, "Doing"), "A", "D", "B", "C")

	if err := s.PlaceTask(ctx, "A", "C"); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, columnOrder(t, s, "Doing"), "D", "B", "A", "C")
	assertOrder(t, columnOrder(t, s, "Backlog"), "X", "Y")
}

func TestPlaceTaskWithNoNeighbourPutsItLast(t *testing.T) {
	s := newTestStore(t)
	seedColumnOrder(t, s, "Doing", "A", "B", "C")

	if err := s.PlaceTask(context.Background(), "A", ""); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, columnOrder(t, s, "Doing"), "B", "C", "A")
}

func TestPlaceTaskBeforeItselfChangesNothing(t *testing.T) {
	s := newTestStore(t)
	seedColumnOrder(t, s, "Doing", "A", "B", "C")

	if err := s.PlaceTask(context.Background(), "B", "B"); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, columnOrder(t, s, "Doing"), "A", "B", "C")
}

// The order is the board's own: placing a card does not count as a change to
// the task, which sync compares with the provider's copy.
func TestPlaceTaskLeavesUpdatedAtAlone(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedColumnOrder(t, s, "Doing", "A", "B")

	if err := s.PlaceTask(ctx, "B", "A"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"A", "B"} {
		task, err := s.GetTask(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if task.UpdatedAt != "2026-01-01T00:00:00Z" {
			t.Fatalf("%s updated_at = %q, want it unchanged", id, task.UpdatedAt)
		}
	}
}

func TestPlaceTaskRefusesANeighbourInAnotherColumn(t *testing.T) {
	s := newTestStore(t)
	seedColumnOrder(t, s, "Doing", "A", "B")
	seedColumnOrder(t, s, "Backlog", "X")

	err := s.PlaceTask(context.Background(), "B", "X")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	assertOrder(t, columnOrder(t, s, "Doing"), "A", "B")
}

func TestPlaceTaskRefusesAnArchivedNeighbour(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedColumnOrder(t, s, "Done", "A", "B", "C")
	if err := s.ArchiveTask(ctx, "B"); err != nil {
		t.Fatal(err)
	}

	if err := s.PlaceTask(ctx, "C", "B"); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

func TestPlaceTaskOfAnUnknownTaskIsNotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.PlaceTask(context.Background(), "NOPE", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// Imported tickets all start at sort_order 0. Ties are listed in the order
// the tasks were added, the same way by every listing, so a card dropped
// among them lands where the person saw it.
func TestTiedCardsListInTheOrderTheyWereAdded(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"Z", "M", "A"} {
		if err := s.CreateTask(ctx, Task{ID: id, Title: id, Status: "Backlog"}); err != nil {
			t.Fatal(err)
		}
	}
	assertOrder(t, columnOrder(t, s, "Backlog"), "Z", "M", "A")
	for _, view := range []WorkspaceView{{Kind: ViewAll}, {Kind: ViewUnassigned}} {
		tasks, err := s.ListTasksByStatusAndWorkspace(ctx, "Backlog", view)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{tasks[0].ID, tasks[1].ID, tasks[2].ID}
		assertOrder(t, got, "Z", "M", "A")
	}

	if err := s.PlaceTask(ctx, "A", "M"); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, columnOrder(t, s, "Backlog"), "Z", "A", "M")
}

func TestPlaceTaskRefusesATaskOffTheBoard(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedColumnOrder(t, s, "Done", "A", "B")
	if err := s.ArchiveTask(ctx, "A"); err != nil {
		t.Fatal(err)
	}

	if err := s.PlaceTask(ctx, "A", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	assertOrder(t, columnOrder(t, s, "Done"), "B")
}
