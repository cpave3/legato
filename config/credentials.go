package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// SecretsConfig is how Legato keeps the secrets it stores itself.
type SecretsConfig struct {
	// MasterKey is where the key of Legato's own store lives: auto (the
	// default), systemd-creds, secret-service, keychain, passphrase or file.
	MasterKey   string            `yaml:"master_key"`
	OnePassword OnePasswordConfig `yaml:"onepassword"`
}

// OnePasswordConfig reads op:// references as a service account.
type OnePasswordConfig struct {
	Token string `yaml:"token"`
}

// CredentialConfig is one credential agents can ask for.
type CredentialConfig struct {
	Name    string `yaml:"name"`
	Secret  string `yaml:"secret"`
	Profile string `yaml:"profile"`
	// Type of the secret, when the profile does not say.
	Type   string           `yaml:"type"`
	Policy []CredentialRule `yaml:"policy"`
}

// CredentialRule says how often the person is asked for some uses.
type CredentialRule struct {
	Operation StringList        `yaml:"operation" json:"operation,omitempty"`
	Resource  string            `yaml:"resource" json:"resource,omitempty"`
	Approve   string            `yaml:"approve" json:"approve"`
	Window    string            `yaml:"window" json:"window,omitempty"`
	Limit     map[string]string `yaml:"limit" json:"limit,omitempty"`
	Signed    bool              `yaml:"signed" json:"signed,omitempty"`

	// Read from the fields above when the configuration loads.
	WindowDuration time.Duration `yaml:"-" json:"-"`
	Limits         []RuleLimit   `yaml:"-" json:"-"`
}

// RuleLimit is "count per period" of one meter: requests: 200/day.
type RuleLimit struct {
	Meter  string
	Count  int64
	Period time.Duration
}

// StringList is a YAML string or list of strings.
type StringList []string

func (s *StringList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*s = StringList{node.Value}
		return nil
	}
	var list []string
	if err := node.Decode(&list); err != nil {
		return err
	}
	*s = list
	return nil
}

// ApproveModes are the ways a rule can decide.
var ApproveModes = []string{"auto", "session", "window", "request", "deny"}

var (
	secretSchemes  = []string{"legato", "op", "systemd-creds", "keychain", "env", "file"}
	masterKeys     = []string{"auto", "systemd-creds", "secret-service", "keychain", "passphrase", "file"}
	credentialName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	limitPattern   = regexp.MustCompile(`^\s*(\d+)\s*/\s*(minute|hour|day|week)\s*$`)
)

var limitPeriods = map[string]time.Duration{"minute": time.Minute, "hour": time.Hour, "day": 24 * time.Hour, "week": 7 * 24 * time.Hour}

func normalizeCredentials(cfg *Config) error {
	cfg.Secrets.MasterKey = strings.TrimSpace(cfg.Secrets.MasterKey)
	if cfg.Secrets.MasterKey == "" {
		cfg.Secrets.MasterKey = "auto"
	}
	if !oneOf(cfg.Secrets.MasterKey, masterKeys) {
		return fmt.Errorf("secrets.master_key %q: want one of %s", cfg.Secrets.MasterKey, strings.Join(masterKeys, ", "))
	}
	seen := map[string]bool{}
	for i := range cfg.Credentials {
		c := &cfg.Credentials[i]
		c.Name = strings.TrimSpace(c.Name)
		if !credentialName.MatchString(c.Name) {
			return fmt.Errorf("credentials[%d]: name %q: want letters, digits, dots, dashes and underscores", i, c.Name)
		}
		key := strings.ToLower(c.Name)
		if seen[key] {
			return fmt.Errorf("credentials %q: the name is used twice", c.Name)
		}
		seen[key] = true
		c.Secret = strings.TrimSpace(c.Secret)
		scheme, path, ok := strings.Cut(c.Secret, "://")
		if !ok || path == "" || !oneOf(scheme, secretSchemes) {
			return fmt.Errorf("credentials %q: secret %q: want <store>://<path> with a store of %s", c.Name, c.Secret, strings.Join(secretSchemes, ", "))
		}
		if c.Profile = strings.TrimSpace(c.Profile); c.Profile == "" {
			c.Profile = "secret"
		}
		for j := range c.Policy {
			if err := NormalizeCredentialRule(&c.Policy[j]); err != nil {
				return fmt.Errorf("credentials %q: policy[%d]: %w", c.Name, j, err)
			}
		}
	}
	return nil
}

// NormalizeCredentialRule checks a rule and reads its window and limits.
func NormalizeCredentialRule(r *CredentialRule) error {
	r.Approve = strings.TrimSpace(r.Approve)
	if !oneOf(r.Approve, ApproveModes) {
		return fmt.Errorf("approve %q: want one of %s", r.Approve, strings.Join(ApproveModes, ", "))
	}
	for i, op := range r.Operation {
		if r.Operation[i] = strings.TrimSpace(op); r.Operation[i] == "" {
			return fmt.Errorf("an operation is blank")
		}
	}
	if r.Approve == "window" || r.Window != "" {
		d, err := time.ParseDuration(strings.TrimSpace(r.Window))
		if err != nil || d <= 0 {
			return fmt.Errorf("window %q: a window rule needs a duration such as 30m", r.Window)
		}
		r.WindowDuration = d
	}
	r.Limits = nil
	for meter, value := range r.Limit {
		m := limitPattern.FindStringSubmatch(value)
		if m == nil {
			return fmt.Errorf("limit %s: %q: want a count per minute, hour, day or week, such as 200/day", meter, value)
		}
		count, _ := strconv.ParseInt(m[1], 10, 64)
		r.Limits = append(r.Limits, RuleLimit{Meter: strings.TrimSpace(meter), Count: count, Period: limitPeriods[m[2]]})
	}
	return nil
}

func oneOf(value string, list []string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}
