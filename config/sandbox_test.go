package config

import (
	"strings"
	"testing"
)

func TestTheSandboxIsOffWithToolchainMountsUnlessConfigured(t *testing.T) {
	writeTestConfig(t, "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	sb := cfg.Agents.Sandbox
	if sb.Enabled || sb.Backend != "bwrap" || !sb.DefaultMountsOn() || len(sb.Mounts) != 0 {
		t.Fatalf("sandbox = %+v", sb)
	}
}

func TestTheSandboxBlockIsParsed(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	writeTestConfig(t, `
agents:
  sandbox:
    enabled: true
    default_mounts: false
    mounts:
      - path: ~/Projects
      - path: /srv/data
        access: write
`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	sb := cfg.Agents.Sandbox
	if !sb.Enabled || sb.DefaultMountsOn() || len(sb.Mounts) != 2 {
		t.Fatalf("sandbox = %+v", sb)
	}
	if sb.Mounts[0] != (ResourcePathConfig{Path: "/home/me/Projects", Access: "read"}) || sb.Mounts[1] != (ResourcePathConfig{Path: "/srv/data", Access: "write"}) {
		t.Fatalf("mounts = %+v", sb.Mounts)
	}
}

func TestASandboxThatCannotWorkFailsToLoad(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"backend", "agents:\n  sandbox:\n    backend: podman\n", "bwrap"},
		{"relative mount", "agents:\n  sandbox:\n    mounts: [{path: notes}]\n", "absolute"},
		{"access", "agents:\n  sandbox:\n    mounts: [{path: /a, access: admin}]\n", "read or write"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeTestConfig(t, tc.yaml)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
