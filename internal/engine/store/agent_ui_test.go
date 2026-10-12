package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

var testUILimits = AgentUILimits{MaxItems: 200, MaxRevisions: 20}

func saveTestUIItem(t *testing.T, s *Store, item AgentUIItem, limits AgentUILimits) *AgentUIItem {
	t.Helper()
	saved, err := s.SaveAgentUIItem(context.Background(), item, limits)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestSaveAgentUIItemStoresContentAtRevisionOne(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	saved := saveTestUIItem(t, s, AgentUIItem{
		ID: "ui-0000000001", TaskID: "task-a", ExecutionID: "ex-1", Placement: "panel",
		Format: "html", Title: "Sync design", Content: "<h1>Design</h1>", FallbackText: "The design",
	}, testUILimits)
	if saved.Revision != 1 {
		t.Fatalf("revision = %d, want 1", saved.Revision)
	}

	got, err := s.GetAgentUIItem(ctx, "ui-0000000001", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != "task-a" || got.ExecutionID != "ex-1" || got.Placement != "panel" {
		t.Fatalf("identity = %#v", got)
	}
	if got.Format != "html" || got.Title != "Sync design" || got.Content != "<h1>Design</h1>" || got.FallbackText != "The design" {
		t.Fatalf("content = %#v", got)
	}
	if got.Revision != 1 || got.OldestRevision != 1 {
		t.Fatalf("revisions = %d..%d, want 1..1", got.OldestRevision, got.Revision)
	}
	if got.CreatedAt == "" || got.UpdatedAt == "" {
		t.Fatalf("timestamps missing: %#v", got)
	}
}

func TestGetAgentUIItemReportsMissingItemsAndRevisions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.GetAgentUIItem(ctx, "ui-missing", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing item error = %v, want ErrNotFound", err)
	}
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000001", TaskID: "task-a", Placement: "panel", Format: "html", Content: "x"}, testUILimits)
	if _, err := s.GetAgentUIItem(ctx, "ui-0000000001", 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing revision error = %v, want ErrNotFound", err)
	}
}

func TestSaveAgentUIItemWithSameKeyAddsARevisionToTheSameItem(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000001", TaskID: "task-a", Placement: "panel", Key: "design", Format: "html", Title: "v1", Content: "one"}, testUILimits)
	second := saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000002", TaskID: "task-a", Placement: "panel", Key: "design", Format: "markdown", Title: "v2", Content: "two"}, testUILimits)

	if second.ID != first.ID || second.Revision != 2 {
		t.Fatalf("second save = %s revision %d, want %s revision 2", second.ID, second.Revision, first.ID)
	}
	latest, err := s.GetAgentUIItem(ctx, first.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Content != "two" || latest.Title != "v2" || latest.Format != "markdown" || latest.Revision != 2 || latest.OldestRevision != 1 {
		t.Fatalf("latest = %#v", latest)
	}
	original, err := s.GetAgentUIItem(ctx, first.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if original.Content != "one" || original.Title != "v1" || original.Format != "html" || original.Revision != 1 {
		t.Fatalf("revision 1 = %#v", original)
	}
}

func TestAgentUIItemKeysAreScopedToTaskAndPlacement(t *testing.T) {
	s := newTestStore(t)

	panel := saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000001", TaskID: "task-a", Placement: "panel", Key: "weather", Format: "html", Content: "a"}, testUILimits)
	inline := saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000002", TaskID: "task-a", Placement: "inline", Key: "weather", Format: "html", Content: "b"}, testUILimits)
	other := saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000003", TaskID: "task-b", Placement: "panel", Key: "weather", Format: "html", Content: "c"}, testUILimits)
	unkeyedOne := saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000004", TaskID: "task-a", Placement: "panel", Format: "html", Content: "d"}, testUILimits)
	unkeyedTwo := saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000005", TaskID: "task-a", Placement: "panel", Format: "html", Content: "e"}, testUILimits)

	ids := map[string]bool{panel.ID: true, inline.ID: true, other.ID: true, unkeyedOne.ID: true, unkeyedTwo.ID: true}
	if len(ids) != 5 {
		t.Fatalf("expected five separate items, got %v", ids)
	}
}

func TestSaveAgentUIItemKeepsOnlyTheNewestRevisions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	limits := AgentUILimits{MaxItems: 200, MaxRevisions: 3}

	var id string
	for i := 1; i <= 5; i++ {
		saved := saveTestUIItem(t, s, AgentUIItem{ID: fmt.Sprintf("ui-%010d", i), TaskID: "task-a", Placement: "panel", Key: "design", Format: "html", Content: fmt.Sprintf("content %d", i)}, limits)
		id = saved.ID
	}

	latest, err := s.GetAgentUIItem(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Revision != 5 || latest.OldestRevision != 3 || latest.Content != "content 5" {
		t.Fatalf("latest = revision %d (oldest %d) %q, want 5 (oldest 3)", latest.Revision, latest.OldestRevision, latest.Content)
	}
	if _, err := s.GetAgentUIItem(ctx, id, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("trimmed revision error = %v, want ErrNotFound", err)
	}
	if kept, err := s.GetAgentUIItem(ctx, id, 3); err != nil || kept.Content != "content 3" {
		t.Fatalf("revision 3 = %#v, %v", kept, err)
	}
}

func TestListAgentUIItemsReturnsOneTaskInCreationOrderWithoutContent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000001", TaskID: "task-a", Placement: "panel", Key: "design", Format: "html", Title: "Design", Content: "one"}, testUILimits)
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000002", TaskID: "task-a", Placement: "inline", Format: "components", Title: "Weather", Content: "[]", FallbackText: "27 C"}, testUILimits)
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000003", TaskID: "task-b", Placement: "panel", Format: "html", Content: "other"}, testUILimits)
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000004", TaskID: "task-a", Placement: "panel", Key: "design", Format: "markdown", Title: "Design v2", Content: "two"}, testUILimits)

	items, err := s.ListAgentUIItems(ctx, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2: %#v", len(items), items)
	}
	if items[0].ID != "ui-0000000001" || items[0].Title != "Design v2" || items[0].Format != "markdown" || items[0].Revision != 2 || items[0].Key != "design" {
		t.Fatalf("first item = %#v", items[0])
	}
	if items[1].ID != "ui-0000000002" || items[1].Placement != "inline" || items[1].FallbackText != "27 C" || items[1].Key != "" {
		t.Fatalf("second item = %#v", items[1])
	}
	for _, item := range items {
		if item.Content != "" {
			t.Fatalf("list must not carry content: %#v", item)
		}
	}
}

func TestSaveAgentUIItemEvictsTheOldestItemsOfTheTaskBeyondTheLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	limits := AgentUILimits{MaxItems: 2, MaxRevisions: 20}

	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000001", TaskID: "task-a", Placement: "inline", Format: "html", Content: "one"}, limits)
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000002", TaskID: "task-a", Placement: "inline", Format: "html", Content: "two"}, limits)
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000009", TaskID: "task-b", Placement: "inline", Format: "html", Content: "other task"}, limits)
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000003", TaskID: "task-a", Placement: "inline", Format: "html", Content: "three"}, limits)

	items, err := s.ListAgentUIItems(ctx, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "ui-0000000002" || items[1].ID != "ui-0000000003" {
		t.Fatalf("items after eviction = %#v", items)
	}
	if _, err := s.GetAgentUIItem(ctx, "ui-0000000001", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("evicted item error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetAgentUIItem(ctx, "ui-0000000009", 0); err != nil {
		t.Fatalf("another task's item was evicted: %v", err)
	}
}

func TestDeleteAgentUIItemRemovesItAndItsRevisions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000001", TaskID: "task-a", Placement: "panel", Key: "design", Format: "html", Content: "one"}, testUILimits)
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000002", TaskID: "task-a", Placement: "panel", Key: "design", Format: "html", Content: "two"}, testUILimits)

	if err := s.DeleteAgentUIItem(ctx, "ui-0000000001"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAgentUIItem(ctx, "ui-0000000001", 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted item revision error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteAgentUIItem(ctx, "ui-0000000001"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete error = %v, want ErrNotFound", err)
	}
	// The key is free again: a new save starts a new item at revision 1.
	fresh := saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000003", TaskID: "task-a", Placement: "panel", Key: "design", Format: "html", Content: "three"}, testUILimits)
	if fresh.ID != "ui-0000000003" || fresh.Revision != 1 {
		t.Fatalf("fresh save = %s revision %d", fresh.ID, fresh.Revision)
	}
}

func TestDeleteTaskRemovesItsAgentUIItemsAndThoseOfItsSwarmWorkers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "parent")
	createTestTask(t, s, "bystander")
	if err := s.CreateSubtask(ctx, Subtask{ID: "st-worker0001", ParentTaskID: "parent", Title: "Worker", Status: "queued"}); err != nil {
		t.Fatal(err)
	}
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000001", TaskID: "parent", Placement: "panel", Format: "html", Content: "conductor"}, testUILimits)
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000002", TaskID: "st-worker0001", Placement: "inline", Format: "html", Content: "worker"}, testUILimits)
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000003", TaskID: "bystander", Placement: "panel", Format: "html", Content: "kept"}, testUILimits)

	if err := s.DeleteTask(ctx, "parent"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ui-0000000001", "ui-0000000002"} {
		if _, err := s.GetAgentUIItem(ctx, id, 0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s after task delete: %v, want ErrNotFound", id, err)
		}
	}
	if _, err := s.GetAgentUIItem(ctx, "ui-0000000003", 0); err != nil {
		t.Fatalf("bystander item removed: %v", err)
	}
}

func TestDeleteSubtaskRemovesTheWorkersAgentUIItems(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "parent")
	if err := s.CreateSubtask(ctx, Subtask{ID: "st-worker0001", ParentTaskID: "parent", Title: "Worker", Status: "queued"}); err != nil {
		t.Fatal(err)
	}
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000001", TaskID: "parent", Placement: "panel", Format: "html", Content: "conductor"}, testUILimits)
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000002", TaskID: "st-worker0001", Placement: "inline", Format: "html", Content: "worker"}, testUILimits)

	if err := s.DeleteSubtask(ctx, "st-worker0001"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAgentUIItem(ctx, "ui-0000000002", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("worker item after sub-task delete: %v, want ErrNotFound", err)
	}
	if _, err := s.GetAgentUIItem(ctx, "ui-0000000001", 0); err != nil {
		t.Fatalf("conductor item removed: %v", err)
	}
}

func TestSummarizeAgentUIItemsCountsByPlacementAndNamesTheOneShownLast(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	empty, err := s.SummarizeAgentUIItems(ctx, "task-a")
	if err != nil || empty.Cards != 0 || empty.Artifacts != 0 || empty.LatestID != "" {
		t.Fatalf("summary of nothing = %#v, %v", empty, err)
	}

	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000001", TaskID: "task-a", Placement: "panel", Key: "design", Format: "html", Title: "Design", Content: "one"}, testUILimits)
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000002", TaskID: "task-a", Placement: "inline", Format: "html", Title: "Weather", Content: "two"}, testUILimits)
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000003", TaskID: "task-b", Placement: "inline", Format: "html", Title: "Other task", Content: "three"}, testUILimits)

	summary, err := s.SummarizeAgentUIItems(ctx, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Artifacts != 1 || summary.Cards != 1 || summary.LatestID != "ui-0000000002" || summary.LatestTitle != "Weather" {
		t.Fatalf("summary = %#v", summary)
	}

	// All of this happens within one second, the precision of the stored
	// timestamps: "last" must not depend on them.
	saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000004", TaskID: "task-a", Placement: "panel", Key: "design", Format: "html", Title: "Design v2", Content: "four"}, testUILimits)
	summary, err = s.SummarizeAgentUIItems(ctx, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Artifacts != 1 || summary.Cards != 1 || summary.LatestID != "ui-0000000001" || summary.LatestTitle != "Design v2" {
		t.Fatalf("summary after a revision = %#v", summary)
	}
}

func TestSaveAgentUIItemKeepsEachRevisionsImages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := saveTestUIItem(t, s, AgentUIItem{
		ID: "ui-0000000001", TaskID: "task-a", Placement: "panel", Key: "shots",
		Format: "html", Title: "Shots", Content: "one", Images: `[{"name":"before","file":"aa.png"}]`,
	}, testUILimits)
	if first.Images != `[{"name":"before","file":"aa.png"}]` {
		t.Fatalf("saved images = %q", first.Images)
	}
	saveTestUIItem(t, s, AgentUIItem{
		ID: "ui-0000000002", TaskID: "task-a", Placement: "panel", Key: "shots",
		Format: "html", Title: "Shots", Content: "two", Images: `[{"name":"after","file":"bb.png"}]`,
	}, testUILimits)

	latest, err := s.GetAgentUIItem(ctx, first.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Images != `[{"name":"after","file":"bb.png"}]` {
		t.Fatalf("latest images = %q", latest.Images)
	}
	original, err := s.GetAgentUIItem(ctx, first.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if original.Images != `[{"name":"before","file":"aa.png"}]` {
		t.Fatalf("first revision's images = %q", original.Images)
	}
	listed, err := s.ListAgentUIItems(ctx, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Images != latest.Images {
		t.Fatalf("listed = %#v", listed)
	}

	plain := saveTestUIItem(t, s, AgentUIItem{ID: "ui-0000000003", TaskID: "task-a", Placement: "panel", Format: "html", Title: "Plain", Content: "x"}, testUILimits)
	if plain.Images != "" {
		t.Fatalf("an item saved without images has %q", plain.Images)
	}
}
