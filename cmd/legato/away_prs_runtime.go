package main

import "github.com/cpave3/legato/internal/service"

// trackAwayPRs gives this main's worker connections its PR tracking, so the
// PRs of the branches a worker reports for an away task are found at once
// with the main's own GitHub read token. Without PR tracking the branches
// are still recorded. See docs/ai/pr-tracking.md § Away tasks.
func (m *mainHandoffs) trackAwayPRs(prSvc service.PRTrackingService) {
	if actions, ok := prSvc.(service.PRBranchActions); ok {
		m.connections.SetPRTracking(actions)
	}
}
