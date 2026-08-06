package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAttentionSignalMigrationClassifiesLegacyRowsAndFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "attention-signals.db")
	s := newStoreAtMigration(t, path, "058_workflow_trigger_confirmation.sql")
	ctx := context.Background()
	insert := func(id, producer, revision string) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, `INSERT INTO attention_items
			(id, producer, source_key, source_revision, title, reason, category, urgency, navigation_json, actions_json, status)
			VALUES (?, ?, ?, ?, 'title', 'reason', 'test', 'normal', '{}', '[]', 'open')`, id, producer, id, revision); err != nil {
			t.Fatal(err)
		}
	}
	insert("plan", "plans", "proposed:1:0")
	insert("completion", "workflows", "completed:run:succeeded")
	insert("unknown", "future_producer", "state:1")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	migrated, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer migrated.Close()

	items, err := migrated.ListAttentionItems(ctx, "open", []string{"plan_decision", "workflow_manual_gate"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "plan" || items[0].SignalKind != "plan_decision" {
		t.Fatalf("default items = %+v", items)
	}
	for id, want := range map[string]string{"completion": "workflow_completion", "unknown": "legacy_unclassified"} {
		item, err := migrated.GetAttentionItem(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if item.SignalKind != want {
			t.Fatalf("%s signal kind = %q, want %q", id, item.SignalKind, want)
		}
		if id == "completion" && item.Status != "resolved" {
			t.Fatalf("excluded completion status = %q, want resolved", item.Status)
		}
		if id == "unknown" && item.Status != "open" {
			t.Fatalf("unknown legacy status = %q, want preserved open", item.Status)
		}
	}
}

func newStoreAtMigration(t *testing.T, path, lastMigration string) *Store {
	t.Helper()
	index := -1
	for i, name := range migrations {
		if name == lastMigration {
			index = i
			break
		}
	}
	if index < 0 {
		t.Fatalf("migration %q not found", lastMigration)
	}
	original := migrations
	migrations = append([]string(nil), migrations[:index+1]...)
	t.Cleanup(func() { migrations = original })
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	migrations = original
	return s
}
