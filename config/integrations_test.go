package config

import (
	"strings"
	"testing"
)

func TestTheArgusIntegrationLoadsWithDefaults(t *testing.T) {
	writeTestConfig(t, "integrations:\n  argus:\n    url: https://argus.lan/\n")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	argus := cfg.Integrations.Argus
	if argus == nil || argus.URL != "https://argus.lan" || argus.Delivery != "auto" || argus.Agent != "claude-code" {
		t.Fatalf("argus = %+v", argus)
	}
}

func TestTheArgusIntegrationIsOffWhenAbsent(t *testing.T) {
	writeTestConfig(t, "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Integrations.Argus != nil {
		t.Fatalf("argus = %+v, want unset", cfg.Integrations.Argus)
	}
}

func TestTheArgusIntegrationRejectsBadSettings(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"no url", "integrations:\n  argus:\n    delivery: ask\n", "integrations.argus.url"},
		{"plain http to another host", "integrations:\n  argus:\n    url: http://argus.lan\n", "integrations.argus.url"},
		{"unknown delivery", "integrations:\n  argus:\n    url: https://argus.lan\n    delivery: sometimes\n", "integrations.argus.delivery"},
		{"unknown agent", "integrations:\n  argus:\n    url: https://argus.lan\n    agent: vim\n", "integrations.argus.agent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeTestConfig(t, tc.yaml)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
	writeTestConfig(t, "integrations:\n  argus:\n    url: http://127.0.0.1:8080\n    delivery: ask\n    agent: codex\n")
	cfg, err := Load()
	if err != nil || cfg.Integrations.Argus.Delivery != "ask" || cfg.Integrations.Argus.Agent != "codex" {
		t.Fatalf("loopback http = %+v, %v", cfg, err)
	}
}
