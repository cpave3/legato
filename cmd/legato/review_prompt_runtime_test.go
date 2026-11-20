package main

import (
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The TUI's Attention service and the web server's are separate: each must
// have the review prompt's action, or Start review passes fails from one.
func TestTheReviewPromptsActionIsRegisteredOnAnAttentionService(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	attention := service.NewAttentionService(db, events.New())
	prompt := service.NewReviewPromptAttentionAdapter(db, nil)

	registerReviewPrompt(attention, prompt)

	if err := attention.RegisterHandler(prompt.Producer(), prompt); err == nil {
		t.Fatal("the review prompt had no action handler")
	}
}
