package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
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

// OnePasswordConfig reads op:// references as a service account. The token
// is given as itself or, better, as a file (mode 0600) read for each read,
// so it never has to be in the environment agents inherit.
type OnePasswordConfig struct {
	Token     string `yaml:"token"`
	TokenFile string `yaml:"token_file"`
}

// CredentialConfig is one credential agents can ask for.
type CredentialConfig struct {
	Name    string `yaml:"name"`
	Secret  string `yaml:"secret"`
	Profile string `yaml:"profile"`
	// Type of the secret, when the profile does not say.
	Type   string           `yaml:"type"`
	Policy []CredentialRule `yaml:"policy"`
	// Settings a profile needs from the credential (api-key: the upstream
	// and the key's place).
	Settings CredentialSettings `yaml:"settings"`
}

// CredentialSettings are what the api-key profile needs of a credential.
type CredentialSettings struct {
	// Upstream is the https base URL every proxied request goes to.
	Upstream string             `yaml:"upstream"`
	Key      CredentialKeyPlace `yaml:"key"`
	// ClientID and Repositories are what the github profile needs: the
	// GitHub App's client ID (or App ID), and the owner/name repositories
	// agents may use; owner/* is every repository of an owner the App is
	// installed on.
	ClientID     string   `yaml:"client_id"`
	Repositories []string `yaml:"repositories"`
}

// CredentialKeyPlace is where an HTTP API takes its key: a header, written
// with Format ("Bearer {secret}"), or a query parameter.
type CredentialKeyPlace struct {
	Header string `yaml:"header"`
	Format string `yaml:"format"`
	Query  string `yaml:"query"`
}

// CredentialSourceConfig is a vault whose items are credentials: for the
// ssh profile, every SSH Key item of a 1Password vault.
type CredentialSourceConfig struct {
	Vault   string           `yaml:"vault"`
	Profile string           `yaml:"profile"`
	Policy  []CredentialRule `yaml:"policy"`
}

// CredentialChannelsConfig configures the channels that keep secrets from agents.
type CredentialChannelsConfig struct {
	HTTPProxy HTTPProxyChannelConfig `yaml:"http_proxy"`
	SSHAgent  SSHAgentChannelConfig  `yaml:"ssh_agent"`
}

// HTTPProxyChannelConfig is the loopback proxy for API keys.
type HTTPProxyChannelConfig struct {
	// Listen is a loopback host:port; port 0 (the default) picks one.
	Listen string `yaml:"listen"`
}

// SSHAgentChannelConfig is the SSH agent of agent sessions.
type SSHAgentChannelConfig struct {
	// KnownHosts names hosts by their keys (default ~/.ssh/known_hosts).
	KnownHosts string `yaml:"known_hosts"`
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
	githubRepo     = regexp.MustCompile(`^[A-Za-z0-9_.-]+/([A-Za-z0-9_.-]+|\*)$`)
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
	op := &cfg.Secrets.OnePassword
	op.Token = strings.TrimSpace(op.Token)
	op.TokenFile = strings.TrimSpace(op.TokenFile)
	if strings.HasPrefix(op.TokenFile, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			op.TokenFile = filepath.Join(home, op.TokenFile[2:])
		}
	}
	if op.Token != "" && op.TokenFile != "" {
		return fmt.Errorf("secrets.onepassword: set token or token_file, not both")
	}
	if op.TokenFile != "" && !filepath.IsAbs(op.TokenFile) {
		return fmt.Errorf("secrets.onepassword.token_file %q: want an absolute path or one starting with ~/", op.TokenFile)
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
		if err := normalizeCredentialSettings(c); err != nil {
			return fmt.Errorf("credentials %q: settings: %w", c.Name, err)
		}
	}
	if err := normalizeCredentialSources(cfg); err != nil {
		return err
	}
	return normalizeCredentialChannels(&cfg.CredentialChannels)
}

