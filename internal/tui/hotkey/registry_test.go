package hotkey

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRegistryRejectsDuplicateKeyInEffectiveScope(t *testing.T) {
	_, err := NewRegistry([]Binding{
		{ID: "view.reporting", Keys: []Key{Rune('R')}, Label: "Reporting", Help: "Open reports", Scope: PageScope(PageBoard), Section: "Views", Order: 1, Surfaces: FullHelp, Target: TargetRoot},
		{ID: "board.pull-request", Keys: []Key{Rune('R')}, Label: "Pull request", Help: "Open pull request", Scope: PageScope(PageBoard), Section: "Actions", Order: 2, Surfaces: FullHelp, Target: TargetPage},
	})
	if err == nil {
		t.Fatal("duplicate R should fail validation")
	}
	for _, want := range []string{"R", "view.reporting", "board.pull-request", "board"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not identify %q", err, want)
		}
	}
}

func TestRegistryRejectsImplicitGlobalShadowing(t *testing.T) {
	_, err := NewRegistry([]Binding{
		{ID: "global.refresh", Keys: []Key{Rune('r')}, Label: "Refresh", Help: "Refresh", Scope: GlobalScope(), Section: "General", Order: 1, Target: TargetRoot},
		{ID: "review.refresh", Keys: []Key{Rune('r')}, Label: "Refresh reviews", Help: "Refresh reviews", Scope: PageScope(PageReview), Section: "Actions", Order: 1, Target: TargetPage},
	})
	if err == nil || !strings.Contains(err.Error(), "shadow") {
		t.Fatalf("expected useful shadow error, got %v", err)
	}
}

func TestRegistryAllowsDeclaredSpecificOverride(t *testing.T) {
	registry, err := NewRegistry([]Binding{
		{ID: "global.refresh", Keys: []Key{Rune('r')}, Label: "Refresh", Help: "Refresh", Scope: GlobalScope(), Section: "General", Order: 1, Target: TargetRoot, Precedence: YieldToSpecific},
		{ID: "review.refresh", Keys: []Key{Rune('r')}, Label: "Refresh reviews", Help: "Refresh reviews", Scope: PageScope(PageReview), Section: "Actions", Order: 1, Target: TargetPage},
	})
	if err != nil {
		t.Fatalf("declared yield should validate: %v", err)
	}
	match, ok := registry.Resolve(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}}, Context{Page: PageReview})
	if !ok || match.ID != "review.refresh" {
		t.Fatalf("resolved %#v, %v; want review.refresh", match, ok)
	}
}

func TestCapabilityFilteringIsSharedByDispatchAndSurfaces(t *testing.T) {
	registry, err := NewRegistry([]Binding{{
		ID: "board.worktree", Keys: []Key{Rune('b')}, Label: "Worktree", Help: "Create task worktree",
		Scope: PageScope(PageBoard), Section: "Actions", Order: 1, Surfaces: FullHelp | Shortlist | ShortcutBar,
		Requires: []Capability{CapabilityWorktrees}, Target: TargetRoot,
	}})
	if err != nil {
		t.Fatal(err)
	}
	disabled := Context{Page: PageBoard}
	if _, ok := registry.Resolve(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}}, disabled); ok {
		t.Fatal("disabled binding resolved")
	}
	if got := registry.Bindings(disabled, FullHelp); len(got) != 0 {
		t.Fatalf("disabled binding displayed: %#v", got)
	}

	enabled := Context{Page: PageBoard, Capabilities: NewCapabilities(CapabilityWorktrees)}
	if match, ok := registry.Resolve(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}}, enabled); !ok || match.ID != "board.worktree" {
		t.Fatalf("enabled binding did not resolve: %#v, %v", match, ok)
	}
	if got := registry.Bindings(enabled, FullHelp); len(got) != 1 || got[0].ID != "board.worktree" {
		t.Fatalf("enabled binding missing from display: %#v", got)
	}
}

func TestBindingsHaveDeterministicSectionAndDisplayOrder(t *testing.T) {
	registry, err := NewRegistry([]Binding{
		{ID: "board.last", Keys: []Key{Rune('z')}, Label: "Last", Help: "Last", Scope: PageScope(PageBoard), Section: "Actions", SectionOrder: 2, Order: 20, Surfaces: FullHelp, Target: TargetPage},
		{ID: "board.first", Keys: []Key{Rune('a')}, Label: "First", Help: "First", Scope: PageScope(PageBoard), Section: "Actions", SectionOrder: 2, Order: 10, Surfaces: FullHelp, Target: TargetPage},
		{ID: "board.navigation", Keys: []Key{Special(tea.KeyEnter, "enter")}, Label: "Open", Help: "Open", Scope: PageScope(PageBoard), Section: "Navigation", SectionOrder: 1, Order: 10, Surfaces: FullHelp, Target: TargetPage},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := registry.Bindings(Context{Page: PageBoard}, FullHelp)
	want := []ActionID{"board.navigation", "board.first", "board.last"}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("order = %#v, want %#v", got, want)
		}
	}
}

func TestRegistryRejectsInvalidDisplayedBinding(t *testing.T) {
	tests := []struct {
		name    string
		binding Binding
		want    string
	}{
		{"action id", Binding{Keys: []Key{Rune('x')}, Label: "X", Help: "X", Scope: PageScope(PageBoard), Section: "Actions", Surfaces: FullHelp, Target: TargetPage}, "action ID"},
		{"scope", Binding{ID: "bad.scope", Keys: []Key{Rune('x')}, Label: "X", Help: "X", Scope: PageScope(Page("bogus")), Section: "Actions", Surfaces: FullHelp, Target: TargetPage}, "scope"},
		{"label", Binding{ID: "bad.label", Keys: []Key{Rune('x')}, Help: "X", Scope: PageScope(PageBoard), Section: "Actions", Surfaces: FullHelp, Target: TargetPage}, "label"},
		{"target", Binding{ID: "bad.target", Keys: []Key{Rune('x')}, Label: "X", Help: "X", Scope: PageScope(PageBoard), Section: "Actions", Surfaces: FullHelp}, "dispatch target"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRegistry([]Binding{tt.binding})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestKeyMatchesTypedControlAndRuneKeys(t *testing.T) {
	if !Rune('R').Matches(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}}) {
		t.Fatal("rune key did not match")
	}
	if !Special(tea.KeyCtrlR, "ctrl+r").Matches(tea.KeyMsg{Type: tea.KeyCtrlR}) {
		t.Fatal("control key did not match")
	}
	if Rune('r').Matches(tea.KeyMsg{Type: tea.KeyCtrlR}) {
		t.Fatal("rune key matched control key")
	}
}
