package store

import (
	"context"
	"errors"
	"testing"
)

func TestASavedResourceKeepsItsPathsInOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	saved, err := s.SaveResource(ctx, Resource{
		Name: "Obsidian vault",
		Paths: []ResourcePath{
			{Path: "/home/me/Documents/My Vault", Access: "write"},
			{Path: "/home/me/Documents/templates", Access: "read"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID == 0 {
		t.Fatalf("saved = %+v", saved)
	}

	resources, err := s.ListResources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].Name != "Obsidian vault" || len(resources[0].Paths) != 2 {
		t.Fatalf("resources = %+v", resources)
	}
	if got := resources[0].Paths; got[0] != (ResourcePath{Path: "/home/me/Documents/My Vault", Access: "write"}) || got[1].Access != "read" {
		t.Fatalf("paths = %+v", got)
	}
}

func TestSavingAResourceAgainReplacesItsPaths(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	saved, err := s.SaveResource(ctx, Resource{Name: "Notes", Paths: []ResourcePath{{Path: "/a", Access: "read"}, {Path: "/b", Access: "read"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveResource(ctx, Resource{ID: saved.ID, Name: "Vault", Paths: []ResourcePath{{Path: "/c", Access: "write"}}}); err != nil {
		t.Fatal(err)
	}

	resources, err := s.ListResources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].Name != "Vault" || len(resources[0].Paths) != 1 || resources[0].Paths[0].Path != "/c" {
		t.Fatalf("resources = %+v", resources)
	}
}

func TestADeletedResourceIsGone(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	saved, err := s.SaveResource(ctx, Resource{Name: "Notes", Paths: []ResourcePath{{Path: "/a", Access: "read"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteResource(ctx, saved.ID); err != nil {
		t.Fatal(err)
	}
	resources, err := s.ListResources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 0 {
		t.Fatalf("resources = %+v", resources)
	}
	if err := s.DeleteResource(ctx, saved.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v", err)
	}
}

func TestAnExecutionRemembersTheResourcesItWasGiven(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.AddExecutionResources(ctx, "ex-1", []string{"Vault", "Specs"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddExecutionResources(ctx, "ex-1", []string{"Vault"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AddExecutionResources(ctx, "ex-2", []string{"Other"}); err != nil {
		t.Fatal(err)
	}

	names, err := s.ListExecutionResources(ctx, "ex-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "Vault" || names[1] != "Specs" {
		t.Fatalf("names = %v", names)
	}
}
