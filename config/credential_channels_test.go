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

func TestAVaultIsASourceOfCredentials(t *testing.T) {
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
	// No profile: every item that is a credential, keys and secrets.
	if cfg.CredentialSources[1].Profile != "" {
		t.Fatalf("the default profile is %q, want none", cfg.CredentialSources[1].Profile)
	}
	writeTestConfig(t, "credential_sources:\n  - {vault: op://Legato, profile: secret}\n")
	if _, err := Load(); err != nil {
		t.Fatalf("a source of secrets: %v", err)
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
		// The reviewer App's credential names the same settings.
		for _, profile := range []string{"github", "github-reviewer"} {
			writeTestConfig(t, "credentials:\n  - name: gh-broken\n    secret: legato://k\n    profile: "+profile+"\n    settings: "+settings+"\n")
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "gh-broken") {
				t.Errorf("%s, %s: err = %v", profile, name, err)
			}
		}
	}
}

func TestAnAzureCredentialNamesItsAppAndTenant(t *testing.T) {
	writeTestConfig(t, `
credentials:
  - name: azure-reader
    secret: legato://azure-reader-secret
    profile: azure
    settings:
      client_id: " b4c42806-dfd5-4a59-b6d3-6ae1c96d2166 "
      tenant_id: 7948632d-756d-40b1-8af2-1b9ca8b344b1
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Credentials[0].Settings
	if s.ClientID != "b4c42806-dfd5-4a59-b6d3-6ae1c96d2166" || s.TenantID != "7948632d-756d-40b1-8af2-1b9ca8b344b1" {
		t.Fatalf("settings = %+v", s)
	}
	for name, settings := range map[string]string{
		"no client id":       "{tenant_id: 7948632d-756d-40b1-8af2-1b9ca8b344b1}",
		"no tenant id":       "{client_id: b4c42806-dfd5-4a59-b6d3-6ae1c96d2166}",
		"a client id flag":   "{client_id: '--debug', tenant_id: 7948632d-756d-40b1-8af2-1b9ca8b344b1}",
		"a tenant with an ;": "{client_id: b4c42806-dfd5-4a59-b6d3-6ae1c96d2166, tenant_id: 'x; rm -rf ~'}",
	} {
		writeTestConfig(t, "credentials:\n  - name: az-broken\n    secret: legato://k\n    profile: azure\n    settings: "+settings+"\n")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "az-broken") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestAgentsGHIsKeptFromThePersonsLoginUnlessTurnedOff(t *testing.T) {
	writeTestConfig(t, "")
	cfg, err := Load()
	if err != nil || !cfg.CredentialChannels.GH.IsolateLogin() {
		t.Fatalf("default isolate = %v, %v; want on", cfg.CredentialChannels.GH.IsolateLogin(), err)
	}
	writeTestConfig(t, "credential_channels:\n  gh:\n    isolate_login: false\n")
	cfg, err = Load()
	if err != nil || cfg.CredentialChannels.GH.IsolateLogin() {
		t.Fatalf("isolate = %v, %v; want off", cfg.CredentialChannels.GH.IsolateLogin(), err)
	}
}

func TestApprovalDevicesExpectTheAppsSigningDigest(t *testing.T) {
	writeTestConfig(t, "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	digests := cfg.ApprovalDevices.AndroidAppDigestBytes()
	if len(digests) != 1 || len(digests[0]) != 32 || digests[0][0] != 0xfa {
		t.Fatalf("default digests = %x", digests)
	}
	writeTestConfig(t, "approval_devices:\n  android_app_digests: ['AB:CD:"+strings.Repeat("00:", 29)+"EF']\n")
	cfg, err = Load()
	if err != nil || len(cfg.ApprovalDevices.AndroidAppDigestBytes()) != 1 || cfg.ApprovalDevices.AndroidAppDigestBytes()[0][0] != 0xab {
		t.Fatalf("digests = %x, %v", cfg.ApprovalDevices.AndroidAppDigestBytes(), err)
	}
	writeTestConfig(t, "approval_devices:\n  android_app_digests: [nothex]\n")
	if _, err := Load(); err == nil {
		t.Fatal("a digest that is not SHA-256 hex loaded")
	}
}
