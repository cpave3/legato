package workflow

import (
	"strings"
	"testing"
)

func TestTheLearnTemplateForAColumnCarriesTheOldSettings(t *testing.T) {
	data, err := LearnTemplateFor("learn-done", "Done", LearnTemplateOptions{Agent: "codex", Tier: "large", WorkingDir: "/srv/notes", MaxItems: 20, MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := Parse("learn-done.workflow.yaml", data)
	if err != nil {
		t.Fatal(err)
	}
	trigger := definition.On.TaskEnteredColumn
	if !strings.Contains(definition.Description, "When a card enters Done") {
		t.Errorf("description = %q", definition.Description)
	}
	if definition.Name != "learn-done" || trigger.Column != "Done" {
		t.Fatalf("name %q, column %q", definition.Name, trigger.Column)
	}
	if trigger.Enabled == nil || *trigger.Enabled {
		t.Fatal("the file switches its trigger on; only this Legato's switch may")
	}
	for _, id := range []string{"curator", "critic"} {
		step := stepNamed(definition, id)
		if step.With["agent"] != "codex" || step.With["tier"] != "large" || step.With["working_dir"] != "/srv/notes" {
			t.Errorf("%s with = %v", id, step.With)
		}
	}
	limits := stepNamed(definition, "packet").With["limits"].(map[string]any)
	if limits["max_items"] != 20 || limits["max_bytes"] != 4096 {
		t.Errorf("limits = %v", limits)
	}
	if !strings.Contains(string(data), "# A Learn run's own task never starts another.") {
		t.Error("the template's comments were lost")
	}
}

func TestTheLearnTemplateForAColumnLeavesWhatWasNotSet(t *testing.T) {
	data, err := LearnTemplateFor("learn", "Review", LearnTemplateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	definition, err := Parse("learn.workflow.yaml", data)
	if err != nil {
		t.Fatal(err)
	}
	example, _ := ExampleNamed("learn")
	template, _ := Parse("learn.workflow.yaml", example.YAML)
	for _, id := range []string{"curator", "critic"} {
		step := stepNamed(definition, id)
		if _, ok := step.With["agent"]; ok {
			t.Errorf("%s names an agent tool", id)
		}
		if _, ok := step.With["tier"]; ok {
			t.Errorf("%s names a tier", id)
		}
		if step.With["working_dir"] != stepNamed(template, id).With["working_dir"] {
			t.Errorf("%s working_dir = %v", id, step.With["working_dir"])
		}
	}
	if got, want := stepNamed(definition, "packet").With["limits"], stepNamed(template, "packet").With["limits"]; got.(map[string]any)["max_items"] != want.(map[string]any)["max_items"] {
		t.Errorf("limits = %v, want %v", got, want)
	}
}

func TestTheLearnTemplateForAColumnRefusesABadName(t *testing.T) {
	if _, err := LearnTemplateFor("Not a name", "Review", LearnTemplateOptions{}); err == nil {
		t.Fatal("a bad name was taken")
	}
	if _, err := LearnTemplateFor("learn", " ", LearnTemplateOptions{}); err == nil {
		t.Fatal("an empty column was taken")
	}
}

func stepNamed(definition *Definition, id string) Step {
	for _, step := range definition.Steps {
		if step.ID == id {
			return step
		}
	}
	return Step{}
}
