package hotkey

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDefaultBindingsValidate(t *testing.T) {
	if _, err := NewRegistry(DefaultBindings()); err != nil {
		t.Fatal(err)
	}
}

func TestMoveTaskToWorkspaceIsDiscoverableOnBoardAndDetail(t *testing.T) {
	registry := DefaultRegistry()
	for _, page := range []Page{PageBoard, PageDetail} {
		ctx := Context{Page: page}
		binding, ok := registry.Resolve(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}}, ctx)
		if !ok || binding.Help != "Move task to workspace" {
			t.Fatalf("page %s binding = %+v, found = %v", page, binding, ok)
		}
		found := false
		for _, displayed := range registry.Bindings(ctx, FullHelp) {
			if displayed.ID == binding.ID {
				found = true
			}
		}
		if !found {
			t.Fatalf("page %s workspace move is absent from full help", page)
		}
	}
}

// The web server starts from web.enabled in the configuration. No key in
// the TUI starts or stops one.
func TestNoKeyStartsAWebServer(t *testing.T) {
	ctx := Context{Page: PageBoard, Capabilities: NewCapabilities(
		CapabilityWorktrees, CapabilitySwarm, CapabilityVoice, CapabilityNotifications,
		CapabilityReview, CapabilityPlans, CapabilityAttention, CapabilitySync, CapabilityPR,
		CapabilityAgentUI, CapabilityApprovals, CapabilityReeve,
	)}
	if binding, ok := DefaultRegistry().Resolve(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'W'}}, ctx); ok {
		t.Fatalf("W on the board runs %s", binding.ID)
	}
	for _, binding := range DefaultBindings() {
		if strings.Contains(strings.ToLower(binding.Help), "web server") {
			t.Errorf("%s (%s) still controls a web server", binding.ID, DisplayKey(binding))
		}
	}
}
