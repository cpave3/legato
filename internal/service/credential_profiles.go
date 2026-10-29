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
	if profile.Issuer != "static" {
		return CredentialProfile{}, fmt.Errorf("issuer %q is not available in this Legato (only static)", profile.Issuer)
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
