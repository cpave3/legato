package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newRulesFixture(t *testing.T) (devicesFixture, *ReeveRules) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte(""), 0o600)
	t.Setenv("LEGATO_CONFIG", path)
	f := newDevicesFixture(t)
	rules := &ReeveRules{Settings: NewSettingsService(f.db, nil), Approvals: f.approvals, Store: f.db}
	rules.Register()
	return f, rules
}

func TestTheReevesOwnRulesAreNotChangedDirectly(t *testing.T) {
	_, rules := newRulesFixture(t)
	ctx := context.Background()
	if err := rules.Settings.Set(ctx, "reeve.levels.risky", json.RawMessage(`"approve"`), "web"); !errors.Is(err, ErrOwnRule) {
		t.Fatalf("a direct change: %v", err)
	}
	if err := rules.Settings.SetEntry(ctx, "reeve.rules", "POST /api/restart", json.RawMessage(`{"level": "approve"}`), "web"); !errors.Is(err, ErrOwnRule) {
		t.Fatalf("a direct rule: %v", err)
	}
}

func TestWithNoDeviceEnrolledAnOwnRuleChangeIsRefused(t *testing.T) {
	_, rules := newRulesFixture(t)
	if _, err := rules.Propose(context.Background(), RuleChange{Key: "reeve.levels.risky", Value: json.RawMessage(`"approve"`)}, "web"); !errors.Is(err, ErrNoDeviceEnrolled) {
		t.Fatalf("propose without a device: %v", err)
	}
}

func TestAnOwnRuleChangesOnceASignedApprovalAllowsIt(t *testing.T) {
	f, rules := newRulesFixture(t)
	ctx := context.Background()
	p := newPhone(t)
	f.enroll(t, p, "Pixel")

	approval, err := rules.Propose(ctx, RuleChange{Key: "reeve.levels.risky", Value: json.RawMessage(`"verified"`)}, "web")
	if err != nil {
		t.Fatal(err)
	}
	if !approval.Signed {
		t.Fatal("the approval does not need a signature")
	}
	if _, err := f.approvals.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "allow", By: "web"}); !errors.Is(err, ErrApprovalNeedsSignature) {
		t.Fatalf("a tap: %v", err)
	}
	if _, err := f.approvals.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "allow", By: "phone", Signature: f.sign(t, p, approval.ID, "allow")}); err != nil {
		t.Fatal(err)
	}
	if got, _ := rules.Settings.Get(ctx, "reeve.levels.risky"); got.Value != "verified" {
		t.Fatalf("reeve.levels.risky = %v", got.Value)
	}
}
