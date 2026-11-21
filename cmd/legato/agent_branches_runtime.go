package main

import (
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// trackAgentBranches has the branches agents push under legato/ through the
// proxy, and those they open pull requests from, tracked on their tasks at
// once, with their pull requests looked for without waiting for the next
// poll. Without PR tracking the branches are still recorded. See
// docs/ai/pr-tracking.md § Agents' branches.
func (rt *approvalRuntime) trackAgentBranches(db *store.Store, prSvc service.PRTrackingService) {
	if rt == nil || rt.channels == nil {
		return
	}
	actions, _ := prSvc.(service.PRBranchActions)
	branches := service.NewAgentBranches(db, actions)
	rt.channels.SetBranchListener(func(b service.AgentBranch) { go branches.Track(b) })
}
