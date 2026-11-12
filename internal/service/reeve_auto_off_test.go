package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"
)

func saved(t *testing.T, f devicesFixture, key, value string) {
	t.Helper()
	if err := f.db.SetSetting(context.Background(), key, json.RawMessage(value), "test"); err != nil {
		t.Fatal(err)
	}
}

func savedRule(t *testing.T, f devicesFixture, pattern, level string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"pattern": pattern, "level": level})
	if err := f.db.SetSettingEntry(context.Background(), "reeve.rules", pattern, raw, "test"); err != nil {
		t.Fatal(err)
	}
}

func TestMakingTheReevesRulesStricterNeedsNoSignature(t *testing.T) {
	f, rules := newRulesFixture(t)
	ctx := context.Background()
	saved(t, f, "reeve.levels.change", `"auto"`)
	saved(t, f, "reeve.levels.risky", `"auto"`)
	saved(t, f, "reeve.host_operations_auto", `true`)
	savedRule(t, f, "POST /api/tasks*", "auto")
	savedRule(t, f, "POST /api/restart", "signed")
	savedRule(t, f, "PATCH /api/tasks/*", "auto")

	for key, value := range map[string]string{
		"reeve.levels.change":        `"approve"`,
		"reeve.levels.risky":         `"default"`,
		"reeve.host_operations_auto": `false`,
	} {
		if err := rules.Settings.Set(ctx, key, json.RawMessage(value), "web"); err != nil {
			t.Errorf("tightening %s to %s: %v", key, value, err)
		}
	}
	if err := rules.Settings.SetEntry(ctx, "reeve.rules", "POST /api/tasks*", json.RawMessage(`{"pattern":"POST /api/tasks*","level":"approve"}`), "web"); err != nil {
		t.Errorf("tightening a rule: %v", err)
	}
	// Removing an auto rule leaves its calls to their class, which is no
	// lower than auto.
	if err := rules.Settings.DeleteEntry(ctx, "reeve.rules", "PATCH /api/tasks/*"); err != nil {
		t.Errorf("removing an auto rule: %v", err)
	}

	// Loosening still needs a signature.
	for key, value := range map[string]string{
		"reeve.levels.change":        `"auto"`,
		"reeve.levels.risky":         `"approve"`,
		"reeve.host_operations_auto": `true`,
	} {
		if err := rules.Settings.Set(ctx, key, json.RawMessage(value), "web"); !errors.Is(err, ErrOwnRule) {
			t.Errorf("loosening %s to %s: %v", key, value, err)
		}
	}
	if err := rules.Settings.SetEntry(ctx, "reeve.rules", "POST /api/restart", json.RawMessage(`{"pattern":"POST /api/restart","level":"auto"}`), "web"); !errors.Is(err, ErrOwnRule) {
		t.Errorf("loosening a rule: %v", err)
	}
	if err := rules.Settings.SetEntry(ctx, "reeve.rules", "DELETE *", json.RawMessage(`{"pattern":"DELETE *","level":"verified"}`), "web"); !errors.Is(err, ErrOwnRule) {
		t.Errorf("a new rule: %v", err)
	}
	// Removing a signed rule lowers what its calls need.
	if err := rules.Settings.DeleteEntry(ctx, "reeve.rules", "POST /api/restart"); !errors.Is(err, ErrOwnRule) {
		t.Errorf("removing a signed rule: %v", err)
	}
}

func TestTheOffSwitchPutsEverythingAutoBackToApproveWithoutASignature(t *testing.T) {
	f, rules := newRulesFixture(t)
	ctx := context.Background()
	saved(t, f, "reeve.levels.change", `"auto"`)
	saved(t, f, "reeve.levels.risky", `"auto"`)
	saved(t, f, "reeve.host_operations_auto", `true`)
	savedRule(t, f, "POST /api/tasks*", "auto")
	savedRule(t, f, "host_shell:journalctl *", "auto")
	savedRule(t, f, "POST /api/restart", "verified")

	status, err := rules.Auto(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !status.On || status.Change != "auto" || status.Risky != "auto" || !status.HostAuto || len(status.Rules) != 2 {
		t.Fatalf("before = %+v", status)
	}

	result, err := rules.AutoOff(ctx, "web")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Pinned) != 0 || len(result.Changed) != 5 {
		t.Fatalf("result = %+v", result)
	}
	cfg, _ := rules.Settings.Effective(ctx)
	if cfg.Reeve.Levels.Change != "approve" || cfg.Reeve.Levels.Risky != "approve" || cfg.Reeve.HostOperationsAutoOn() {
		t.Fatalf("after: levels %+v, host auto %v", cfg.Reeve.Levels, cfg.Reeve.HostOperationsAutoOn())
	}
	for _, r := range cfg.Reeve.Rules {
		want := "approve"
		if r.Pattern == "POST /api/restart" {
			want = "verified"
		}
		if r.Level != want {
			t.Errorf("rule %s = %s, want %s", r.Pattern, r.Level, want)
		}
	}
	if status, _ := rules.Auto(ctx); status.On {
		t.Fatalf("after = %+v", status)
	}
	// It made no approval.
	if pending, _ := f.approvals.Pending(ctx); len(pending) != 0 {
		t.Fatalf("approvals = %+v", pending)
	}
}

func TestTheOffSwitchSaysWhatConfigYamlPins(t *testing.T) {
	f, rules := newRulesFixture(t)
	ctx := context.Background()
	if err := os.WriteFile(os.Getenv("LEGATO_CONFIG"), []byte("reeve:\n  levels:\n    change: auto\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	saved(t, f, "reeve.host_operations_auto", `true`)
	result, err := rules.AutoOff(ctx, "web")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Pinned, []string{"reeve.levels.change"}) || !slices.Equal(result.Changed, []string{"reeve.host_operations_auto"}) {
		t.Fatalf("result = %+v", result)
	}
	if status, _ := rules.Auto(ctx); !status.On || !slices.Equal(status.Pinned, []string{"reeve.levels.change"}) {
		t.Fatalf("status = %+v", status)
	}
}
