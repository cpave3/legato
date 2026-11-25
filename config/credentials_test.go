package config

import (
	"strings"
	"testing"
	"time"
)

func TestCredentialsLoadWithTheirRules(t *testing.T) {
	writeTestConfig(t, `
secrets:
  master_key: secret-service
  onepassword:
    token: ops_abc
credentials:
  - name: elevenlabs
    secret: legato://eleven-key
    policy:
      - { operation: speak, approve: auto, limit: { requests: 200/day } }
      - { operation: [speak, read], approve: window, window: 30m }
      - { resource: "https://api.elevenlabs.io/v1/user/**", approve: deny }
  - name: homelab
    secret: op://Legato/homelab/private key
    profile: secret
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Secrets.MasterKey != "secret-service" || cfg.Secrets.OnePassword.Token != "ops_abc" {
		t.Fatalf("secrets = %+v", cfg.Secrets)
	}
	if len(cfg.Credentials) != 2 {
		t.Fatalf("credentials = %+v", cfg.Credentials)
	}
	eleven := cfg.Credentials[0]
	if eleven.Profile != "secret" || len(eleven.Policy) != 3 {
		t.Fatalf("elevenlabs = %+v; want the default profile and three rules", eleven)
	}
	rule := eleven.Policy[1]
	if strings.Join(rule.Operation, ",") != "speak,read" || rule.Approve != "window" || rule.WindowDuration != 30*time.Minute {
		t.Fatalf("rule = %+v", rule)
	}
	limit := eleven.Policy[0].Limits[0]
	if limit.Meter != "requests" || limit.Count != 200 || limit.Period != 24*time.Hour {
		t.Fatalf("limit = %+v", limit)
	}
}

func TestSecretsDefaultToAnAutomaticMasterKey(t *testing.T) {
	writeTestConfig(t, "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Secrets.MasterKey != "auto" {
		t.Fatalf("master_key = %q, want auto", cfg.Secrets.MasterKey)
	}
}

func TestBadCredentialsFailToLoad(t *testing.T) {
	for name, body := range map[string]string{
		"no name":                   "credentials:\n  - secret: legato://x\n",
		"duplicate name":            "credentials:\n  - {name: a, secret: legato://x}\n  - {name: A, secret: legato://y}\n",
		"no secret":                 "credentials:\n  - {name: a}\n",
		"unknown store":             "credentials:\n  - {name: a, secret: vault://kv/a}\n",
		"unknown approve":           "credentials:\n  - {name: a, secret: legato://x, policy: [{approve: sometimes}]}\n",
		"window without a duration": "credentials:\n  - {name: a, secret: legato://x, policy: [{approve: window}]}\n",
		"bad limit":                 "credentials:\n  - {name: a, secret: legato://x, policy: [{approve: auto, limit: {requests: lots}}]}\n",
		"bad master key":            "secrets:\n  master_key: usb-stick\n",
	} {
		writeTestConfig(t, body)
		if _, err := Load(); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

func TestOnePasswordIsReadThroughTheSDKUnlessOpIsChosen(t *testing.T) {
	writeTestConfig(t, "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Secrets.OnePassword.Backend != "sdk" {
		t.Fatalf("backend = %q, want sdk", cfg.Secrets.OnePassword.Backend)
	}

	writeTestConfig(t, "secrets:\n  onepassword:\n    backend: op\n")
	if cfg, err = Load(); err != nil || cfg.Secrets.OnePassword.Backend != "op" {
		t.Fatalf("backend = %q, %v; want op", cfg.Secrets.OnePassword.Backend, err)
	}

	writeTestConfig(t, "secrets:\n  onepassword:\n    backend: cli\n")
	if _, err = Load(); err == nil || !strings.Contains(err.Error(), "secrets.onepassword.backend") || !strings.Contains(err.Error(), "sdk, op") {
		t.Fatalf("err = %v; want the setting and its values", err)
	}
}

func TestAJiraCredentialLoadsWithoutSettings(t *testing.T) {
	writeTestConfig(t, `
credentials:
  - name: jira
    profile: jira
    secret: legato://jira-api-token
  - name: other-jira
    profile: jira
    secret: legato://other-token
    settings:
      upstream: https://other.atlassian.net/
      username: " bot@example.com "
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	other := cfg.Credentials[1].Settings
	if other.Upstream != "https://other.atlassian.net" || other.Username != "bot@example.com" {
		t.Fatalf("settings = %+v", other)
	}
}

func TestABasicKeyIsAHeaderWithoutAFormat(t *testing.T) {
	writeTestConfig(t, `
credentials:
  - name: tracker
    profile: api-key
    secret: legato://tracker
    settings:
      upstream: https://tracker.example.com
      username: bot
      key: { header: Authorization, scheme: basic }
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if k := cfg.Credentials[0].Settings.Key; k.Scheme != "basic" || k.Header != "Authorization" {
		t.Fatalf("key = %+v", k)
	}
	for name, key := range map[string]string{
		"basic in a query":    "{ query: key, scheme: basic }",
		"basic with a format": `{ header: Authorization, format: "Basic {secret}", scheme: basic }`,
		"an unknown scheme":   "{ header: Authorization, scheme: digest }",
	} {
		writeTestConfig(t, "credentials:\n  - {name: t, profile: api-key, secret: legato://t, settings: {upstream: https://t.example.com, username: bot, key: "+key+"}}\n")
		if _, err := Load(); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}
