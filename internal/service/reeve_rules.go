package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// ApprovalSourceReeveRules is the approval of a change to one of the
// Reeve's own rules (levels, rules, host operations, the peer setting).
const ApprovalSourceReeveRules = "reeve_rules"

var (
	// ErrOwnRule: one of the Reeve's own rules, changed only with a signed
	// approval (ReeveRules.Propose).
	ErrOwnRule = errors.New("this is one of the Reeve's own rules: it changes only with a signed approval")
	// ErrNoDeviceEnrolled: a signed approval is needed and no device is
	// enrolled to sign it; the change is refused, never downgraded.
	ErrNoDeviceEnrolled = errors.New("no approval device is enrolled: enroll a phone, passkey or security key first (Settings › Security)")
)

type signedRuleKey struct{}

// isOwnRule reports whether a setting key or entry list is one of the
// Reeve's own rules.
func isOwnRule(key string) bool {
	if s, ok := config.LookupSetting(key); ok {
		return s.OwnRule
	}
	if l, ok := config.LookupEntryList(key); ok {
		return l.OwnRule
	}
	return false
}

// ownRuleAllowed refuses a change to an own rule unless it comes from a
// signed approval being applied.
func ownRuleAllowed(ctx context.Context, key string) error {
	if !isOwnRule(key) {
		return nil
	}
	if signed, _ := ctx.Value(signedRuleKey{}).(bool); signed {
		return nil
	}
	return fmt.Errorf("%s: %w", key, ErrOwnRule)
}

// RuleChange is a change to one of the Reeve's own rules: a setting's new
// value, or an entry of reeve.rules; Delete resets or removes it.
type RuleChange struct {
	Key    string          `json:"key"`
	Name   string          `json:"name,omitempty"`
	Value  json.RawMessage `json:"value,omitempty"`
	Delete bool            `json:"delete,omitempty"`
}

// ReeveRules puts changes to the Reeve's own rules to the person as signed
// approvals, and applies them once signed.
type ReeveRules struct {
	Settings  *SettingsService
	Approvals *ApprovalService
	Store     *store.Store
}

// Register makes the approval service apply signed rule changes.
func (r *ReeveRules) Register() {
	r.Approvals.RegisterResolver(ApprovalSourceReeveRules, reeveRulesResolver{r})
}

// Propose checks a change and submits it for a signed approval. It is
// refused when no device is enrolled.
func (r *ReeveRules) Propose(ctx context.Context, change RuleChange, by string) (*Approval, error) {
	if !isOwnRule(change.Key) {
		return nil, fmt.Errorf("%s is not one of the Reeve's own rules", change.Key)
	}
	devices, err := r.Store.ListApprovalDevices(ctx, store.DeviceActive)
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return nil, ErrNoDeviceEnrolled
	}
	if !change.Delete {
		if _, ok := config.LookupEntryList(change.Key); ok {
			if _, err := r.Settings.CheckEntry(ctx, change.Key, change.Name, change.Value); err != nil {
				return nil, err
			}
		} else if _, err := r.Settings.Check(ctx, change.Key, change.Value); err != nil {
			return nil, err
		}
	}
	what := change.Key
	if change.Name != "" {
		what += " " + change.Name
	}
	verb := "Set"
	if change.Delete {
		verb = "Reset"
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	approval, _, err := r.Approvals.Submit(ctx, ApprovalInput{
		Source: ApprovalSourceReeveRules, SourceKey: hex.EncodeToString(b),
		Title:   fmt.Sprintf("%s %s's rule %s", verb, r.Approvals.ReeveName(ctx).Mid(), what),
		Summary: string(change.Value), Details: change, Signed: true,
		Options: []ApprovalOption{{ID: "allow", Label: "Allow", Kind: ApprovalAllow}, {ID: "deny", Label: "Deny", Kind: ApprovalDeny}},
	})
	_ = by
	return approval, err
}

// apply makes a signed change.
func (r *ReeveRules) apply(ctx context.Context, change RuleChange, by string) error {
	ctx = context.WithValue(ctx, signedRuleKey{}, true)
	_, isList := config.LookupEntryList(change.Key)
	switch {
	case isList && change.Delete:
		return r.Settings.DeleteEntry(ctx, change.Key, change.Name)
	case isList:
		return r.Settings.SetEntry(ctx, change.Key, change.Name, change.Value, by)
	case change.Delete:
		return r.Settings.Reset(ctx, change.Key)
	}
	return r.Settings.Set(ctx, change.Key, change.Value, by)
}

type reeveRulesResolver struct{ r *ReeveRules }

func (res reeveRulesResolver) Resolve(ctx context.Context, approval Approval, decision ApprovalDecision) error {
	if decision.Decision != ApprovalAllow {
		return nil
	}
	var change RuleChange
	if err := json.Unmarshal(approval.Details, &change); err != nil {
		return err
	}
	by := decision.By
	if decision.Device != "" {
		by = decision.Device
	}
	return res.r.apply(ctx, change, by)
}
