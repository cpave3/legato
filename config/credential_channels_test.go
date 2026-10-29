package config

import (
	"strings"
	"testing"
)

func TestAnAPIKeyCredentialNamesItsUpstreamAndKeyPlace(t *testing.T) {
	writeTestConfig(t, `
credentials:
  - name: openai
    secret: legato://openai-key
    profile: api-key
    settings:
      upstream: https://api.openai.com/v1
      key: { header: Authorization, format: "Bearer {secret}" }
  - name: maps
    secret: legato://maps-key
    profile: api-key
    settings:
      upstream: https://maps.example.com
      key: { query: key }
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	openai := cfg.Credentials[0].Settings
	if openai.Upstream != "https://api.openai.com/v1" || openai.Key.Header != "Authorization" || openai.Key.Format != "Bearer {secret}" {
		t.Fatalf("settings = %+v", openai)
	}
	if cfg.Credentials[1].Settings.Key.Query != "key" {
		t.Fatalf("maps = %+v", cfg.Credentials[1].Settings)
	}
}

func TestBadAPIKeySettingsFailNamingTheCredential(t *testing.T) {
	for name, settings := range map[string]string{
		"http upstream":  "{upstream: http://api.example.com, key: {header: X-Key}}",
		"no upstream":    "{key: {header: X-Key}}",
		"no key place":   "{upstream: https://api.example.com}",
		"both places":    "{upstream: https://api.example.com, key: {header: X-Key, query: key}}",
		"format no slot": "{upstream: https://api.example.com, key: {header: Authorization, format: Bearer}}",
		"no settings":    "",
		"upstream w/ ?":  "{upstream: 'https://api.example.com/?a=1', key: {header: X-Key}}",
	} {
		body := "credentials:\n  - name: broken-one\n    secret: legato://x\n    profile: api-key\n"
		if settings != "" {
			body += "    settings: " + settings + "\n"
		}
		writeTestConfig(t, body)
		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "broken-one") {
			t.Errorf("%s: err = %v; want one naming the credential", name, err)
		}
	}
}

func TestCredentialChannelsHaveLoopbackDefaults(t *testing.T) {
	writeTestConfig(t, "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CredentialChannels.HTTPProxy.Listen != "127.0.0.1:0" {
		t.Fatalf("listen = %q", cfg.CredentialChannels.HTTPProxy.Listen)
	}
	if !strings.HasSuffix(cfg.CredentialChannels.SSHAgent.KnownHosts, "/.ssh/known_hosts") {
		t.Fatalf("known_hosts = %q", cfg.CredentialChannels.SSHAgent.KnownHosts)
	}

	writeTestConfig(t, "credential_channels:\n  http_proxy: {listen: '127.0.0.1:7788'}\n  ssh_agent: {known_hosts: /etc/legato/known_hosts}\n")
	cfg, err = Load()
	if err != nil || cfg.CredentialChannels.HTTPProxy.Listen != "127.0.0.1:7788" || cfg.CredentialChannels.SSHAgent.KnownHosts != "/etc/legato/known_hosts" {
		t.Fatalf("cfg = %+v, %v", cfg.CredentialChannels, err)
	}

	for _, listen := range []string{"0.0.0.0:7788", "192.168.1.5:80", "7788", "example.com:80"} {
		writeTestConfig(t, "credential_channels:\n  http_proxy: {listen: '"+listen+"'}\n")
		if _, err := Load(); err == nil {
			t.Errorf("listen %s: loaded; the proxy listens on loopback only", listen)
		}
	}
}

func TestTheOnePasswordTokenCanComeFromAFile(t *testing.T) {
	writeTestConfig(t, "secrets:\n  onepassword:\n    token_file: ~/.config/legato/op-token\n")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(cfg.Secrets.OnePassword.TokenFile, "/.config/legato/op-token") || strings.HasPrefix(cfg.Secrets.OnePassword.TokenFile, "~") {
		t.Fatalf("token_file = %q", cfg.Secrets.OnePassword.TokenFile)
	}
	for name, body := range map[string]string{
		"both":     "secrets:\n  onepassword:\n    token: ops_x\n    token_file: /run/op-token\n",
		"relative": "secrets:\n  onepassword:\n    token_file: op-token\n",
	} {
		writeTestConfig(t, body)
		if _, err := Load(); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

func TestAVaultIsASourceOfSSHKeys(t *testing.T) {
	writeTestConfig(t, `
credential_sources:
  - vault: op://Legato
    profile: ssh
    policy:
      - { operation: ssh.login, approve: window, window: 30m }
  - vault: op://Servers
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CredentialSources) != 2 {
		t.Fatalf("sources = %+v", cfg.CredentialSources)
	}
	s := cfg.CredentialSources[0]
	if s.Vault != "op://Legato" || s.Profile != "ssh" || len(s.Policy) != 1 || s.Policy[0].WindowDuration.Minutes() != 30 {
		t.Fatalf("source = %+v", s)
	}
	if cfg.CredentialSources[1].Profile != "ssh" {
		t.Fatalf("the default profile is %q, want ssh", cfg.CredentialSources[1].Profile)
	}
	for name, body := range map[string]string{
		"not a vault":     "credential_sources:\n  - vault: op://Legato/item\n",
		"not 1Password":   "credential_sources:\n  - vault: legato://keys\n",
		"another profile": "credential_sources:\n  - {vault: op://Legato, profile: elevenlabs}\n",
		"bad rule":        "credential_sources:\n  - {vault: op://Legato, policy: [{approve: sometimes}]}\n",
		"the same twice":  "credential_sources:\n  - {vault: op://Legato}\n  - {vault: op://Legato/}\n",
	} {
		writeTestConfig(t, body)
		if _, err := Load(); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

func TestAGitHubCredentialNamesItsAppAndRepositories(t *testing.T) {
	writeTestConfig(t, `
credentials:
  - name: github
    secret: op://Legato/legato-app/private key
    profile: github
    settings:
      client_id: Iv23liAbC
      repositories: [cpave3/legato, cpave3/chimera, my-org/*]
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Credentials[0].Settings
	if s.ClientID != "Iv23liAbC" || len(s.Repositories) != 3 || s.Repositories[1] != "cpave3/chimera" || s.Repositories[2] != "my-org/*" {
		t.Fatalf("settings = %+v", s)
	}
	for name, settings := range map[string]string{
		"no client id":     "{repositories: [cpave3/legato]}",
		"no repositories":  "{client_id: Iv1}",
		"not owner/name":   "{client_id: Iv1, repositories: [legato]}",
		"a name pattern":   "{client_id: Iv1, repositories: ['cpave3/leg*']}",
		"an owner pattern": "{client_id: Iv1, repositories: ['*/legato']}",
	} {
		writeTestConfig(t, "credentials:\n  - name: gh-broken\n    secret: legato://k\n    profile: github\n    settings: "+settings+"\n")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "gh-broken") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
