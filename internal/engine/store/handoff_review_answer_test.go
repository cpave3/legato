package store

import (
	"context"
	"testing"
)

// Each answer to an item has a marker of its own: the same answer given
// again is a new answer, with a new marker.
func TestEachAnswerToAReviewItemHasAMarkerOfItsOwn(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "card")
	for _, id := range []string{"b-1", "b-2"} {
		if err := s.UpsertHandoffReviewItem(ctx, HandoffReviewItem{ID: id, TaskID: "card", Kind: "blocking", Pass: 1, PR: 7, Version: 1, CreatedAt: "2026-11-24T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
	}
	marker := func(id string) string {
		t.Helper()
		item, err := s.GetHandoffReviewItem(ctx, "card", id)
		if err != nil {
			t.Fatal(err)
		}
		return item.AnswerMarker
	}
	if m := marker("b-1"); m != "" {
		t.Fatalf("an unanswered item has a marker %q", m)
	}
	for _, id := range []string{"b-1", "b-2"} {
		if err := s.AnswerHandoffReviewItem(ctx, "card", id, "fixed", "", "2026-11-24T00:00:01Z"); err != nil {
			t.Fatal(err)
		}
	}
	first, other := marker("b-1"), marker("b-2")
	if first == "" || first == other {
		t.Fatalf("markers %q and %q; want one each", first, other)
	}
	if err := s.AnswerHandoffReviewItem(ctx, "card", "b-1", "fixed", "", "2026-11-24T00:00:01Z"); err != nil {
		t.Fatal(err)
	}
	if again := marker("b-1"); again == "" || again == first {
		t.Fatalf("answered again: marker %q, was %q", again, first)
	}
}
