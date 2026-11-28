package store

import (
	"context"
	"testing"
)

func TestManagedHookStatesAreKeptByFolderNameApartFromSkills(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	states, err := s.ManagedHookStates(ctx)
	if err != nil || len(states) != 0 {
		t.Fatalf("ManagedHookStates on a new database = %v, %v; want none", states, err)
	}
	if err := s.SetManagedHookEnabled(ctx, "codeeval", false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetManagedHookEnabled(ctx, "lint", true); err != nil {
		t.Fatal(err)
	}
	states, err = s.ManagedHookStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 || states["codeeval"] || !states["lint"] {
		t.Fatalf("states = %v, want codeeval off and lint on", states)
	}
	if err := s.SetManagedHookEnabled(ctx, "codeeval", true); err != nil {
		t.Fatal(err)
	}
	if states, _ := s.ManagedHookStates(ctx); !states["codeeval"] {
		t.Fatalf("codeeval still off after turning it on: %v", states)
	}
	if skills, _ := s.SkillStates(ctx); len(skills) != 0 {
		t.Fatalf("skill states = %v; a hook's state is not a skill's", skills)
	}
}
