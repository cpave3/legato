package agents

import (
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// What an agent in the background waits on, beside its state.
func TestBackgroundSummaryNamesOneJobAndCountsSeveral(t *testing.T) {
	for _, tc := range []struct {
		jobs []service.BackgroundJob
		want string
	}{
		{nil, ""},
		{[]service.BackgroundJob{{Kind: "subagent", Label: "Explore: find the callers"}}, "subagent: Explore: find the callers"},
		{[]service.BackgroundJob{{Kind: "bash", Label: "npm run build"}, {Kind: "monitor", Label: "watch the deploy"}}, "2 jobs"},
	} {
		if got := backgroundSummary(tc.jobs); got != tc.want {
			t.Errorf("backgroundSummary(%v) = %q, want %q", tc.jobs, got, tc.want)
		}
	}
}
