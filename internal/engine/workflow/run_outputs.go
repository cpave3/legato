package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// A workflow's own outputs (spec LEG-482 §4; LEG-489): outputs: maps names
// to {type, value, description}. When a run succeeds its outputs are
// computed from its inputs, its task and its steps' outputs
// (ResolveOutputs), kept on the run, and returned by the workflow.run step
// that called it. A loop's pass results (LEG-490) are mapped the same way
// (resolveOutputMap, in any scope a pass reads).

// WorkflowOutput is one of a workflow's outputs.
type WorkflowOutput struct {
	Type        string `yaml:"type" json:"type"`
	Value       any    `yaml:"value" json:"value"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// WorkflowOutputTypes are the types a workflow output may have.
var WorkflowOutputTypes = []string{"string", "integer", "boolean", "json"}

// outputProblems checks the workflow's outputs against its inputs and
// every step's readable outputs.
func (d *Definition) outputProblems(steps map[string]map[string]string) []*PathError {
	var problems []*PathError
	for _, key := range sortedKeys(d.Outputs) {
		output := d.Outputs[key]
		at := "outputs." + key
		switch {
		case d.SchemaVersion == SchemaVersion:
			problems = append(problems, within(at, fmt.Errorf("outputs is a schema_version 2 key")))
		case !validOutputType(output.Type):
			problems = append(problems, within(at+".type", fmt.Errorf("output %q has unsupported type %q: string, integer, boolean or json", key, output.Type)))
		case output.Value == nil:
			problems = append(problems, within(at, fmt.Errorf("output %q needs a value", key)))
		default:
			if err := validateReferences(output.Value, d.Inputs, nil, steps); err != nil {
				problems = append(problems, within(at+".value", fmt.Errorf("output %q: %w", key, err)))
			}
		}
	}
	return problems
}

func validOutputType(outputType string) bool {
	for _, known := range WorkflowOutputTypes {
		if outputType == known {
			return true
		}
	}
	return false
}

// ResolveOutputs computes the workflow's outputs once its run has
// succeeded: from its inputs, its task (nil when there is none) and the
// outputs of its steps that succeeded or failed under continue. An output
// that is missing, fails to resolve or resolves to a value of another
// type is left out, and each one is named in a warning.
func (d *Definition) ResolveOutputs(inputs, workflowTask map[string]any, steps map[string]map[string]any) (map[string]any, []string) {
	return resolveOutputMap(d.Outputs, resolveScope{inputs: inputs, task: workflowTask, outputs: steps})
}

// resolveOutputMap resolves declared outputs in a scope: those that
// resolve, and a warning for each that does not.
func resolveOutputMap(declared map[string]WorkflowOutput, scope resolveScope) (map[string]any, []string) {
	resolved := make(map[string]any, len(declared))
	var warnings []string
	for _, key := range sortedKeys(declared) {
		output := declared[key]
		value, err := scope.value(output.Value)
		switch {
		case errors.Is(err, errMissing):
			warnings = append(warnings, fmt.Sprintf("output %q was not resolved: %s is missing", key, describeValue(output.Value)))
		case err != nil:
			warnings = append(warnings, fmt.Sprintf("output %q was not resolved: %v", key, err))
		case !valueOfType(output.Type, value):
			warnings = append(warnings, fmt.Sprintf("output %q was not resolved: %s is not of type %s", key, describeValue(output.Value), output.Type))
		default:
			resolved[key] = value
		}
	}
	return resolved, warnings
}

func describeValue(value any) string {
	if text, ok := value.(string); ok {
		if inner, whole := wholeExpression(text); whole {
			return inner
		}
		return fmt.Sprintf("%q", text)
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// valueOfType reports whether a resolved value is of a declared type.
func valueOfType(valueType string, value any) bool {
	switch valueType {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "integer":
		number, ok := numberOf(value)
		return ok && number == math.Trunc(number)
	}
	return true
}
