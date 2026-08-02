package main

import (
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestLearningPromptProviderDisabled(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "disabled.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if provider := learningPromptProvider(&config.Config{Learning: config.LearningConfig{Enabled: true, PromptEnabled: false}}, db, nil); provider != nil {
		t.Fatal("learning prompt provider enabled while prompt learning is disabled")
	}
	if provider := learningPromptProvider(&config.Config{Learning: config.LearningConfig{PromptEnabled: true}}, db, nil); provider == nil {
		t.Fatal("learning prompt provider disabled while prompt learning is enabled")
	}
}

func TestLearningWorkflowExecutorsAreRegisteredWithSharedService(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	learning := service.NewLearningService(db, nil)
	executors := learningWorkflowExecutors(db, learning, nil, "/opt/legato")
	got := map[string]bool{}
	for _, executor := range executors {
		got[executor.Type()] = true
	}
	for _, want := range []string{"agent.interview", "learning.packet.prepare", "learning.proposals.submit"} {
		if !got[want] {
			t.Fatalf("executor %q not registered: %+v", want, got)
		}
	}
}
