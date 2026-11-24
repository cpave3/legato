package store

import (
	"context"
	"testing"
)

func TestASettingOfThisLegatoIsKeptAndChanged(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if got, err := s.GetInstanceValue(ctx, "web.tint"); err != nil || got != "" {
		t.Fatalf("before it is set: %q, %v", got, err)
	}
	for _, value := range []string{"purple", "yellow", ""} {
		if err := s.SetInstanceValue(ctx, "web.tint", value); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.GetInstanceValue(ctx, "web.tint"); got != value {
			t.Fatalf("after setting %q: %q", value, got)
		}
	}
}

func TestOnlyOneProcessClaimsAValueOfThisLegato(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	results := make(chan bool, 8)
	for range 8 {
		go func() {
			claimed, err := s.ClaimInstanceValue(ctx, "learning.template_migrated", "done")
			if err != nil {
				t.Error(err)
			}
			results <- claimed
		}()
	}
	claims := 0
	for range 8 {
		if <-results {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("%d claims, want 1", claims)
	}
	if err := s.DeleteInstanceValue(ctx, "learning.template_migrated"); err != nil {
		t.Fatal(err)
	}
	if claimed, err := s.ClaimInstanceValue(ctx, "learning.template_migrated", "done"); err != nil || !claimed {
		t.Fatalf("after it was let go: %v, %v", claimed, err)
	}
}

func TestAValueOfThisLegatoIsReplacedOnlyFromWhatWasRead(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.SetInstanceValue(ctx, "k", "old"); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ReplaceInstanceValue(ctx, "k", "stale", "new"); err != nil || ok {
		t.Fatalf("replaced from a value it no longer had: %v, %v", ok, err)
	}
	if ok, err := s.ReplaceInstanceValue(ctx, "k", "old", "new"); err != nil || !ok {
		t.Fatalf("not replaced: %v, %v", ok, err)
	}
	if got, _ := s.GetInstanceValue(ctx, "k"); got != "new" {
		t.Fatalf("value = %q", got)
	}
}
