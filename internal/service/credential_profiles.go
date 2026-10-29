package service

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
	"gopkg.in/yaml.v3"
)

//go:embed credential_profiles/*.yaml
var shippedProfiles embed.FS

// CredentialProfile describes one kind of credential: which issuer makes it,
// which channels give it to an agent, the secret types it takes, and the
// rules a credential gets when it sets none of its own. It holds no secret.
type CredentialProfile struct {
	Name        string                  `yaml:"name"`
	Description string                  `yaml:"description"`
	Issuer      string                  `yaml:"issuer"`
	Channels    []string                `yaml:"channels"`
	Accepts     []string                `yaml:"accepts"`
	Policy      []config.CredentialRule `yaml:"policy"`
	// Inspector says what a use does: rest (HTTP routes) or ssh (logins).
	Inspector string `yaml:"inspector"`
	// Upstream and Key are where the http-proxy channel sends requests and
	// puts the key. A credential's settings replace them.
	Upstream string              `yaml:"upstream"`
	Key      credential.KeyPlace `yaml:"key"`
	Routes   []credential.Route  `yaml:"routes"`
	// APIPrefix serves the REST routes under /c/<credential><prefix>; Git
	// is the git host pushes go to, under /c/<credential>/git/.
	APIPrefix string `yaml:"api_prefix"`
	Git       string `yaml:"git"`
	// JWTExchange is the program of the jwt-exchange issuer; Permissions
	// say what a token for each operation may do.
	JWTExchange *credential.ExchangeTemplate `yaml:"jwt_exchange"`
	Permissions map[string]map[string]string `yaml:"permissions"`
	// Env is what an agent session gets for each credential of the profile.
	// {placeholder}, {base_url} and {NAME} (the credential's name in
	// capitals) are filled in.
	Env map[string]string `yaml:"env"`
	// Source is where the profile was read from: "shipped" or a file.
	Source string `yaml:"-"`
}

// LoadCredentialProfiles returns the shipped profiles and those in dirs, a
// later one replacing an earlier of the same name.
func LoadCredentialProfiles(dirs ...string) (map[string]CredentialProfile, error) {
	profiles := map[string]CredentialProfile{}
	entries, err := shippedProfiles.ReadDir("credential_profiles")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		data, err := shippedProfiles.ReadFile("credential_profiles/" + entry.Name())
		if err != nil {
			return nil, err
		}
		profile, err := parseCredentialProfile(data, "shipped")
		if err != nil {
			return nil, fmt.Errorf("shipped profile %s: %w", entry.Name(), err)
		}
		profiles[profile.Name] = profile
	}
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
		if err != nil {
			continue
		}
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				return nil, err
			}
			profile, err := parseCredentialProfile(data, file)
			if err != nil {
				return nil, fmt.Errorf("profile %s: %w", file, err)
			}
			profiles[profile.Name] = profile
		}
	}
	return profiles, nil
}

func parseCredentialProfile(data []byte, source string) (CredentialProfile, error) {
	var profile CredentialProfile
	if err := yaml.Unmarshal(data, &profile); err != nil {
		return CredentialProfile{}, err
	}
	profile.Name = strings.TrimSpace(profile.Name)
	if profile.Name == "" {
		return CredentialProfile{}, fmt.Errorf("a profile needs a name")
	}
	if profile.Issuer == "" {
		profile.Issuer = "static"
	}
	switch profile.Issuer {
	case "static":
	case "jwt-exchange":
		if profile.JWTExchange == nil {
			return CredentialProfile{}, fmt.Errorf("issuer jwt-exchange needs jwt_exchange")
		}
		if err := profile.JWTExchange.Validate(); err != nil {
			return CredentialProfile{}, fmt.Errorf("jwt_exchange: %w", err)
		}
		if len(profile.Accepts) != 1 || profile.Accepts[0] != credential.TypeKeyPair {
			return CredentialProfile{}, fmt.Errorf("issuer jwt-exchange signs with a keypair secret only")
		}
	default:
		return CredentialProfile{}, fmt.Errorf("issuer %q is not available in this Legato (static, jwt-exchange)", profile.Issuer)
	}
	if profile.Git != "" && !strings.HasPrefix(profile.Git, "https://") {
		return CredentialProfile{}, fmt.Errorf("git %q: want an https address", profile.Git)
	}
	for i := range profile.Policy {
		if err := config.NormalizeCredentialRule(&profile.Policy[i]); err != nil {
			return CredentialProfile{}, fmt.Errorf("policy[%d]: %w", i, err)
		}
	}
	if err := checkProfileChannels(profile); err != nil {
		return CredentialProfile{}, err
	}
	profile.Source = source
	return profile, nil
}

// The channels a profile can name.
const (
	ChannelHelper    = "helper"
	ChannelHTTPProxy = "http-proxy"
	ChannelSSHAgent  = "ssh-agent"
)

func checkProfileChannels(p CredentialProfile) error {
	for _, ch := range p.Channels {
		switch ch {
		case ChannelHelper:
		case ChannelHTTPProxy:
			if p.Inspector != "rest" {
				return fmt.Errorf("channel http-proxy needs inspector: rest")
			}
			if len(p.Routes) == 0 {
				return fmt.Errorf("channel http-proxy needs routes")
			}
			if err := credential.ValidateRoutes(p.Routes); err != nil {
				return err
			}
			if p.Upstream != "" && !strings.HasPrefix(p.Upstream, "https://") {
				return fmt.Errorf("upstream %q: want an https address", p.Upstream)
			}
			if p.Key != (credential.KeyPlace{}) {
				if err := p.Key.Validate(); err != nil {
					return fmt.Errorf("key: %w", err)
				}
			}
		case ChannelSSHAgent:
			if p.Inspector != "ssh" {
				return fmt.Errorf("channel ssh-agent needs inspector: ssh")
			}
			if len(p.Accepts) != 1 || p.Accepts[0] != credential.TypeKeyPair {
				return fmt.Errorf("channel ssh-agent takes keypair secrets only")
			}
		default:
			return fmt.Errorf("channel %q is not one this Legato has (helper, http-proxy, ssh-agent)", ch)
		}
	}
	return nil
}

// defaultType is the secret type of a profile that takes only one.
func (p CredentialProfile) defaultType() string {
	if len(p.Accepts) == 1 {
		return p.Accepts[0]
	}
	return ""
}
