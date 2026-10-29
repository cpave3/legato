package service

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cpave3/legato/config"
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
	profile.Source = source
	return profile, nil
}
