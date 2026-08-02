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
	if definition.Source != "builtin:legato-learning" || len(definition.Steps) != 4 {
		t.Fatalf("definition = %#v", definition)
	}
	if definition.Steps[0].Type != "agent.interview" || definition.Steps[1].Type != "learning.packet.prepare" || definition.Steps[2].Type != "agent.run" || definition.Steps[3].Type != "learning.proposals.submit" {
		t.Fatalf("steps = %#v", definition.Steps)
	}
	curator := definition.Steps[2]
	if curator.With["agent"] != "chimera" || curator.With["tier"] != "careful" || curator.With["working_dir"] != "/tmp/project" {
		t.Fatalf("curator config = %#v", curator.With)
	}
	prompt := curator.With["prompt"].(string)
	if !strings.Contains(prompt, "steps.packet.outputs.packet") {
		t.Fatalf("curator prompt = %q", prompt)
	}
	for _, guidance := range []string{
		"change a decision on a future task",
		"user-global, repository-wide, or module-specific",
		"Do not create task recaps",
		"implementation detail tied only to this task",
	} {
		if !strings.Contains(prompt, guidance) {
			t.Errorf("curator prompt missing %q: %q", guidance, prompt)
		}
	}
}

func TestBuiltInLearningWorkflowUsesTaskWorkingDirectoryWhenNoOverride(t *testing.T) {
	definition, err := BuiltInLearningWorkflow(LearningWorkflowOptions{Adapter: "chimera", PacketMaxItems: 25, PacketMaxBytes: 8192})
	if err != nil {
		t.Fatal(err)
	}
	if got := definition.Steps[2].With["working_dir"]; got != "${{ workflow.inputs.working_dir }}" {
		t.Fatalf("curator working_dir = %v", got)
	}
}

func TestBuiltInConsolidationWorkflowUsesNormalAgentAndScopedPacket(t *testing.T) {
	definition, err := BuiltInConsolidationWorkflow("project", "repo:github.com/acme/widget", "acme/widget", LearningWorkflowOptions{
		Adapter: "chimera", PacketMaxItems: 50, PacketMaxBytes: 16384,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(definition.Steps) != 3 || definition.Steps[1].Type != "agent.run" {
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
