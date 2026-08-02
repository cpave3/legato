package service

import (
	"strings"
	"testing"
)

func TestBuiltInTriggeredLearningWorkflowsAreStableValidAndPerColumn(t *testing.T) {
	options := LearningWorkflowOptions{Adapter: "chimera", Tier: "large", WorkingDir: "/repo", PacketMaxItems: 25, PacketMaxBytes: 4096}
	definitions, err := BuiltInTriggeredLearningWorkflows([]string{"Review", "Done"}, options)
	if err != nil {
		t.Fatal(err)
	}
	again, err := BuiltInTriggeredLearningWorkflows([]string{"Review", "Done"}, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 2 || definitions[0].Name == definitions[1].Name {
		t.Fatalf("definitions = %+v", definitions)
	}
	for i, definition := range definitions {
		if definition.Name != again[i].Name || definition.On.TaskEnteredColumn.Column == "" {
			t.Fatalf("definition %d is not stable/configured: %+v", i, definition)
		}
		if err := definition.Validate(); err != nil {
			t.Fatalf("definition %d invalid: %v", i, err)
		}
		wantTypes := []string{"agent.interview", "learning.packet.prepare", "agent.run", "learning.proposals.submit"}
		for step, want := range wantTypes {
			if definition.Steps[step].Type != want {
				t.Fatalf("step %d type = %q, want %q", step, definition.Steps[step].Type, want)
			}
		}
		curator := definition.Steps[2]
		if curator.With["working_dir"] != "/repo" {
			t.Fatalf("configured working directory override = %v", curator.With["working_dir"])
		}
		if curator.Outputs["operations"].Type != "json" || !strings.Contains(curator.With["prompt"].(string), "--output operations='<json>'") {
			t.Fatalf("curator lacks declared JSON completion contract: %+v", curator)
		}
		trigger := definition.On.TaskEnteredColumn
		ordinary, err := trigger.Matches(map[string]any{"metadata": map[string]any{}})
		if err != nil || !ordinary {
			t.Fatalf("ordinary task match = %v, %v", ordinary, err)
		}
		recursive, err := trigger.Matches(map[string]any{"metadata": map[string]any{"legato": map[string]any{"learning_workflow": true}}})
		if err != nil || recursive {
			t.Fatalf("recursive task match = %v, %v", recursive, err)
		}
	}
}
