package main

import (
	"strings"
	"testing"
)

// `legato review-pass list` prints the Shipping page's list (LEG-706):
// every review loop with its phase, as GET /api/shipping gives it.
func TestReviewPassListPrintsEveryLoopWithItsPhase(t *testing.T) {
	_, db := reviewPassStore(t)
	outsideASession(t)
	loop := stewardedLoop(t, db)

	out, code := runCLICaptured(t, "review-pass", "list", "--json")
	if code != 0 || !strings.Contains(out, `"loop_id":"`+loop+`"`) || !strings.Contains(out, `"phase":"watching"`) {
		t.Fatalf("list --json: exit %d, out %s", code, out)
	}
	out, code = runCLICaptured(t, "review-pass", "list")
	if code != 0 || !strings.Contains(out, "acme/widget#12") || !strings.Contains(out, "watching") || !strings.Contains(out, loop) {
		t.Fatalf("list: exit %d, out %s", code, out)
	}
}
