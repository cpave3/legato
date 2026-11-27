package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

type UsagePinRequest struct {
	Profile    string     `json:"profile"`
	Until      *time.Time `json:"until,omitempty"`
	UntilReset string     `json:"until_reset,omitempty"`
}

type UsageProfileState struct {
	Profile     config.UsageProfile `json:"profile"`
	Reason      UsageProfileReason  `json:"reason"`
	Since       time.Time           `json:"since"`
	Latch       UsageRuleLatch      `json:"latch"`
	Pin         *UsagePinRequest    `json:"pin,omitempty"`
	Initialized bool                `json:"initialized"`
	Error       string              `json:"error,omitempty"`
	// Baseline is a repository scope's default profile; empty for the
	// global scope, whose baseline is usage.fallback_profile.
	Baseline string `json:"baseline,omitempty"`
	// Scopes, on the global state only, are the repository scopes by
	// lower-cased baseline. The global pin applies to each.
	Scopes map[string]UsageProfileState `json:"scopes,omitempty"`
}

type UsagePolicySnapshot struct {
	Active UsageProfileState `json:"active"`
	// Scopes are the repository scopes: the current defaults for new work
	// in the repositories whose default is Baseline (not the fallback).
	Scopes   []UsagePolicyScope         `json:"scopes"`
	Profiles []config.UsageProfile      `json:"profiles"`
	Switches []store.UsageProfileSwitch `json:"switches"`
}

type UsagePolicyOptions struct {
	Store    *store.Store
	Config   func(context.Context) (config.UsageConfig, error)
	Readings func(context.Context) (SubscriptionUsage, error)
	Now      func() time.Time
	Validate func(config.UsageProfile) error
	OnSwitch func(context.Context, UsageProfileState, int64)
	OnChange func()
}

// UsagePolicyService serializes policy transitions. The runtime must hold
// usage.lock before Evaluate or Pin; snapshots and defaults are read-only.
type UsagePolicyService struct {
	opts UsagePolicyOptions
	mu   sync.Mutex
}

func NewUsagePolicyService(opts UsagePolicyOptions) *UsagePolicyService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &UsagePolicyService{opts: opts}
}

func (s *UsagePolicyService) state(ctx context.Context) (UsageProfileState, error) {
	text, err := s.opts.Store.ActiveUsageProfile(ctx)
	if err != nil {
		return UsageProfileState{}, err
	}
	var state UsageProfileState
	if text != "" {
		if err = json.Unmarshal([]byte(text), &state); err != nil {
			return state, fmt.Errorf("read active usage profile: %w", err)
		}
	}
	return state, nil
}

func (s *UsagePolicyService) Snapshot(ctx context.Context) (UsagePolicySnapshot, error) {
	cfg, err := s.opts.Config(ctx)
	if err != nil {
		return UsagePolicySnapshot{}, err
	}
	state, err := s.state(ctx)
	if err != nil {
		return UsagePolicySnapshot{}, err
	}
	history, err := s.opts.Store.UsageProfileSwitches(ctx, 20)
	scopes := usagePolicyScopes(cfg, state)
	state.Scopes = nil
	return UsagePolicySnapshot{Active: state, Scopes: scopes, Profiles: cfg.Profiles, Switches: history}, err
}

func (s *UsagePolicyService) Evaluate(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.evaluate(ctx, nil, false)
}

// Pin sets or removes a pin and evaluates in the same serialized operation.
func (s *UsagePolicyService) Pin(ctx context.Context, request *UsagePinRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.evaluate(ctx, request, true)
}

func usageProfileNamed(profiles []config.UsageProfile, name string) (config.UsageProfile, bool) {
	for _, p := range profiles {
		if strings.EqualFold(p.Name, name) {
			return p, true
		}
	}
	return config.UsageProfile{}, name == ""
}

