package main

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
)

// Automatic Learn is the learn workflow's trigger now: discovery finds only
// what the workflow folders hold, whatever the deprecated settings say.
func TestDiscoveryMakesNoLearnWorkflowOfItsOwn(t *testing.T) {
	t.Setenv("LEGATO_CONFIG", t.TempDir()+"/config.yaml")
	t.Chdir(t.TempDir())
	cfg := &config.Config{Learning: config.LearningConfig{
		Enabled: true, TransitionColumns: []string{"Review", "Done"}, CuratorAdapter: "chimera",
		PacketMaxItems: 20, PacketMaxBytes: 4096,
	}}
	result := workflowDiscoveryProvider(cfg, nil)()
	for _, definition := range result.Definitions {
		if strings.HasPrefix(definition.Source, "builtin:legato-learning") || definition.UsesLearning() || definition.On.TaskEnteredColumn != nil {
			t.Errorf("discovery made %s (%s)", definition.Name, definition.Source)
		}
	}
}
