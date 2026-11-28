package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestNativeSecurityIncidentsDeduplicateAndSurviveReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "incidents.db")
	db, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.RecordNativeSecurityIncident(ctx, "task", "execution", "generation", "permission"); err != nil {
		t.Fatal(err)
	}
	records, err := db.ListNativeSecurityIncidents(ctx)
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %+v, %v", records, err)
	}
	original := records[0]
	if err := db.RecordNativeSecurityIncident(ctx, "task", "execution", "generation", "question"); err != nil {
		t.Fatal(err)
	}
	records, err = db.ListNativeSecurityIncidents(ctx)
	if err != nil || len(records) != 1 {
		t.Fatalf("repeated refusal = %+v, %v", records, err)
	}
	first := records[0]
	if first.TaskID != "task" || first.ExecutionID != "execution" || first.Generation != "generation" || first.Count != 2 || first.FirstSeenAt != original.FirstSeenAt || first.Category != "permission" {
		t.Fatalf("incident = %+v", first)
	}
	started, err := time.Parse(time.RFC3339Nano, first.FirstSeenAt)
	if err != nil {
		t.Fatal(err)
	}
	last, err := time.Parse(time.RFC3339Nano, first.LastSeenAt)
	if err != nil || !last.After(started) {
		t.Fatalf("last refusal = %q, first = %q, %v", first.LastSeenAt, first.FirstSeenAt, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	records, err = db.ListNativeSecurityIncidents(ctx)
	if err != nil || len(records) != 1 || records[0] != first {
		t.Fatalf("reopened = %+v, %v", records, err)
	}
	if err := db.RecordNativeSecurityIncident(ctx, "task", "execution", "next-generation", "plan"); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordNativeSecurityIncident(ctx, "task", "other-execution", "generation", "untrusted payload must not be retained"); err != nil {
		t.Fatal(err)
	}
	records, err = db.ListNativeSecurityIncidents(ctx)
	if err != nil || len(records) != 3 {
		t.Fatalf("independent generations/executions = %+v, %v", records, err)
	}
	for _, r := range records {
		if r.ExecutionID == "other-execution" && r.Category != "unknown" {
			t.Fatalf("unbounded category retained: %+v", r)
		}
	}
}