func (s *UsagePolicyService) evaluate(ctx context.Context, request *UsagePinRequest, changePin bool) error {
	cfg, err := s.opts.Config(ctx)
	if err != nil {
		return err
	}
	snap, err := s.opts.Readings(ctx)
	if err != nil {
		return err
	}
	before, err := s.state(ctx)
	if err != nil {
		return err
	}
	now := s.opts.Now().UTC()
	pin := before.Pin
	if changePin {
		if request != nil {
			next := *request
			profile, ok := usageProfileNamed(cfg.Profiles, next.Profile)
			if !ok || next.Profile == "" {
				return fmt.Errorf("unknown usage profile %q", next.Profile)
			}
			next.Profile = profile.Name
			if next.Until != nil && next.UntilReset != "" {
				return errors.New("choose until or until_reset, not both")
			}
			if next.UntilReset != "" {
				if _, _, _, valid := config.ParseUsageMeter(next.UntilReset); !valid {
					return fmt.Errorf("unknown usage meter %q", next.UntilReset)
				}
				w, _, ok := usageMeter(snap.Providers, next.UntilReset)
				if !ok || !w.ResetsAt.After(now) {
					return errors.New("that meter has no fresh future reset")
				}
				reset := w.ResetsAt.UTC()
				next.Until = &reset
			}
			if next.Until != nil && !next.Until.After(now) {
				return errors.New("pin expiry must be in the future")
			}
			pin = &next
		} else {
			pin = nil
		}
	}
	if pin != nil && pin.Until != nil && !now.Before(*pin.Until) {
		pin = nil
	}

	global := before
	global.Scopes = nil
	next, change, err := s.evaluateScope(cfg, snap, global, pin, cfg.FallbackProfile, "", now)
	if err != nil {
		var invalid usageValidationError
		if changePin || !errors.As(err, &invalid) {
			return err
		}
		next = global
		next.Error = err.Error()
	}
	next.Pin = pin
	changes := []*store.UsageProfileSwitch{}
	switched := []UsageProfileState{}
	if change != nil {
		changes = append(changes, change)
		switched = append(switched, next)
	}
	for _, baseline := range usageScopeBaselines(cfg) {
		key := strings.ToLower(baseline)
		prior, ok := before.Scopes[key]
		if !ok {
			prior = UsageProfileState{}
		}
		scope, change, err := s.evaluateScope(cfg, snap, prior, pin, baseline, key, now)
		if err != nil {
			scope = prior
			scope.Baseline = baseline
			if !scope.Initialized {
				// Never evaluated: keep the baseline, as readers already do.
				scope.Profile, _ = usageProfileNamed(cfg.Profiles, baseline)
				scope.Latch.Current = scope.Profile.Name
			}
			scope.Pin = nil
			scope.Error = err.Error()
		}
		if next.Scopes == nil {
			next.Scopes = map[string]UsageProfileState{}
		}
		next.Scopes[key] = scope
		if change != nil {
			changes = append(changes, change)
			switched = append(switched, scope)
		}
	}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := s.opts.Store.SaveUsageProfileChanges(ctx, string(data), changes); err != nil {
		return err
	}
	if s.opts.OnSwitch != nil {
		for i, change := range changes {
			s.opts.OnSwitch(ctx, switched[i], change.ID)
		}
	}
	beforeData, _ := json.Marshal(before)
	if string(beforeData) != string(data) && s.opts.OnChange != nil {
		s.opts.OnChange()
	}
	return nil
}

// usageValidationError is a selected profile that cannot launch here: the
// scope keeps its previous state and shows the error.
type usageValidationError struct{ error }

func (e usageValidationError) Unwrap() error { return e.error }

// usageScopeBaselines are the repository defaults' profiles that are not
// the fallback, once each, in configuration order. Each is a scope with its
// own latch; repositories that default to the fallback share the global one.
func usageScopeBaselines(cfg config.UsageConfig) []string {
	seen := map[string]bool{strings.ToLower(cfg.FallbackProfile): true}
	out := []string{}
	for _, d := range cfg.Repositories {
		key := strings.ToLower(d.Profile)
		if seen[key] {
			continue
		}
		seen[key] = true
		if p, ok := usageProfileNamed(cfg.Profiles, d.Profile); ok && p.Name != "" {
			out = append(out, p.Name)
		}
	}
	return out
}

