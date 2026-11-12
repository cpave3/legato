package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/cpave3/legato/config"
)

// Making one of the Reeve's own rules stricter needs no signature: a
// signature guards against a change that lets the Reeve do more without
// the person, and this only ever makes it ask more. Only the levels and
// rules that decide what a change needs, and host operations, are judged
// this way; anything else needs a signature as before.

// levelSpan is the least and the most a class level can mean: risky's
// default is signed once a device is enrolled, else approve.
func levelSpan(key, value string) (lo, hi ReeveLevel, ok bool) {
	v := strings.TrimSpace(value)
	switch {
	case key == "reeve.levels.risky" && (v == "" || v == "default"):
		return LevelApprove, LevelSigned, true
	case key == "reeve.levels.change" && v == "":
		return LevelApprove, LevelApprove, true
	}
	switch l := ReeveLevel(v); l {
	case LevelAuto, LevelApprove, LevelSigned, LevelVerified:
		return l, l, true
	}
	return "", "", false
}

// tightensSetting reports whether setting key to raw (or resetting it)
// only makes the Reeve ask more.
func (s *SettingsService) tightensSetting(ctx context.Context, key string, raw json.RawMessage, reset bool) bool {
	setting, ok := config.LookupSetting(key)
	if !ok || !setting.OwnRule {
		return false
	}
	current, err := s.Get(ctx, key)
	if err != nil {
		return false
	}
	next := any(nil)
	if reset {
		next, err = config.SettingValue(config.Defaults(), key)
	} else {
		next, err = config.DecodeSetting(setting, raw)
	}
	if err != nil {
		return false
	}
	switch key {
	case "reeve.levels.change", "reeve.levels.risky":
		old, _ := current.Value.(string)
		now, _ := next.(string)
		_, oldHi, ok1 := levelSpan(key, old)
		newLo, _, ok2 := levelSpan(key, now)
		return ok1 && ok2 && levelRank[newLo] >= levelRank[oldHi]
	case "reeve.host_operations_auto", "reeve.host_operations":
		on, isBool := next.(bool)
		return isBool && !on
	}
	return false
}

// tightensRule reports whether saving a rule (raw), or removing it (nil),
// only makes the Reeve ask more: the rule's level raised and its pattern
// kept, or an auto rule removed (its calls then need their class's level,
// which is never below auto).
func (s *SettingsService) tightensRule(ctx context.Context, list, name string, raw json.RawMessage) bool {
	if list != "reeve.rules" {
		return false
	}
	cfg, err := s.Effective(ctx)
	if err != nil {
		return false
	}
	name = strings.TrimSpace(name)
	var old *config.ReeveRule
	for i, r := range cfg.Reeve.Rules {
		if strings.TrimSpace(r.Pattern) == name {
			old = &cfg.Reeve.Rules[i]
		}
	}
	if old == nil {
		return false
	}
	oldLevel := ReeveLevel(strings.TrimSpace(old.Level))
	if _, known := levelRank[oldLevel]; !known || oldLevel == LevelFree {
		return false
	}
	if raw == nil {
		return oldLevel == LevelAuto
	}
	var next config.ReeveRule
	if json.Unmarshal(raw, &next) != nil {
		return false
	}
	if p := strings.TrimSpace(next.Pattern); p != "" && p != name {
		return false
	}
	newLevel := ReeveLevel(strings.TrimSpace(next.Level))
	if _, known := levelRank[newLevel]; !known || newLevel == LevelFree {
		return false
	}
	return levelRank[newLevel] >= levelRank[oldLevel]
}

// AutoStatus is what runs without the person: the class levels, the host
// auto setting, the rules at auto, what config.yaml pins of them (the off
// switch cannot change those), and the last sets that ran at auto.
type AutoStatus struct {
	On       bool      `json:"on"`
	Change   string    `json:"change"`
	Risky    string    `json:"risky"`
	HostAuto bool      `json:"host_auto"`
	Rules    []string  `json:"rules"`
	Pinned   []string  `json:"pinned"`
	Recent   []AutoRun `json:"recent"`
}

// AutoRun is one change set that ran at the auto level.
type AutoRun struct {
	ID         string `json:"id"`
	ApprovalID string `json:"approval_id"`
	TaskID     string `json:"task_id"`
	Reason     string `json:"reason"`
	State      string `json:"state"`
	By         string `json:"by"`
	At         string `json:"at"`
}

