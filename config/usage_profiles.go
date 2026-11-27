package config

import (
	"fmt"
	"math"
	"strings"

	"gopkg.in/yaml.v3"
)

// UsageProfile selects defaults for new work by adapter tier, never raw model.
type UsageProfile struct {
	Name     string                `yaml:"name" json:"name"`
	Agent    string                `yaml:"agent" json:"agent"`
	Tier     string                `yaml:"tier,omitempty" json:"tier,omitempty"`
	Account  string                `yaml:"account,omitempty" json:"account,omitempty"`
	Reviewer *UsageProfileReviewer `yaml:"reviewer,omitempty" json:"reviewer,omitempty"`
}

// UsageProfileReviewer overrides the default reviewer for a new review pass.
type UsageProfileReviewer struct {
	Agent   string `yaml:"agent" json:"agent"`
	Tier    string `yaml:"tier,omitempty" json:"tier,omitempty"`
	Account string `yaml:"account,omitempty" json:"account,omitempty"`
}

// ValidateUsageProfiles checks usage policy against the configured adapter tiers.
func ValidateUsageProfiles(cfg *Config) error {
	if err := ValidateClaudeAccounts(cfg); err != nil {
		return err
	}
	names := map[string]bool{}
	for i := range cfg.Usage.Profiles {
		p := &cfg.Usage.Profiles[i]
		path := fmt.Sprintf("usage.profiles[%d]", i)
		p.Name = strings.TrimSpace(p.Name)
		if err := usageName(names, p.Name, path); err != nil {
			return err
		}
		p.Agent, p.Tier = strings.TrimSpace(p.Agent), strings.TrimSpace(p.Tier)
		if err := usageAgent(cfg, p.Agent, p.Tier, path); err != nil {
			return err
		}
		if err := cfg.ValidateClaudeSelection(p.Agent, p.Account); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if p.Reviewer != nil {
			p.Reviewer.Agent, p.Reviewer.Tier = strings.TrimSpace(p.Reviewer.Agent), strings.TrimSpace(p.Reviewer.Tier)
			if err := usageAgent(cfg, p.Reviewer.Agent, p.Reviewer.Tier, path+".reviewer"); err != nil {
				return err
			}
			if err := cfg.ValidateClaudeSelection(p.Reviewer.Agent, p.Reviewer.Account); err != nil {
				return fmt.Errorf("%s.reviewer: %w", path, err)
			}
		}
	}
	cfg.Usage.FallbackProfile = strings.TrimSpace(cfg.Usage.FallbackProfile)
	if cfg.Usage.FallbackProfile != "" && !names[strings.ToLower(cfg.Usage.FallbackProfile)] {
		return fmt.Errorf("usage.fallback_profile %q is not a usage profile", cfg.Usage.FallbackProfile)
	}
	ruleNames := map[string]bool{}
	for i := range cfg.Usage.Rules {
		r := &cfg.Usage.Rules[i]
		path := fmt.Sprintf("usage.rules[%d]", i)
		r.Name, r.Meter, r.Profile = strings.TrimSpace(r.Name), strings.TrimSpace(r.Meter), strings.TrimSpace(r.Profile)
		if err := usageName(ruleNames, r.Name, path); err != nil {
			return err
		}
		if r.Order == nil {
			return fmt.Errorf("%s.order must be explicit", path)
		}
		if !cfg.IsUsageMeter(r.Meter) {
			return fmt.Errorf("%s.meter %q is not a qualified usage meter", path, r.Meter)
		}
		if math.IsNaN(r.AtLeast) || r.AtLeast < 1 || r.AtLeast > 100 {
			return fmt.Errorf("%s.at_least must be between 1 and 100", path)
		}
		if !names[strings.ToLower(r.Profile)] {
			return fmt.Errorf("%s.profile %q is not a usage profile", path, r.Profile)
		}
	}
	return nil
}

// IsUsageMeter reports whether meter is one of the qualified subscription
// meters the providers declare (UsageQualifiedMeters).
func IsUsageMeter(meter string) bool {
	_, account, _, ok := ParseUsageMeter(meter)
	return ok && account == ""
}

func usageName(seen map[string]bool, name, path string) error {
	if name == "" {
		return fmt.Errorf("%s.name must not be empty", path)
	}
	key := strings.ToLower(name)
	if seen[key] {
		return fmt.Errorf("%s.name %q is repeated (names ignore case)", path, name)
	}
	seen[key] = true
	return nil
}

func usageAgent(cfg *Config, agent, tier, path string) error {
	switch agent {
	case "claude-code", "codex", "chimera":
	default:
		return fmt.Errorf("%s.agent %q must be claude-code, codex, or chimera", path, agent)
	}
	if tier != "" {
		if _, ok := cfg.Adapters[agent].Tiers[tier]; !ok {
			return fmt.Errorf("%s.tier %q is not configured for adapter %q", path, tier, agent)
		}
	}
	return nil
}

// UnmarshalYAML refuses raw models even though the general config parser
// permits unknown fields. Entry overlays also decode through this method.
func (p *UsageProfile) UnmarshalYAML(node *yaml.Node) error {
	if err := rejectUsageModel(node, "usage.profiles"); err != nil {
		return err
	}
	type plain UsageProfile
	return node.Decode((*plain)(p))
}

func (r *UsageProfileReviewer) UnmarshalYAML(node *yaml.Node) error {
	if err := rejectUsageModel(node, "usage.profiles.reviewer"); err != nil {
		return err
	}
	type plain UsageProfileReviewer
	return node.Decode((*plain)(r))
}

func rejectUsageModel(node *yaml.Node, path string) error {
	var fields map[string]any
	if err := node.Decode(&fields); err != nil {
		return err
	}
	if _, ok := fields["model"]; ok {
		return fmt.Errorf("%s.model is not allowed; use a configured tier", path)
	}
	return nil
}

// UsageRule selects a profile when its qualified subscription meter reaches
// AtLeast percent. Lower Order wins; names break ties.
type UsageRule struct {
	Name    string  `yaml:"name" json:"name"`
	Order   *int    `yaml:"order" json:"order"`
	Meter   string  `yaml:"meter" json:"meter"`
	AtLeast float64 `yaml:"at_least" json:"at_least"`
	Profile string  `yaml:"profile" json:"profile"`
}
