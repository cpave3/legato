package store

import (
	"context"
	"testing"
)

func TestSkillStatesAreKeptByFolderName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	states, err := s.SkillStates(ctx)
	if err != nil || len(states) != 0 {
		t.Fatalf("SkillStates on a new database = %v, %v; want none", states, err)
	}
	if err := s.SetSkillEnabled(ctx, "tdd", false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSkillEnabled(ctx, "code-review", true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSkillEnabled(ctx, "tdd", true); err != nil {
		t.Fatal(err)
	}
	states, err = s.SkillStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 || !states["tdd"] || !states["code-review"] {
		t.Fatalf("states = %v, want tdd and code-review on", states)
	}
	if err := s.SetSkillEnabled(ctx, "tdd", false); err != nil {
		t.Fatal(err)
	}
	if states, _ := s.SkillStates(ctx); states["tdd"] {
		t.Fatalf("tdd still on after turning it off: %v", states)
	}
}
