package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ClaudeAccount is an instance-local subscription identity. The implicit
// default is never an entry: its existing login and environment stay unchanged.
type ClaudeAccount struct {
	Name      string `yaml:"name" json:"name"`
	Label     string `yaml:"label,omitempty" json:"label,omitempty"`
	ConfigDir string `yaml:"config_dir" json:"config_dir"`
}

var claudeAccountName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func ValidateClaudeAccounts(cfg *Config) error {
	names := map[string]bool{}
	for i, a := range cfg.ClaudeAccounts {
		path := fmt.Sprintf("claude_accounts[%d]", i)
		if !claudeAccountName.MatchString(a.Name) || a.Name == "default" {
			return fmt.Errorf("%s.name must be a lowercase slug other than default", path)
		}
		if names[a.Name] {
			return fmt.Errorf("%s.name is repeated", path)
		}
		names[a.Name] = true
		if !filepath.IsAbs(a.ConfigDir) || filepath.Clean(a.ConfigDir) != a.ConfigDir || a.ConfigDir == string(filepath.Separator) {
			return fmt.Errorf("%s.config_dir must be an absolute normalized directory, not a filesystem root", path)
		}
		for _, prev := range cfg.ClaudeAccounts[:i] {
			if accountPathContains(prev.ConfigDir, a.ConfigDir) || accountPathContains(a.ConfigDir, prev.ConfigDir) {
				return fmt.Errorf("%s.config_dir overlaps another Claude account", path)
			}
		}
	}
	return nil
}

func accountPathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (cfg *Config) ValidateClaudeSelection(agent, account string) error {
	if account == "" || (account == "default" && agent == "claude-code") {
		return nil
	}
	if agent != "claude-code" {
		return fmt.Errorf("account requires the claude-code agent")
	}
	for _, a := range cfg.ClaudeAccounts {
		if a.Name == account {
			return nil
		}
	}
	return fmt.Errorf("Claude account %q is not configured on this instance", account)
}

// UsageReaderID keeps legacy cache identities intact. Only named Claude
// subscriptions acquire a suffix; this is not a new provider kind.
func UsageReaderID(provider, account string) string {
	if account == "" || account == "default" {
		return provider
	}
	return provider + ":" + account
}

// ParseUsageMeter resolves wire identities centrally. Account existence is
// checked by Config.IsUsageMeter, not by the provider-kind descriptor table.
func ParseUsageMeter(meter string) (provider, account, window string, ok bool) {
	parts := strings.Split(meter, ".")
	switch len(parts) {
	case 2:
		provider, window = parts[0], parts[1]
	case 3:
		provider, account, window = parts[0], parts[1], parts[2]
		if provider != "claude" || !claudeAccountName.MatchString(account) || account == "default" {
			return "", "", "", false
		}
	default:
		return "", "", "", false
	}
	for _, p := range UsageProviders() {
		if p.ID != provider {
			continue
		}
		for _, w := range p.Windows {
			if w == window {
				return provider, account, window, true
			}
		}
	}
	return "", "", "", false
}

func (cfg *Config) IsUsageMeter(meter string) bool {
	_, account, _, ok := ParseUsageMeter(meter)
	return ok && (account == "" || cfg.ValidateClaudeSelection("claude-code", account) == nil)
}
