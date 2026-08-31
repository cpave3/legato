package main

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
)

func TestConfiguredAdaptersUsesWorkerDefaultAndConfiguredLaunchSettings(t *testing.T) {
	cfg := &config.Config{
		Swarm: config.SwarmConfig{Worker: config.SwarmParticipantConfig{Agent: "chimera"}},
		Adapters: map[string]config.AdapterConfig{
			"chimera": {
				LaunchArgs: []string{"--sandbox"},
				Tiers: map[string]config.TierConfig{
					"large": {LaunchArgs: []string{"--model", "tier-model"}},
				},
			},
		},
	}
	defaultAdapter, adapters := configuredAdapters(cfg, "legato")
	if defaultAdapter.Name() != "chimera" || len(adapters) != 3 {
		t.Fatalf("default = %q, adapters = %d", defaultAdapter.Name(), len(adapters))
	}
	launcher := adapters["chimera"].(service.LaunchCommandAdapter)
	cmd := launcher.LaunchCommand(map[string]string{}, "", "direct-model", "large")
	for _, want := range []string{"--sandbox", "--model direct-model", "--model tier-model"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command %q missing %q", cmd, want)
		}
	}
}

func TestBuildValidateOptionsUsesRegisteredDefaultAdapter(t *testing.T) {
	cfg := &config.Config{Adapters: map[string]config.AdapterConfig{
		"claude-code": {Tiers: map[string]config.TierConfig{"small": {}}},
	}}
	opts := buildValidateOptions(cfg)
	if opts.DefaultAgent != "claude-code" {
		t.Fatalf("default agent = %q, want claude-code", opts.DefaultAgent)
	}
}
