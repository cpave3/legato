package service

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cpave3/legato/config"
)

// UsageRuleLatch retains the current selection and the triggering reading's
// reset. Rule is the complete rule identity, so an edit invalidates a latch.
type UsageRuleLatch struct {
	Current string    `json:"current"`
	Rule    string    `json:"rule,omitempty"`
	Reset   time.Time `json:"reset,omitzero"`
}

type UsageProfileReason struct {
	Text        string    `json:"text"`
	Rule        string    `json:"rule,omitempty"`
	Meter       string    `json:"meter,omitempty"`
	UsedPercent float64   `json:"used_percent,omitempty"`
	Threshold   float64   `json:"threshold,omitempty"`
	Reset       time.Time `json:"reset,omitzero"`
}

func usageRuleIdentity(r config.UsageRule) string {
	return fmt.Sprintf("%s:%d:%s:%g:%s", r.Name, *r.Order, r.Meter, r.AtLeast, r.Profile)
}

func usageMeter(readings []ProviderUsage, meter string) (UsageWindow, *time.Time, bool) {
	kind, account, window, valid := config.ParseUsageMeter(meter)
	if !valid {
		return UsageWindow{}, nil, false
	}
	provider := config.UsageReaderID(kind, account)
	for _, p := range readings {
		if p.Provider != provider || p.State != UsageOK || p.AsOf == nil {
			continue
		}
		for _, w := range p.Windows {
			if w.Meter == window {
				return w, p.AsOf, true
			}
		}
	}
	return UsageWindow{}, nil, false
}

// EvaluateUsageRules selects defaults without I/O. Missing evidence holds the
// current profile unless a rule matches. A latch releases only on post-reset
// evidence, never merely because the wall clock passed a reset.
func EvaluateUsageRules(rules []config.UsageRule, profiles []config.UsageProfile, fallback string, readings []ProviderUsage, latch UsageRuleLatch, now time.Time) (string, UsageProfileReason, UsageRuleLatch) {
	ordered := slices.Clone(rules)
	slices.SortFunc(ordered, func(a, b config.UsageRule) int {
		if *a.Order < *b.Order {
			return -1
		}
		if *a.Order > *b.Order {
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	known := func(name string) string {
		for _, p := range profiles {
			if strings.EqualFold(p.Name, name) {
				return p.Name
			}
		}
		return ""
	}
	latch.Current = known(latch.Current)
	held := false
	for _, r := range ordered {
		if usageRuleIdentity(r) == latch.Rule {
			held = true
			break
		}
	}
	if !held {
		latch.Rule = ""
		latch.Reset = time.Time{}
	}
	missing := false
	for _, r := range ordered {
		identity := usageRuleIdentity(r)
		w, asOf, ok := usageMeter(readings, r.Meter)
		latched := latch.Rule == identity
		if latched {
			if latch.Reset.IsZero() && ok && w.ResetsAt.After(now) {
				latch.Reset = w.ResetsAt
			}
			if ok && !latch.Reset.IsZero() && !now.Before(latch.Reset) && !asOf.Before(latch.Reset) && w.UsedPercent < r.AtLeast {
				latched = false
				latch.Rule = ""
				latch.Reset = time.Time{}
			}
		}
		if latched || (ok && w.UsedPercent >= r.AtLeast) {
			if !latched {
				latch.Rule = identity
				latch.Reset = w.ResetsAt
			}
			latch.Current = known(r.Profile)
			text := fmt.Sprintf("%s reached %g%% (%s)", r.Meter, r.AtLeast, r.Name)
			if latched {
				text = fmt.Sprintf("%s held until reset and a fresh reading below %g%%", r.Name, r.AtLeast)
			}
			return latch.Current, UsageProfileReason{Text: text, Rule: r.Name, Meter: r.Meter, UsedPercent: w.UsedPercent, Threshold: r.AtLeast, Reset: latch.Reset}, latch
		}
		if !ok {
			missing = true
		}
	}
	if missing {
		return latch.Current, UsageProfileReason{Text: "Usage data is unavailable or stale; keeping current defaults"}, latch
	}
	latch.Current = known(fallback)
	return latch.Current, UsageProfileReason{Text: "No usage rule matches; using fallback defaults"}, latch
}
