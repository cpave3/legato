package service

import (
	"testing"

	"github.com/cpave3/legato/config"
)

func TestTheReevesDefaultLevels(t *testing.T) {
	var cfg config.ReeveConfig
	for _, tc := range []struct {
		class    string
		call     string
		enrolled bool
		want     ReeveLevel
	}{
		{"read", "GET /api/agents", true, LevelFree},
		{"change", "POST /api/tasks", true, LevelApprove},
		{"risky", "POST /api/restart", false, LevelApprove},
		{"risky", "POST /api/restart", true, LevelSigned},
	} {
		if got := ReeveLevelFor(cfg, tc.class, tc.call, tc.enrolled); got != tc.want {
			t.Errorf("%s %s (enrolled %v) = %s, want %s", tc.class, tc.call, tc.enrolled, got, tc.want)
		}
	}
}

func TestLevelsPerClassAndRulesThatRaiseOrLowerThem(t *testing.T) {
	cfg := config.ReeveConfig{
		Levels: config.ReeveLevels{Change: "signed", Risky: "verified"},
		Rules: []config.ReeveRule{
			{Pattern: "POST /api/tasks*", Level: "approve"},
			{Pattern: "* /api/config/settings/*", Level: "verified"},
		},
	}
	if got := ReeveLevelFor(cfg, "change", "PATCH /api/tasks/T-1", true); got != LevelSigned {
		t.Errorf("change by its class = %s", got)
	}
	if got := ReeveLevelFor(cfg, "change", "POST /api/tasks/T-1/archive", true); got != LevelApprove {
		t.Errorf("a rule lowering it = %s", got)
	}
	if got := ReeveLevelFor(cfg, "change", "PUT /api/config/settings/theme", true); got != LevelVerified {
		t.Errorf("a rule raising it = %s", got)
	}
	if got := ReeveLevelFor(cfg, "risky", "POST /api/restart", true); got != LevelVerified {
		t.Errorf("risky = %s", got)
	}
	// A signed level with no device enrolled cannot be met: it stays signed,
	// and the change is refused rather than downgraded.
	if got := ReeveLevelFor(cfg, "change", "PATCH /api/tasks/T-1", false); got != LevelSigned {
		t.Errorf("without a device = %s", got)
	}
}

func TestTheStrictestLevelOfASetIsItsLevel(t *testing.T) {
	if got := StrictestLevel(LevelApprove, LevelVerified, LevelSigned); got != LevelVerified {
		t.Fatalf("strictest = %s", got)
	}
}
