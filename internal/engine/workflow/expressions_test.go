package workflow_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

type expressionFixture struct {
	Definition json.RawMessage `json:"definition"`
	Cases      []struct {
		Location struct {
			Kind   string `json:"kind"`
			Step   int    `json:"step"`
			Output string `json:"output"`
		} `json:"location"`
		Expression string `json:"expression"`
		Valid      bool   `json:"valid"`
	} `json:"cases"`
}

// The table web/src/lib/workflowExpressions.ts checks too, so the editor's
// verdicts and the engine's cannot drift apart.
func TestTheSharedExpressionTable(t *testing.T) {
	data, err := os.ReadFile("testdata/expressions.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture expressionFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	fresh := func() *workflow.Definition {
		var definition workflow.Definition
		if err := json.Unmarshal(fixture.Definition, &definition); err != nil {
			t.Fatal(err)
		}
		return &definition
	}
	if err := fresh().Validate(); err != nil {
		t.Fatalf("the fixture's definition: %v", err)
	}
	for _, tc := range fixture.Cases {
		definition := fresh()
		value := "${{ " + tc.Expression + " }}"
		switch tc.Location.Kind {
		case "with":
			definition.Steps[tc.Location.Step].With["prompt"] = "Look at " + value
		case "run_title":
			definition.RunTitle = "Run " + value
		case "output_path":
			output := definition.Steps[tc.Location.Step].Outputs[tc.Location.Output]
			output.Path = "out/" + value
			definition.Steps[tc.Location.Step].Outputs[tc.Location.Output] = output
		default:
			t.Fatalf("location %q", tc.Location.Kind)
		}
		err := definition.Validate()
		if (err == nil) != tc.Valid {
			t.Errorf("%s in %s: valid = %v, want %v (%v)", tc.Expression, tc.Location.Kind, err == nil, tc.Valid, err)
		}
	}
}
