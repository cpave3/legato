package service

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"gopkg.in/yaml.v3"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// The built-in workflows this package defines match the workflow schema, as
// a file written the same way would.
func TestBuiltInWorkflowsMatchTheSchema(t *testing.T) {
	var schema jsonschema.Schema
	if err := json.Unmarshal(workflowengine.JSONSchema(), &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	options := LearningWorkflowOptions{Adapter: "claude-code", PacketMaxItems: 10, PacketMaxBytes: 1000}
	learning, err := BuiltInLearningWorkflow(options)
	if err != nil {
		t.Fatal(err)
	}
	definitions := []*workflowengine.Definition{BuiltInJobWorkflow(), learning}
	for _, scope := range []string{"user", "project"} {
		consolidation, err := BuiltInConsolidationWorkflow(scope, "LEG", "Legato", options)
		if err != nil {
			t.Fatal(err)
		}
		definitions = append(definitions, consolidation)
	}
	for _, definition := range definitions {
		data, err := yaml.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err := yaml.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(value)
		var instance any
		_ = json.Unmarshal(encoded, &instance)
		if err := resolved.Validate(instance); err != nil {
			t.Errorf("%s: %v", definition.Name, err)
		}
	}
}
