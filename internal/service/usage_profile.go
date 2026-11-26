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
}

type UsagePolicySnapshot struct {
	Active   UsageProfileState          `json:"active"`
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
	return UsagePolicySnapshot{Active: state, Profiles: cfg.Profiles, Switches: history}, err
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
	next := before
	now := s.opts.Now().UTC()
	if changePin {
		if request != nil {
			pin := *request
			profile, ok := usageProfileNamed(cfg.Profiles, pin.Profile)
			if !ok || pin.Profile == "" {
				return fmt.Errorf("unknown usage profile %q", pin.Profile)
			}
			pin.Profile = profile.Name
			if pin.Until != nil && pin.UntilReset != "" {
				return errors.New("choose until or until_reset, not both")
			}
			if pin.UntilReset != "" {
				if !config.IsUsageMeter(pin.UntilReset) {
					return fmt.Errorf("unknown usage meter %q", pin.UntilReset)
				}
				w, _, ok := usageMeter(snap.Providers, pin.UntilReset)
				if !ok || !w.ResetsAt.After(now) {
					return errors.New("that meter has no fresh future reset")
				}
				reset := w.ResetsAt.UTC()
				pin.Until = &reset
			}
			if pin.Until != nil && !pin.Until.After(now) {
				return errors.New("pin expiry must be in the future")
			}
			next.Pin = &pin
		} else {
			next.Pin = nil
		}
	}
	if next.Pin != nil && next.Pin.Until != nil && !now.Before(*next.Pin.Until) {
		next.Pin = nil
	}
	next.Latch.Current = before.Profile.Name
	profile, reason, latch := EvaluateUsageRules(cfg.Rules, cfg.Profiles, cfg.FallbackProfile, snap.Providers, next.Latch, now)
	next.Latch = latch
	if next.Pin != nil {
		profile = next.Pin.Profile
		reason = UsageProfileReason{Text: "Pinned by the operator"}
	}
	selected, ok := usageProfileNamed(cfg.Profiles, profile)
	if !ok {
		return fmt.Errorf("active usage profile %q no longer exists", profile)
	}
	next.Error = ""
	if s.opts.Validate != nil {
		if err := s.opts.Validate(selected); err != nil {
			next.Profile = before.Profile
			next.Reason = before.Reason
			next.Latch = before.Latch
			next.Error = err.Error()
			if changePin {
				return err
			}
			data, _ := json.Marshal(next)
			if err := s.opts.Store.SaveUsageProfile(ctx, string(data), nil); err != nil {
				return err
			}
			if s.opts.OnChange != nil {
				s.opts.OnChange()
			}
			return nil
		}
	}
	next.Profile = selected
	next.Reason = reason
	previousProfile := before.Profile
	if !before.Initialized {
		previousProfile = next.Profile
		// Seeding fallback is a baseline, but a startup rule or operator pin
		// still changes defaults and must have history and notification.
		if reason.Rule != "" || next.Pin != nil {
			previousProfile, _ = usageProfileNamed(cfg.Profiles, cfg.FallbackProfile)
		}
	}
	oldDefaults, _ := json.Marshal(previousProfile)
	newDefaults, _ := json.Marshal(next.Profile)
	switched := string(oldDefaults) != string(newDefaults)
	if !before.Initialized || switched {
		next.Since = now
	}
	next.Initialized = true
	var change *store.UsageProfileSwitch
	if switched {
		change = &store.UsageProfileSwitch{From: previousProfile.Name, To: next.Profile.Name, Since: now.Format(time.RFC3339Nano), Reason: reason.Text}
	}
	data, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := s.opts.Store.SaveUsageProfile(ctx, string(data), change); err != nil {
		return err
	}
	if switched && s.opts.OnSwitch != nil {
		s.opts.OnSwitch(ctx, next, change.ID)
	}
	beforeData, _ := json.Marshal(before)
	if string(beforeData) != string(data) && s.opts.OnChange != nil {
		s.opts.OnChange()
	}
	return nil
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
