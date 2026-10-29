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
