package hotkey

import "testing"

func TestEveryPageHasRegistryDerivedShortlist(t *testing.T) {
	registry := DefaultRegistry()
	capabilities := NewCapabilities(
		CapabilityWorktrees, CapabilitySwarm, CapabilityVoice, CapabilityNotifications,
		CapabilityReview, CapabilityPlans, CapabilityAttention, CapabilitySync, CapabilityPR,
	)
	tests := []struct {
		page Page
		mode Mode
	}{
		{PageBoard, ""}, {PageDetail, ""}, {PageAgents, ""}, {PageReport, ""},
		{PageReview, ModeTour}, {PagePlans, ModeQueue}, {PageAttention, ""},
	}
	for _, test := range tests {
		t.Run(string(test.page), func(t *testing.T) {
			ctx := Context{Page: test.page, Mode: test.mode, Capabilities: capabilities, Predicates: NewPredicates(PredicateLocalTask, PredicateEditor)}
			if got := registry.Bindings(ctx, Shortlist); len(got) == 0 {
				t.Fatalf("%s shortlist is empty", test.page)
			}
		})
	}
}

func TestCapabilityGatedBindingIsAbsentFromEveryDisplaySurface(t *testing.T) {
	registry := DefaultRegistry()
	ctx := Context{Page: PageAgents}
	for _, surface := range []Surface{FullHelp, Shortlist, ShortcutBar, InlineHint} {
		for _, binding := range registry.Bindings(ctx, surface) {
			if binding.ID == ActionAgentsVoice {
				t.Fatalf("voice appeared on surface %d while disabled", surface)
			}
		}
	}
}
