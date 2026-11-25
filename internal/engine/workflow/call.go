package workflow

import (
	"fmt"
	"strings"
)

// Calling a workflow (spec LEG-482 §1 "Calling a workflow"; LEG-489): a
// workflow.run step starts another workflow as a child run and waits for
// it to end. Its outputs are the child's declared outputs, with status and
// run_id. Which workflow it runs is a literal name, or an input with
// choices, so the call graph is known from the files alone
// (CallDiagnostics).

// WorkflowRunStepType is the step that runs another workflow.
const WorkflowRunStepType = "workflow.run"

// MaxCallDepth is how deep calls may nest: a run has at most this many
// ancestors.
const MaxCallDepth = 5

// anyOutput stands, in a step's readable outputs, for every output name a
// workflow.run step's child may declare: they are known only from the
// child's file, which CallDiagnostics reads.
const anyOutput = "*"

var workflowRunSpec = ExecutorSpec{
	Type: WorkflowRunStepType, Group: "Control", Title: "Run a workflow",
	Description: "Run another workflow as a child run, and wait for it to end. The step's outputs are the child's declared outputs, with status and run_id. A child that fails, stops or is cancelled fails the step; a gate in the child keeps it waiting. Retry resumes the same child.",
	With: []Field{
		{Name: "workflow", Kind: KindString, Label: "Workflow", Description: "The workflow's name, or ${{ workflow.inputs.<name> }} of an input with choices", Required: true, Essential: true, Order: 1},
		{Name: "inputs", Kind: KindJSON, Label: "Its inputs", Description: "The child's inputs by name, as typed values: a repository input is passed as the resolved repository, and one that resolves to missing is not passed", Essential: true, Order: 2},
	},
	Summary: []string{"workflow"},
	Outputs: []OutputSpec{
		{Name: "status", Type: "string", Description: "The child run's status: succeeded; with on_failure: continue, failed, cancelled or stopped"},
		{Name: "run_id", Type: "string", Description: "The child run"},
	},
}

// Calls says whether a step of the definition runs another workflow.
func (d *Definition) Calls() bool {
	for _, step := range d.Steps {
		if step.Type == WorkflowRunStepType {
			return true
		}
	}
	return false
}

// checkWorkflowRunStep checks what a workflow.run step names: the
// workflow, as a literal or an input with choices, and its inputs, a map.
func checkWorkflowRunStep(step Step, inputs map[string]Input) error {
	if step.Type != WorkflowRunStepType {
		return nil
	}
	if _, ok := CalledWorkflows(step, inputs); !ok {
		return within("with.workflow", fmt.Errorf("with workflow must be a workflow's name, or ${{ workflow.inputs.<name> }} of an input with choices"))
	}
	if value, ok := step.With["inputs"]; ok && value != nil {
		if _, isMap := value.(map[string]any); !isMap {
			return within("with.inputs", fmt.Errorf("with inputs must be a map of the workflow's inputs"))
		}
	}
	return nil
}

// CalledWorkflows are the workflows a workflow.run step may run: its
// literal name, or each choice of the input it names. ok is false when
// with.workflow is neither.
func CalledWorkflows(step Step, inputs map[string]Input) ([]string, bool) {
	text, isText := step.With["workflow"].(string)
	if !isText || strings.TrimSpace(text) == "" {
		return nil, false
	}
	if !HasExpression(text) {
		return []string{strings.TrimSpace(text)}, true
	}
	inner, whole := wholeExpression(text)
	if !whole {
		return nil, false
	}
	expression, err := ParseExpression(inner)
	if err != nil || len(expression.Operands) != 1 || expression.Operands[0].IsLiteral {
		return nil, false
	}
	path := expression.Operands[0].Path
	names := pathNames(path)
	if len(names) != 3 || len(path) != 3 || names[0] != "workflow" || names[1] != "inputs" {
		return nil, false
	}
	input, declared := inputs[names[2]]
	if !declared || len(input.Choices) == 0 {
		return nil, false
	}
	called := make([]string, 0, len(input.Choices))
	for _, choice := range input.Choices {
		name, ok := choice.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return nil, false
		}
		called = append(called, name)
	}
	return called, true
}

// NormalizeValues is NormalizeInputs for typed values, as a workflow.run
// step passes them: unknown inputs are refused, defaults fill those not
// given, and each value must be of its input's type. A task is its ID (or
// a task as a step resolves it, with an id); a repository is a preset's
// name, or the object a run resolved one to, so a custom repository
// survives the call. The run resolves both as it does a form's.
func (d *Definition) NormalizeValues(raw map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(d.Inputs))
	for key := range raw {
		if _, ok := d.Inputs[key]; !ok {
			return nil, fmt.Errorf("unknown workflow input %q", key)
		}
	}
	for key, spec := range d.Inputs {
		value, supplied := raw[key]
		if !supplied || value == nil {
			if spec.Default != nil {
				out[key] = spec.Default
				continue
			}
			if spec.Required {
				return nil, fmt.Errorf("workflow input %q is required", key)
			}
			continue
		}
		normalized, err := typedValue(spec.Type, value)
		if err != nil {
			return nil, fmt.Errorf("workflow input %q: %w", key, err)
		}
		if len(spec.Choices) > 0 && !choiceOf(spec.Choices, normalized) {
			return nil, fmt.Errorf("workflow input %q: %v is not one of its choices", key, normalized)
		}
		out[key] = normalized
	}
	return out, nil
}

func choiceOf(choices []any, value any) bool {
	for _, choice := range choices {
		if ValuesEqual(choice, value) {
			return true
		}
	}
	return false
}

// typedValue checks a value against an input type.
func typedValue(valueType string, value any) (any, error) {
	switch valueType {
	case "string":
		if text, ok := value.(string); ok {
			return text, nil
		}
		return nil, fmt.Errorf("must be text")
	case "integer":
		number, ok := numberOf(value)
		if !ok || number != float64(int64(number)) {
			return nil, fmt.Errorf("must be an integer")
		}
		return int64(number), nil
	case "boolean":
		if boolean, ok := value.(bool); ok {
			return boolean, nil
		}
		return nil, fmt.Errorf("must be true or false")
	case "json":
		return value, nil
	case "task":
		switch task := value.(type) {
		case string:
			if strings.TrimSpace(task) != "" {
				return task, nil
			}
		case map[string]any:
			if id, ok := task["id"].(string); ok && id != "" {
				return id, nil
			}
		}
		return nil, fmt.Errorf("must be a task or a task ID")
	case "repository":
		switch repository := value.(type) {
		case string:
			if strings.TrimSpace(repository) != "" {
				return repository, nil
			}
		case map[string]any:
			repo, _ := repository["repo"].(string)
			dir, _ := repository["primary_dir"].(string)
			if repo != "" && dir != "" {
				return repository, nil
			}
		}
		return nil, fmt.Errorf("must be a repository preset's name, or a repository with repo and primary_dir")
	}
	return nil, fmt.Errorf("unsupported type %q", valueType)
}
