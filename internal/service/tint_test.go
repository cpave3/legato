package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestALegatoKeepsItsTint(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tints := NewTintService(db)
	ctx := context.Background()

	if got, err := tints.Tint(ctx); err != nil || got != "" {
		t.Fatalf("a new Legato's tint: %q, %v", got, err)
	}
	if err := tints.SetTint(ctx, "Purple "); err != nil {
		t.Fatal(err)
	}
	if got, _ := tints.Tint(ctx); got != "purple" {
		t.Fatalf("tint = %q, want purple", got)
	}
	if err := tints.SetTint(ctx, "chartreuse"); !errors.Is(err, ErrUnknownTint) {
		t.Fatalf("a colour that is not a tint: %v", err)
	}
	if got, _ := tints.Tint(ctx); got != "purple" {
		t.Fatalf("a refused tint changed it: %q", got)
	}
	if err := tints.SetTint(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := tints.Tint(ctx); got != "" {
		t.Fatalf("after clearing: %q", got)
	}
}
