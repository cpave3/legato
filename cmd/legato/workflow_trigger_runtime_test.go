package main

import (
	"testing"

	"github.com/cpave3/legato/config"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestLearningTriggerDiscoveryIsOptIn(t *testing.T) {
	existing := &workflowengine.Definition{Name: "existing"}
	base := workflowengine.DiscoveryResult{Definitions: []*workflowengine.Definition{existing}}

	disabled := injectBuiltInLearningTriggers(base, &config.Config{})
	if len(disabled.Definitions) != 1 || disabled.Definitions[0] != existing {
		t.Fatalf("disabled discovery changed: %+v", disabled.Definitions)
	}

	cfg := &config.Config{Learning: config.LearningConfig{
		Enabled: true, TransitionColumns: []string{"Review", "Done"}, CuratorAdapter: "chimera",
		PacketMaxItems: 20, PacketMaxBytes: 4096,
	}}
	enabled := injectBuiltInLearningTriggers(base, cfg)
	if len(enabled.Errors) != 0 || len(enabled.Definitions) != 3 {
		t.Fatalf("enabled discovery = definitions %d, errors %+v", len(enabled.Definitions), enabled.Errors)
	}
	columns := map[string]bool{}
	for _, definition := range enabled.Definitions {
		if definition.On.TaskEnteredColumn != nil {
			columns[definition.On.TaskEnteredColumn.Column] = true
		}
	}
	if !columns["Review"] || !columns["Done"] {
		t.Fatalf("trigger columns = %+v", columns)
	}
}

func TestLearningTriggerAdapterPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		curator string
		worker  string
		want    string
	}{
		{name: "worker fallback", worker: "chimera", want: "chimera"},
		{name: "curator override", curator: "codex", worker: "chimera", want: "codex"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				Swarm:    config.SwarmConfig{Worker: config.SwarmParticipantConfig{Agent: tc.worker}},
				Learning: config.LearningConfig{Enabled: true, TransitionColumns: []string{"Review"}, CuratorAdapter: tc.curator, PacketMaxItems: 20, PacketMaxBytes: 4096},
			}
			result := injectBuiltInLearningTriggers(workflowengine.DiscoveryResult{}, cfg)
			if len(result.Errors) != 0 || len(result.Definitions) != 1 {
				t.Fatalf("result = definitions %d, errors %+v", len(result.Definitions), result.Errors)
			}
			got, _ := result.Definitions[0].Steps[2].With["agent"].(string)
			if got != tc.want {
				t.Fatalf("curator agent = %q, want %q", got, tc.want)
			}
		})
	}
}
