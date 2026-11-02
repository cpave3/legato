package store

import (
	"context"
	"errors"
	"testing"
)

func TestRepositoryPresetsAreSavedListedAndRemoved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	saved, err := s.SaveRepositoryPreset(ctx, RepositoryPreset{Name: "Widget", Repo: "acme/widget", BaseBranch: "main"})
	if err != nil || saved.ID == 0 {
		t.Fatalf("SaveRepositoryPreset: %+v, %v", saved, err)
	}
	if _, err := s.SaveRepositoryPreset(ctx, RepositoryPreset{Name: "Widget", Repo: "acme/other"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("a second Widget: %v, want ErrConflict", err)
	}
	if _, err := s.SaveRepositoryPreset(ctx, RepositoryPreset{Name: "Notes", PrimaryDir: "/srv/notes"}); err != nil {
		t.Fatal(err)
	}
	listed, err := s.ListRepositoryPresets(ctx)
	if err != nil || len(listed) != 2 || listed[0].Name != "Widget" || listed[0].Repo != "acme/widget" || listed[1].PrimaryDir != "/srv/notes" {
		t.Fatalf("listed %+v, %v", listed, err)
	}
	if err := s.DeleteRepositoryPreset(ctx, saved.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRepositoryPreset(ctx, saved.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting twice: %v", err)
	}
	if listed, _ := s.ListRepositoryPresets(ctx); len(listed) != 1 {
		t.Fatalf("after delete: %+v", listed)
	}
}
