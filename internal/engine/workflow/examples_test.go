package workflow

import "testing"

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
	if _, ok := ExampleNamed("review-pr-passes"); !ok {
		t.Fatal("review-pr-passes is not an example")
	}
}

func TestAnEmptyNumberOrBooleanInputTakesItsDefault(t *testing.T) {
	example, _ := ExampleNamed("review-pr-passes")
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
	for _, name := range []string{"triage", "review-pr", "discuss-pr", "address-feedback"} {
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
			if _, ok := step.With["agent"]; ok {
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
