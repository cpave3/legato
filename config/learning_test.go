package config

import (
	"strings"
	"testing"
)

func TestLearningConfigDefaultsToDisabledWithBoundedDefaults(t *testing.T) {
	writeTestConfig(t, "theme: default\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Learning.Enabled {
		t.Fatal("Learning.Enabled should default to false")
	}
	if cfg.Learning.PromptEnabled {
		t.Fatal("Learning.PromptEnabled should default to false")
	}
	if cfg.Learning.PacketMaxItems <= 0 || cfg.Learning.PacketMaxBytes <= 0 {
		t.Fatalf("packet defaults must be positive: %+v", cfg.Learning)
	}
	if cfg.Learning.ReflectionTimeout == "" || cfg.Learning.ReflectionRetries <= 0 {
		t.Fatalf("reflection defaults must be configured: %+v", cfg.Learning)
	}
	if cfg.Learning.PromptMaxItems <= 0 || cfg.Learning.PromptMaxBytes <= 0 {
		t.Fatalf("prompt defaults must be positive: %+v", cfg.Learning)
	}
	if cfg.Learning.ConsolidationThreshold <= 0 {
		t.Fatalf("consolidation threshold default must be positive: %+v", cfg.Learning)
	}
}

func TestLearningConfigParsesOptInSettings(t *testing.T) {
	writeTestConfig(t, `
board:
  columns:
    - name: Doing
    - name: Review
adapters:
  chimera:
    tiers:
      careful:
        description: careful curation
learning:
  enabled: true
  prompt_enabled: true
  transition_columns: [Review]
  curator_adapter: chimera
  curator_tier: careful
  curator_working_dir: /tmp/legato-learning
  packet_max_items: 40
  packet_max_bytes: 65536
  reflection_timeout: 3m
  reflection_retries: 4
  prompt_max_items: 12
  prompt_max_bytes: 4096
  consolidation_automatic: true
  consolidation_threshold: 25
`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Learning
	if !got.Enabled || !got.PromptEnabled || len(got.TransitionColumns) != 1 || got.TransitionColumns[0] != "Review" {
		t.Fatalf("learning trigger = %+v", got)
	}
	if got.CuratorAdapter != "chimera" || got.CuratorTier != "careful" || got.CuratorWorkingDir != "/tmp/legato-learning" {
		t.Fatalf("curator = %+v", got)
	}
	if got.PacketMaxItems != 40 || got.PacketMaxBytes != 65536 {
		t.Fatalf("packet = %+v", got)
	}
	if got.ReflectionTimeout != "3m" || got.ReflectionRetries != 4 {
		t.Fatalf("reflection = %+v", got)
	}
	if got.PromptMaxItems != 12 || got.PromptMaxBytes != 4096 {
		t.Fatalf("prompt = %+v", got)
	}
	if !got.ConsolidationAutomatic || got.ConsolidationThreshold != 25 {
		t.Fatalf("consolidation = %+v", got)
	}
}

func TestLearningConfigRejectsNonPositiveLimits(t *testing.T) {
	cases := []struct {
		name  string
		yaml  string
		field string
	}{
		{"packet items", "packet_max_items: 0", "learning.packet_max_items"},
		{"packet bytes", "packet_max_bytes: -1", "learning.packet_max_bytes"},
		{"reflection retries", "reflection_retries: 0", "learning.reflection_retries"},
		{"prompt items", "prompt_max_items: 0", "learning.prompt_max_items"},
		{"prompt bytes", "prompt_max_bytes: -1", "learning.prompt_max_bytes"},
		{"consolidation threshold", "consolidation_threshold: 0", "learning.consolidation_threshold"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeTestConfig(t, "learning:\n  "+tc.yaml+"\n")
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("error = %v, want field %s", err, tc.field)
			}
		})
	}
}

func TestLearningConfigRejectsInvalidReflectionTimeout(t *testing.T) {
	writeTestConfig(t, "learning:\n  reflection_timeout: eventually\n")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "learning.reflection_timeout") {
		t.Fatalf("error = %v", err)
	}
}

func TestLearningConfigFallsBackToSwarmDefaultAgent(t *testing.T) {
	writeTestConfig(t, `
board:
  columns:
    - name: Review
swarm:
  default_agent: chimera
adapters:
  chimera: {}
learning:
  enabled: true
  transition_columns: [Review]
`)
	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLearningConfigValidatesConfiguredStructure(t *testing.T) {
	base := `
board:
  columns:
    - name: Review
adapters:
  chimera:
    tiers:
      careful: {}
learning:
  enabled: true
  transition_columns: [Review]
  curator_adapter: chimera
  curator_tier: careful
`
	cases := []struct {
		name  string
		yaml  string
		field string
	}{
		{"missing transitions", strings.Replace(base, "  transition_columns: [Review]\n", "", 1), "learning.transition_columns"},
		{"unknown transition", strings.Replace(base, "[Review]", "[Done]", 1), "learning.transition_columns"},
		{"missing adapter", strings.Replace(base, "  curator_adapter: chimera\n", "", 1), "learning.curator_adapter"},
		{"unknown adapter", strings.Replace(base, "curator_adapter: chimera", "curator_adapter: codex", 1), "learning.curator_adapter"},
		{"unknown tier", strings.Replace(base, "curator_tier: careful", "curator_tier: ghost", 1), "learning.curator_tier"},
		{"relative working dir", base + "  curator_working_dir: relative/path\n", "learning.curator_working_dir"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeTestConfig(t, tc.yaml)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("error = %v, want field %s", err, tc.field)
			}
		})
	}
}

func TestDisabledLearningDoesNotRequireTriggerOrCurator(t *testing.T) {
	writeTestConfig(t, "learning:\n  enabled: false\n")
	if _, err := Load(); err != nil {
		t.Fatalf("disabled learning should load without automation structure: %v", err)
	}
}
