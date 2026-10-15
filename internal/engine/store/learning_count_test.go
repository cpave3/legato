package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCountLearningsCountsOneStatus(t *testing.T) {
	db, err := New(filepath.Join(t.TempDir(), "count.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, item := range []Learning{
		{ID: "learn-a", Scope: "user", Kind: "fact", Claim: "a", Status: "candidate", Revision: 1},
		{ID: "learn-b", Scope: "user", Kind: "fact", Claim: "b", Status: "candidate", Revision: 1},
		{ID: "learn-c", Scope: "user", Kind: "fact", Claim: "c", Status: "validated", Revision: 1},
	} {
		if _, err := db.DB().NamedExec(`INSERT INTO learnings (id, scope, kind, claim, status, revision)
			VALUES (:id, :scope, :kind, :claim, :status, :revision)`, item); err != nil {
			t.Fatal(err)
		}
	}

	count, err := db.CountLearnings(context.Background(), "candidate")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("candidates = %d, want 2", count)
	}
}
