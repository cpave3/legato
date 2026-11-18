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
