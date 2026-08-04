package store

import (
	"context"
	"testing"
)

func TestCountAttentionItemsMatchesOpenList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, item := range []AttentionItem{
		{ID: "open", Producer: "test", SourceKey: "open", SourceRevision: "1", Title: "Open", Reason: "reason", Category: "test", Urgency: "normal", NavigationJSON: `{}`, ActionsJSON: `[]`},
		{ID: "dismissed", Producer: "test", SourceKey: "dismissed", SourceRevision: "1", Title: "Dismissed", Reason: "reason", Category: "test", Urgency: "normal", NavigationJSON: `{}`, ActionsJSON: `[]`},
	} {
		if _, _, err := s.UpsertAttentionItem(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DismissAttentionItem(ctx, "dismissed", "1"); err != nil {
		t.Fatal(err)
	}
	items, err := s.ListAttentionItems(ctx, "open")
	if err != nil {
		t.Fatal(err)
	}
	count, err := s.CountAttentionItems(ctx, "open")
	if err != nil {
		t.Fatal(err)
	}
	if count != len(items) || count != 1 {
		t.Fatalf("count=%d items=%d", count, len(items))
	}
}