func normalizeCredentialSources(cfg *Config) error {
	seen := map[string]bool{}
	for i := range cfg.CredentialSources {
		src := &cfg.CredentialSources[i]
		scheme, vault, ok := strings.Cut(strings.TrimSpace(src.Vault), "://")
		vault = strings.TrimSuffix(vault, "/")
		if !ok || scheme != "op" || vault == "" || strings.Contains(vault, "/") {
			return fmt.Errorf("credential_sources[%d]: vault %q: want a 1Password vault, op://<vault>", i, src.Vault)
		}
		src.Vault = "op://" + vault
		if seen[strings.ToLower(src.Vault)] {
			return fmt.Errorf("credential_sources: %s is listed twice", src.Vault)
		}
		seen[strings.ToLower(src.Vault)] = true
		if src.Profile = strings.TrimSpace(src.Profile); src.Profile == "" {
			src.Profile = "ssh"
		}
		if src.Profile != "ssh" {
			return fmt.Errorf("credential_sources %s: profile %q: a vault gives SSH keys only for now (profile ssh)", src.Vault, src.Profile)
		}
		for j := range src.Policy {
			if err := NormalizeCredentialRule(&src.Policy[j]); err != nil {
				return fmt.Errorf("credential_sources %s: policy[%d]: %w", src.Vault, j, err)
			}
		}
	}
	return nil
}

// normalizeCredentialSettings checks the settings a credential gives. The
// api-key profile needs both; any credential may set them.
func normalizeCredentialSettings(c *CredentialConfig) error {
	s := &c.Settings
	s.Upstream = strings.TrimRight(strings.TrimSpace(s.Upstream), "/")
	if c.Profile == "api-key" {
		if s.Upstream == "" {
			return fmt.Errorf("the api-key profile needs settings.upstream, the https address of the API")
		}
		if s.Key == (CredentialKeyPlace{}) {
			return fmt.Errorf("the api-key profile needs settings.key: {header: <name>} or {query: <name>}")
		}
	}
	if c.Profile == "github" {
		s.ClientID = strings.TrimSpace(s.ClientID)
		if s.ClientID == "" {
			return fmt.Errorf("the github profile needs settings.client_id, the GitHub App's client ID")
		}
		if len(s.Repositories) == 0 {
			return fmt.Errorf("the github profile needs settings.repositories, the owner/name repositories agents may use")
		}
		for i, repo := range s.Repositories {
			s.Repositories[i] = strings.TrimSpace(repo)
			if !githubRepo.MatchString(s.Repositories[i]) {
				return fmt.Errorf("settings.repositories: %q: want owner/name, or owner/* for every repository of an owner", repo)
			}
		}
	}
	if s.Upstream != "" {
		u, err := url.Parse(s.Upstream)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return fmt.Errorf("upstream %q: want an https address with no query, such as https://api.example.com/v1", s.Upstream)
		}
	}
	k := s.Key
	switch {
	case k == (CredentialKeyPlace{}):
	case k.Header != "" && k.Query != "":
		return fmt.Errorf("key: a header or a query parameter, not both")
	case k.Header == "" && k.Query == "":
		return fmt.Errorf("key: name a header or a query parameter")
	case k.Format != "" && k.Query != "":
		return fmt.Errorf("key: a query parameter takes no format")
	case k.Format != "" && !strings.Contains(k.Format, "{secret}"):
		return fmt.Errorf("key: format %q must contain {secret}", k.Format)
	}
	return nil
}

func normalizeCredentialChannels(c *CredentialChannelsConfig) error {
	c.HTTPProxy.Listen = strings.TrimSpace(c.HTTPProxy.Listen)
	if c.HTTPProxy.Listen == "" {
		c.HTTPProxy.Listen = "127.0.0.1:0"
	}
	host, port, err := net.SplitHostPort(c.HTTPProxy.Listen)
	if ip := net.ParseIP(host); err != nil || port == "" || ((ip == nil || !ip.IsLoopback()) && host != "localhost") {
		return fmt.Errorf("credential_channels.http_proxy.listen %q: want a loopback address and port, such as 127.0.0.1:0", c.HTTPProxy.Listen)
	}
	c.SSHAgent.KnownHosts = strings.TrimSpace(c.SSHAgent.KnownHosts)
	if c.SSHAgent.KnownHosts == "" {
		if home, err := os.UserHomeDir(); err == nil {
			c.SSHAgent.KnownHosts = filepath.Join(home, ".ssh", "known_hosts")
		}
	} else if strings.HasPrefix(c.SSHAgent.KnownHosts, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			c.SSHAgent.KnownHosts = filepath.Join(home, c.SSHAgent.KnownHosts[2:])
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
