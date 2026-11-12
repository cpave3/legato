package service

import (
	"testing"

	"github.com/cpave3/legato/config"
)

func TestAutoAsTheChangeLevelRunsChangesAtOnceButNotRiskyOnes(t *testing.T) {
	cfg := config.ReeveConfig{Levels: config.ReeveLevels{Change: "auto"}}
	got := ResolveReeveLevel(cfg, LevelCall{Class: "change", Calls: []string{"PATCH /api/tasks/T-1"}}, true)
	if got.Level != LevelAuto || got.By != "reeve.levels.change" {
		t.Fatalf("change = %+v, want auto by reeve.levels.change", got)
	}
	if got := ResolveReeveLevel(cfg, LevelCall{Class: "risky", Calls: []string{"POST /api/restart"}}, true); got.Level != LevelSigned {
		t.Fatalf("risky in the same configuration = %+v, want signed", got)
	}
}

func TestRiskyIsAutoOnlyWhenChosenAndItsOldDefaultIsCalledDefault(t *testing.T) {
	for _, tc := range []struct {
		risky    string
		enrolled bool
		want     ReeveLevel
	}{
		{"", true, LevelSigned},
		{"", false, LevelApprove},
		{"default", true, LevelSigned},
		{"default", false, LevelApprove},
		{"auto", true, LevelAuto},
		{"approve", true, LevelApprove},
	} {
		cfg := config.ReeveConfig{Levels: config.ReeveLevels{Risky: tc.risky}}
		if got := ResolveReeveLevel(cfg, LevelCall{Class: "risky", Calls: []string{"POST /api/restart"}}, tc.enrolled); got.Level != tc.want {
			t.Errorf("risky %q (enrolled %v) = %s, want %s", tc.risky, tc.enrolled, got.Level, tc.want)
		}
	}
}

func TestARuleAutoRunsOnlyTheCallsItMatches(t *testing.T) {
	cfg := config.ReeveConfig{Rules: []config.ReeveRule{{Pattern: "POST /api/tasks*", Level: "auto"}}}
	got := ResolveReeveLevel(cfg, LevelCall{Class: "change", Calls: []string{"POST /api/tasks"}}, true)
	if got.Level != LevelAuto || got.By != "rule POST /api/tasks*" {
		t.Fatalf("matching call = %+v", got)
	}
	if got := ResolveReeveLevel(cfg, LevelCall{Class: "change", Calls: []string{"PATCH /api/tasks/T-1"}}, true); got.Level != LevelApprove {
		t.Fatalf("a call the rule does not match = %+v, want approve", got)
	}
}

func TestHostAutoRunsHostCallsNotAsRootAtOnce(t *testing.T) {
	on := true
	cfg := config.ReeveConfig{HostOperationsAuto: &on}
	for _, kind := range []string{CallHostShell, CallEditFile, CallManageService} {
		got := ResolveReeveLevel(cfg, LevelCall{Class: "risky", Host: true, Calls: []string{kind + ":x"}}, true)
		if got.Level != LevelAuto || got.By != "reeve.host_operations_auto" {
			t.Errorf("%s = %+v, want auto by the setting", kind, got)
		}
	}
	got := ResolveReeveLevel(cfg, LevelCall{Class: "risky", Host: true, AsRoot: true, Calls: []string{"host_shell:dnf upgrade"}}, true)
	if got.Level != LevelVerified {
		t.Fatalf("as root with host auto on = %+v, want verified", got)
	}
	// A call that is not a host operation is not touched by it.
	if got := ResolveReeveLevel(cfg, LevelCall{Class: "risky", Calls: []string{"POST /api/restart"}}, true); got.Level != LevelSigned {
		t.Fatalf("a risky API call with host auto on = %+v, want signed", got)
	}
	// A rule can still ask for more.
	cfg.Rules = []config.ReeveRule{{Pattern: "host_shell:rm *", Level: "approve"}}
	if got := ResolveReeveLevel(cfg, LevelCall{Class: "risky", Host: true, Calls: []string{"host_shell:rm -rf build"}}, true); got.Level != LevelApprove {
		t.Fatalf("a rule raising a host call = %+v, want approve", got)
	}
}