// usageRulesFor are the rules new work with this baseline obeys: those with
// no when_default, and those whose when_default names it.
func usageRulesFor(rules []config.UsageRule, baseline string) []config.UsageRule {
	out := []config.UsageRule{}
	for _, r := range rules {
		eligible := len(r.WhenDefault) == 0
		for _, name := range r.WhenDefault {
			if strings.EqualFold(name, baseline) {
				eligible = true
			}
		}
		if eligible {
			out = append(out, r)
		}
	}
	return out
}

// evaluateScope evaluates one baseline's rules and the pin against its
// stored state. A validation failure is returned as an error, and the
// caller keeps the scope's previous state.
func (s *UsagePolicyService) evaluateScope(cfg config.UsageConfig, snap SubscriptionUsage, before UsageProfileState, pin *UsagePinRequest, baseline, scope string, now time.Time) (UsageProfileState, *store.UsageProfileSwitch, error) {
	next := before
	next.Scopes = nil
	next.Pin = nil
	next.Latch.Current = before.Profile.Name
	if !before.Initialized && scope != "" {
		next.Latch.Current = baseline
	}
	profile, reason, latch := EvaluateUsageRules(usageRulesFor(cfg.Rules, baseline), cfg.Profiles, baseline, snap.Providers, next.Latch, now)
	next.Latch = latch
	if pin != nil {
		profile = pin.Profile
		reason = UsageProfileReason{Text: "Pinned by the operator"}
	}
	selected, ok := usageProfileNamed(cfg.Profiles, profile)
	if !ok {
		return before, nil, fmt.Errorf("active usage profile %q no longer exists", profile)
	}
	next.Error = ""
	if s.opts.Validate != nil {
		if err := s.opts.Validate(selected); err != nil {
			return before, nil, usageValidationError{err}
		}
	}
	if scope != "" {
		next.Baseline = baseline
	}
	next.Profile = selected
	next.Reason = reason
	previousProfile := before.Profile
	if !before.Initialized {
		previousProfile = next.Profile
		// Seeding the baseline is silent, but a startup rule or operator
		// pin still changes defaults and must have history and notification.
		if reason.Rule != "" || pin != nil {
			previousProfile, _ = usageProfileNamed(cfg.Profiles, baseline)
		}
	}
	oldDefaults, _ := json.Marshal(previousProfile)
	newDefaults, _ := json.Marshal(next.Profile)
	switched := string(oldDefaults) != string(newDefaults)
	if !before.Initialized || switched {
		next.Since = now
	}
	next.Initialized = true
	if !switched {
		return next, nil, nil
	}
	switchScope := ""
	if scope != "" {
		switchScope = baseline
	}
	return next, &store.UsageProfileSwitch{Scope: switchScope, From: previousProfile.Name, To: next.Profile.Name, Since: now.Format(time.RFC3339Nano), Reason: reason.Text}, nil
}

// UsageDefaults reads the committed profile; it never evaluates policy or
// reads a subscription. Existing work must retain its saved agent identity.
func UsageDefaults(ctx context.Context, db *store.Store) (config.UsageProfile, error) {
	text, err := db.ActiveUsageProfile(ctx)
	if err != nil {
		return config.UsageProfile{}, err
	}
	var state UsageProfileState
	if text != "" {
		if err = json.Unmarshal([]byte(text), &state); err != nil {
			return config.UsageProfile{}, err
		}
	}
	return state.Profile, nil
}

// UsagePolicyScope is one repository scope as Settings and the Usage card
// show it.
type UsagePolicyScope struct {
	Key          string            `json:"key"`
	Baseline     string            `json:"baseline"`
	Repositories []string          `json:"repositories"`
	Active       UsageProfileState `json:"active"`
}

func usagePolicyScopes(cfg config.UsageConfig, global UsageProfileState) []UsagePolicyScope {
	out := []UsagePolicyScope{}
	for _, baseline := range usageScopeBaselines(cfg) {
		key := strings.ToLower(baseline)
		repos := []string{}
		for _, d := range cfg.Repositories {
			if strings.EqualFold(d.Profile, baseline) {
				repos = append(repos, d.Repository)
			}
		}
		out = append(out, UsagePolicyScope{Key: key, Baseline: baseline, Repositories: repos, Active: usageScopeState(cfg, global, baseline)})
	}
	return out
}

