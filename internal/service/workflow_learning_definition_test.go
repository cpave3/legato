package service

import (
	"strings"
	"testing"
)

func TestBuiltInLearningWorkflowUsesInterviewAndConfiguredAgentRun(t *testing.T) {
	definition, err := BuiltInLearningWorkflow(LearningWorkflowOptions{
		Adapter: "chimera", Tier: "careful", WorkingDir: "/tmp/project",
		PacketMaxItems: 25, PacketMaxBytes: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}
	if definition.Source != "builtin:legato-learning" || len(definition.Steps) != 6 {
		t.Fatalf("definition = %#v", definition)
	}
	wantTypes := []string{"agent.interview", "learning.packet.prepare", "agent.stop", "agent.run", "agent.run", "learning.proposals.submit"}
	for i, want := range wantTypes {
		if definition.Steps[i].Type != want {
			t.Fatalf("step %d type = %q, want %q", i, definition.Steps[i].Type, want)
		}
	}
	curator := definition.Steps[3]
	if curator.With["agent"] != "chimera" || curator.With["tier"] != "careful" || curator.With["working_dir"] != "/tmp/project" {
		t.Fatalf("curator config = %#v", curator.With)
	}
	prompt := curator.With["prompt"].(string)
	if !strings.Contains(prompt, "steps.packet.outputs.packet") {
		t.Fatalf("curator prompt = %q", prompt)
	}
	for _, guidance := range []string{
		"Novelty",
		"Future trigger",
		"Transfer",
		"Correct boundary",
		"Actionability",
		"Non-duplication",
		"empty operations array",
	} {
		if !strings.Contains(prompt, guidance) {
			t.Errorf("curator prompt missing %q: %q", guidance, prompt)
		}
	}
	critic := definition.Steps[4]
	criticPrompt := critic.With["prompt"].(string)
	if critic.With["agent"] != "chimera" || critic.With["tier"] != "careful" || critic.With["working_dir"] != "/tmp/project" {
		t.Fatalf("critic config = %#v", critic.With)
	}
	for _, reference := range []string{"steps.packet.outputs.packet", "steps.curator.outputs.operations", "untrusted", "empty operations array"} {
		if !strings.Contains(criticPrompt, reference) {
			t.Errorf("critic prompt missing %q: %q", reference, criticPrompt)
		}
	}
	if got := definition.Steps[5].With["operations"]; got != "${{ steps.critic.outputs.operations }}" {
		t.Fatalf("submit operations = %v, want critic output", got)
	}
}

func TestBuiltInLearningWorkflowUsesTaskWorkingDirectoryWhenNoOverride(t *testing.T) {
	definition, err := BuiltInLearningWorkflow(LearningWorkflowOptions{Adapter: "chimera", PacketMaxItems: 25, PacketMaxBytes: 8192})
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{3, 4} {
		if got := definition.Steps[index].With["working_dir"]; got != "${{ workflow.inputs.working_dir }}" {
			t.Fatalf("agent step %d working_dir = %v", index, got)
		}
	}
}

func TestBuiltInConsolidationWorkflowUsesNormalAgentAndScopedPacket(t *testing.T) {
	definition, err := BuiltInConsolidationWorkflow("project", "repo:github.com/acme/widget", "acme/widget", LearningWorkflowOptions{
		Adapter: "chimera", PacketMaxItems: 50, PacketMaxBytes: 16384,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(definition.Steps) != 4 || definition.Steps[1].Type != "agent.run" || definition.Steps[2].Type != "agent.run" || definition.Steps[3].Type != "learning.proposals.submit" {
		t.Fatalf("steps = %#v", definition.Steps)
	}
	project := definition.Steps[0].With["project"].(map[string]any)
	if project["key"] != "repo:github.com/acme/widget" {
		t.Fatalf("project = %#v", project)
	}
	prompt := definition.Steps[1].With["prompt"].(string)
	if !strings.Contains(prompt, "Consolidate") || !strings.Contains(prompt, "do not make human acceptance decisions") {
		t.Fatalf("prompt = %q", prompt)
	}
	if got := definition.Steps[3].With["operations"]; got != "${{ steps.critic.outputs.operations }}" {
		t.Fatalf("consolidation submit operations = %v, want critic output", got)
	}
}

func TestBuiltInConsolidationWorkflowValidatesScope(t *testing.T) {
	options := LearningWorkflowOptions{Adapter: "chimera", PacketMaxItems: 1, PacketMaxBytes: 1024}
	if _, err := BuiltInConsolidationWorkflow("all", "", "", options); err == nil {
		t.Fatal("expected invalid scope error")
	}
	if _, err := BuiltInConsolidationWorkflow("project", "", "", options); err == nil {
		t.Fatal("expected missing project error")
	}
}