func TestANarrowHostRuleAutoRunsOnlyPlainCommandsItMatches(t *testing.T) {
	cfg := config.ReeveConfig{Rules: []config.ReeveRule{
		{Pattern: "host_shell:journalctl *", Level: "auto"},
		{Pattern: "edit_file:/home/me/Projects/**", Level: "auto"},
	}}
	if got := ResolveReeveLevel(cfg, LevelCall{Class: "risky", Host: true, Calls: []string{"host_shell:journalctl -n 50 -u legato"}}, true); got.Level != LevelAuto {
		t.Fatalf("journalctl = %+v, want auto", got)
	}
	if got := ResolveReeveLevel(cfg, LevelCall{Class: "risky", Host: true, Calls: []string{"host_shell:ls"}}, true); got.Level != LevelSigned {
		t.Fatalf("ls = %+v, want signed", got)
	}
	// journalctl followed by another command is not what the rule allowed.
	if got := ResolveReeveLevel(cfg, LevelCall{Class: "risky", Host: true, Compound: true, Calls: []string{"host_shell:journalctl -n 5; rm -rf ~"}}, true); got.Level != LevelSigned {
		t.Fatalf("a compound command = %+v, want signed", got)
	}
	if got := ResolveReeveLevel(cfg, LevelCall{Class: "risky", Host: true, Calls: []string{"edit_file:/home/me/Projects/a/b.go"}}, true); got.Level != LevelAuto {
		t.Fatalf("a file in Projects = %+v, want auto", got)
	}
	if got := ResolveReeveLevel(cfg, LevelCall{Class: "risky", Host: true, Calls: []string{"edit_file:/home/me/.bashrc"}}, true); got.Level != LevelSigned {
		t.Fatalf("a file elsewhere = %+v, want signed", got)
	}
}

func TestAFloorIsNeverLoweredByAuto(t *testing.T) {
	on := true
	cfg := config.ReeveConfig{
		Levels:             config.ReeveLevels{Change: "auto", Risky: "auto"},
		Rules:              []config.ReeveRule{{Pattern: "*", Level: "auto"}},
		HostOperationsAuto: &on,
	}
	got := ResolveReeveLevel(cfg, LevelCall{Class: "risky", Calls: []string{"DELETE /api/tasks/T-1"}, Floor: LevelApprove, FloorWhy: "a deletion"}, true)
	if got.Level != LevelApprove || got.By != "a deletion" {
		t.Fatalf("a call with a floor = %+v, want approve by its floor", got)
	}
	got = ResolveReeveLevel(cfg, LevelCall{Class: "risky", Calls: []string{"config_edit"}, Floor: LevelSigned, FloorWhy: "the Reeve's own rules"}, true)
	if got.Level != LevelSigned {
		t.Fatalf("a signed floor = %+v", got)
	}
	got = ResolveReeveLevel(cfg, LevelCall{Class: "risky", Host: true, AsRoot: true, Calls: []string{"host_shell:id"}}, true)
	if got.Level != LevelVerified || got.By != "as root" {
		t.Fatalf("as root = %+v, want verified as root", got)
	}
	// Everything else in this configuration runs at once.
	if got := ResolveReeveLevel(cfg, LevelCall{Class: "risky", Calls: []string{"POST /api/restart"}}, true); got.Level != LevelAuto {
		t.Fatalf("restart = %+v, want auto", got)
	}
}

func TestReadsStayFreeAndAutoIsBelowApprove(t *testing.T) {
	if got := ResolveReeveLevel(config.ReeveConfig{}, LevelCall{Class: "read", Calls: []string{"GET /api/agents"}}, true); got.Level != LevelFree {
		t.Fatalf("read = %+v", got)
	}
	if got := StrictestLevel(LevelAuto, LevelApprove); got != LevelApprove {
		t.Fatalf("strictest of auto and approve = %s", got)
	}
	if got := StrictestLevel(LevelAuto, LevelAuto); got != LevelAuto {
		t.Fatalf("strictest of auto and auto = %s", got)
	}
}