// usageScopeState is a scope's stored state, or, before its first
// evaluation, its baseline (under the pin, if one is set).
func usageScopeState(cfg config.UsageConfig, global UsageProfileState, baseline string) UsageProfileState {
	if state, ok := global.Scopes[strings.ToLower(baseline)]; ok && state.Initialized {
		return state
	}
	if global.Pin != nil {
		state := global
		state.Scopes = nil
		state.Pin = nil
		state.Baseline = baseline
		return state
	}
	profile, _ := usageProfileNamed(cfg.Profiles, baseline)
	return UsageProfileState{Profile: profile, Baseline: baseline, Reason: UsageProfileReason{Text: "Repository default"}, Latch: UsageRuleLatch{Current: profile.Name}}
}

// Where the current defaults for new work came from.
const (
	UsageSourcePin        = "pin"
	UsageSourceRule       = "rule"
	UsageSourceHeld       = "held"
	UsageSourceRepository = "repository"
	UsageSourceOwner      = "owner"
	UsageSourceFallback   = "fallback"
)

// UsageSelection is the current defaults for new work in one repository:
// the profile, the repository default it starts from (Baseline, with the
// entry it Matched), and why (Source, Reason). It describes new work only,
// never an agent already running.
type UsageSelection struct {
	Repository string              `json:"repository,omitempty"`
	Profile    config.UsageProfile `json:"profile"`
	Baseline   string              `json:"baseline"`
	Source     string              `json:"source"`
	Matched    string              `json:"matched,omitempty"`
	Reason     UsageProfileReason  `json:"reason"`
	Since      time.Time           `json:"since,omitzero"`
	Error      string              `json:"error,omitempty"`
}

// DefaultsFor reads the committed defaults for new work in a repository
// under the live configuration.
func (s *UsagePolicyService) DefaultsFor(ctx context.Context, repo string) (UsageSelection, error) {
	cfg, err := s.opts.Config(ctx)
	if err != nil {
		return UsageSelection{}, err
	}
	return UsageDefaultsFor(ctx, s.opts.Store, cfg, repo)
}

// UsageDefaultsFor reads the committed defaults for new work in a
// repository (owner/name, host/owner/name or a remote address; empty for
// none). It never evaluates policy or reads a subscription.
func UsageDefaultsFor(ctx context.Context, db *store.Store, cfg config.UsageConfig, repo string) (UsageSelection, error) {
	text, err := db.ActiveUsageProfile(ctx)
	if err != nil {
		return UsageSelection{}, err
	}
	var global UsageProfileState
	if text != "" {
		if err = json.Unmarshal([]byte(text), &global); err != nil {
			return UsageSelection{}, err
		}
	}
	sel := UsageSelection{Repository: repo, Baseline: cfg.FallbackProfile, Source: UsageSourceFallback}
	if fallback, ok := usageProfileNamed(cfg.Profiles, cfg.FallbackProfile); ok {
		sel.Baseline = fallback.Name
	}
	state := global
	if d, kind, ok := cfg.RepositoryDefault(repo); ok {
		sel.Baseline, sel.Matched = d.Profile, d.Repository
		sel.Source = UsageSourceRepository
		if kind == config.UsageRepositoryOwner {
			sel.Source = UsageSourceOwner
		}
		if !strings.EqualFold(d.Profile, cfg.FallbackProfile) {
			state = usageScopeState(cfg, global, d.Profile)
		}
	}
	sel.Profile, sel.Reason, sel.Since, sel.Error = state.Profile, state.Reason, state.Since, state.Error
	switch {
	case global.Pin != nil:
		sel.Source = UsageSourcePin
	case state.Reason.Rule != "":
		sel.Source = UsageSourceRule
	case !strings.EqualFold(state.Profile.Name, sel.Baseline):
		sel.Source = UsageSourceHeld
	}
	return sel, nil
}
