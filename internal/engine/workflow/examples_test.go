package workflow

import (
	"strings"
	"testing"
)

func TestEveryEmbeddedExampleValidates(t *testing.T) {
	examples := Examples()
	if len(examples) == 0 {
		t.Fatal("no examples")
	}
	for _, example := range examples {
		definition, err := Parse(example.Name+".workflow.yaml", example.YAML)
		if err != nil {
			t.Fatalf("%s: %v", example.Name, err)
		}
		if definition.Name != example.Name {
			t.Fatalf("%s is named %q inside", example.Name, definition.Name)
		}
	}
}

func TestAnEmptyNumberOrBooleanInputTakesItsDefault(t *testing.T) {
	example := OldReviewPassesCopy()
	definition, err := Parse("review-pr-passes.workflow.yaml", example.YAML)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := definition.NormalizeInputs(map[string]string{"repo": "acme/widget", "pr_number": "12", "max_passes": "", "agent_author": " "})
	if err != nil {
		t.Fatal(err)
	}
	if inputs["max_passes"] != 0 || inputs["agent_author"] != false {
		t.Fatalf("inputs = %#v", inputs)
	}
	if _, err := definition.NormalizeInputs(map[string]string{"repo": "acme/widget", "pr_number": ""}); err == nil {
		t.Fatal("an empty required number was taken")
	}
}

func TestTheBundledTemplatesArePausedAndNeedNoExtraTools(t *testing.T) {
	for _, name := range []string{"triage", "review-pr", "discuss-pr", "address-feedback", "ticket-to-merged-pr", "learn"} {
		example, ok := ExampleNamed(name)
		if !ok {
			t.Fatalf("%s is not bundled", name)
		}
		definition, err := Parse(name+".workflow.yaml", example.YAML)
		if err != nil {
			t.Fatal(err)
		}
		for _, trigger := range definition.On.List() {
			if trigger.EnabledByDefault {
				t.Errorf("%s: trigger %s is not enabled: false", name, trigger.Kind)
			}
		}
		for _, step := range definition.Steps {
			if step.Type == "github.pr.prepare" || step.Type == "worktree.remove" {
				t.Errorf("%s: step %s needs Yggdrasil", name, step.ID)
			}
			// agent: {from: <step>} names an earlier step's agent, not a tool.
			if _, from := AgentFrom(step.With["agent"]); step.With["agent"] != nil && !from {
				t.Errorf("%s: step %s names an agent tool; templates use the default", name, step.ID)
			}
			if _, ok := step.With["tier"]; ok {
				t.Errorf("%s: step %s names a tier", name, step.ID)
			}
		}
		if _, ok := definition.Inputs["primary_dir"]; ok && name != "triage" {
			t.Errorf("%s declares primary_dir; it works in a fresh clone", name)
		}
	}
}

func TestTheLearnTemplateReflectsCuratesAndCritiquesWhenACardEntersReview(t *testing.T) {
	example, ok := ExampleNamed("learn")
	if !ok {
		t.Fatal("learn is not bundled")
	}
	definition, err := Parse("learn.workflow.yaml", example.YAML)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, step := range definition.Steps {
		types = append(types, step.Type)
	}
	want := []string{"agent.interview", "learning.packet.prepare", "agent.run", "agent.run", "learning.proposals.submit"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("steps = %v, want %v", types, want)
	}
	trigger := definition.On.TaskEnteredColumn
	if trigger == nil || trigger.Column != "Review" {
		t.Fatalf("trigger = %#v", trigger)
	}
	if trigger.Enabled == nil || *trigger.Enabled {
		t.Fatal("the trigger is not enabled: false")
	}
	if !definition.UsesLearning() {
		t.Fatal("the learn template does not use Learn steps")
	}
	ownRun, err := trigger.Matches(map[string]any{"metadata": map[string]any{"legato": map[string]any{"learning_workflow": true}}})
	if err != nil || ownRun {
		t.Fatalf("a learning run's own task matched: %v, %v", ownRun, err)
	}
	card, err := trigger.Matches(map[string]any{"metadata": map[string]any{}})
	if err != nil || !card {
		t.Fatalf("an ordinary card did not match: %v, %v", card, err)
	}
}
