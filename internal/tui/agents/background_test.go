package agents

import (
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// What an agent in the background waits on, beside its state.
func TestBackgroundSummaryNamesOneJobAndCountsSeveralByWhatTheyDo(t *testing.T) {
	for _, tc := range []struct {
		jobs []service.BackgroundJob
		want string
	}{
		{nil, ""},
		{[]service.BackgroundJob{{Kind: "subagent", Label: "Explore: find the callers"}}, "subagent: Explore: find the callers"},
		{[]service.BackgroundJob{{Kind: "bash", Label: "npm run build"}, {Kind: "monitor", Label: "watch the deploy"}}, "2 jobs"},
		// Subagents at any depth count by what they do.
		{[]service.BackgroundJob{
			{Kind: "subagent", ID: "a1", Label: "Orchestrate", State: "background"},
			{Kind: "subagent", ID: "a2", Parent: "a1", Label: "Implement D1", State: "working"},
			{Kind: "subagent", ID: "a3", Parent: "a1", Label: "Implement D2", State: "working"},
			{Kind: "bash", ID: "b1", Label: "npm run build"},
		}, "2 sub-agents working · 1 job"},
		{[]service.BackgroundJob{
			{Kind: "subagent", ID: "a1", Label: "Orchestrate", State: "working"},
			{Kind: "subagent", ID: "a2", Parent: "a1", Label: "Implement D1", State: "waiting"},
		}, "2 sub-agents working"},
		{[]service.BackgroundJob{
			{Kind: "subagent", ID: "a1", Label: "Orchestrate", State: "background"},
			{Kind: "subagent", ID: "a2", Parent: "a1", Label: "Implement D1", State: "background"},
			{Kind: "bash", ID: "b1", Parent: "a2", Label: "sleep 25"},
		}, "2 sub-agents waiting · 1 job"},
	} {
		if got := backgroundSummary(tc.jobs); got != tc.want {
			t.Errorf("backgroundSummary(%v) = %q, want %q", tc.jobs, got, tc.want)
		}
	}
}
