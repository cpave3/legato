package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestProviderUpsertPreservesLocalTaskMetadata(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	provider := "jira"
	metadata := `{"triaged":true}`
	if err := s.CreateTask(context.Background(), Task{ID: "AB-1", Title: "Before", Status: "Backlog", Provider: &provider, Metadata: &metadata}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertTask(context.Background(), Task{ID: "AB-1", Title: "After", Status: "Triaged", Provider: &provider}); err != nil {
		t.Fatal(err)
	}
	task, err := s.GetTask(context.Background(), "AB-1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseTaskMetadata(task.Metadata)
	if err != nil || got["triaged"] != true {
		t.Fatalf("metadata=%#v err=%v", got, err)
	}
}