// recentAutoRuns is how many sets AutoStatus lists.
const recentAutoRuns = 20

// Auto says what runs without the person now.
func (r *ReeveRules) Auto(ctx context.Context) (AutoStatus, error) {
	cfg, err := r.Settings.Effective(ctx)
	if err != nil {
		return AutoStatus{}, err
	}
	st := AutoStatus{Change: cfg.Reeve.Levels.Change, Risky: cfg.Reeve.Levels.Risky, HostAuto: cfg.Reeve.HostOperationsAutoOn(), Rules: []string{}, Pinned: []string{}, Recent: []AutoRun{}}
	for _, rule := range cfg.Reeve.Rules {
		if ReeveLevel(strings.TrimSpace(rule.Level)) == LevelAuto {
			st.Rules = append(st.Rules, rule.Pattern)
			if cfg.PinnedEntry("reeve.rules", rule.Pattern) {
				st.Pinned = append(st.Pinned, "reeve.rules "+rule.Pattern)
			}
		}
	}
	for _, key := range autoKeys(cfg) {
		if cfg.Pinned(key) {
			st.Pinned = append(st.Pinned, key)
		}
	}
	st.On = len(autoKeys(cfg)) > 0 || len(st.Rules) > 0
	sets, err := r.Store.ListAutoReeveChangeSets(ctx, recentAutoRuns)
	if err != nil {
		return st, err
	}
	for _, set := range sets {
		run := AutoRun{ID: set.ID, ApprovalID: set.ApprovalID, TaskID: set.TaskID, Reason: set.Reason, State: set.State, At: set.CreatedAt}
		if approval, err := r.Approvals.Get(ctx, set.ApprovalID); err == nil && approval.Decision != nil {
			run.By = approval.Decision.By
		}
		st.Recent = append(st.Recent, run)
	}
	return st, nil
}

// autoKeys are the settings now at auto.
func autoKeys(cfg *config.Config) []string {
	var keys []string
	if strings.TrimSpace(cfg.Reeve.Levels.Change) == string(LevelAuto) {
		keys = append(keys, "reeve.levels.change")
	}
	if strings.TrimSpace(cfg.Reeve.Levels.Risky) == string(LevelAuto) {
		keys = append(keys, "reeve.levels.risky")
	}
	if cfg.Reeve.HostOperationsAutoOn() {
		keys = append(keys, "reeve.host_operations_auto")
	}
	return keys
}

// AutoOffResult is what the off switch changed, and what config.yaml pins
// so that it could not.
type AutoOffResult struct {
	Changed []string `json:"changed"`
	Pinned  []string `json:"pinned"`
}

// AutoOff is the off switch: every class level at auto back to approve,
// host auto off, and every rule at auto back to approve, at once and with
// no signature (it only makes the Reeve ask more). A value config.yaml
// pins is left as it is and reported.
func (r *ReeveRules) AutoOff(ctx context.Context, by string) (AutoOffResult, error) {
	cfg, err := r.Settings.Effective(ctx)
	if err != nil {
		return AutoOffResult{}, err
	}
	out := AutoOffResult{Changed: []string{}, Pinned: []string{}}
	var errs []error
	keep := func(what string, err error) {
		switch {
		case err == nil:
			out.Changed = append(out.Changed, what)
		case errors.Is(err, ErrSettingPinned), errors.Is(err, config.ErrEntryPinned):
			out.Pinned = append(out.Pinned, what)
		default:
			errs = append(errs, err)
		}
	}
	for _, key := range autoKeys(cfg) {
		value := json.RawMessage(`"approve"`)
		if key == "reeve.host_operations_auto" {
			value = json.RawMessage(`false`)
		}
		keep(key, r.Settings.Set(ctx, key, value, by))
	}
	for _, rule := range cfg.Reeve.Rules {
		if ReeveLevel(strings.TrimSpace(rule.Level)) != LevelAuto {
			continue
		}
		raw, _ := json.Marshal(map[string]string{"pattern": rule.Pattern, "level": string(LevelApprove)})
		keep("reeve.rules "+rule.Pattern, r.Settings.SetEntry(ctx, "reeve.rules", rule.Pattern, raw, by))
	}
	return out, errors.Join(errs...)
}
