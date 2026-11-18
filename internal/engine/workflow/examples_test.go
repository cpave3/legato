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
