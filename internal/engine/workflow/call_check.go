package workflow

import (
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// CallDiagnostics checks d's workflow.run steps against the workflows
// Legato finds (others; one named as d stands for d's file before this
// edit and is passed over): the call graph across the files. A call that
// leads back to d, or nests deeper than MaxCallDepth, is an error; a
// workflow that is not found, an input the child does not declare or
// requires and is not given, and an output read that the child does not
// declare are warnings, since the child may change. The run checks the
// same again as it calls (spec LEG-482 §1, LEG-489).
func CallDiagnostics(data []byte, d *Definition, others []*Definition) []Diagnostic {
	var root yaml.Node
	if d == nil || yaml.Unmarshal(data, &root) != nil {
		return nil
	}
	byName := map[string]*Definition{}
	for _, other := range others {
		if other != nil && other.Name != d.Name {
			byName[other.Name] = other
		}
	}
	byName[d.Name] = d
	var out []Diagnostic
	add := func(severity Severity, path, message string) {
		diagnostic := diagnosticAt(&root, path, message)
		diagnostic.Severity = severity
		out = append(out, diagnostic)
	}
	for i, step := range d.Steps {
		called, ok := CalledWorkflows(step, d.Inputs)
		if step.Type != WorkflowRunStepType || !ok {
			continue
		}
		at := fmt.Sprintf("steps[%d].with.workflow", i)
		for _, name := range called {
			child, found := byName[name]
			if !found {
				add(SeverityWarning, at, fmt.Sprintf("step %q runs workflow %q, which is not among the workflows Legato finds", step.ID, name))
				continue
			}
			if chain := callChainBack(name, d.Name, byName, nil); chain != nil {
				add(SeverityError, at, fmt.Sprintf("step %q: calling %q leads back to %q (%s): a workflow may not call itself, directly or through another", step.ID, name, d.Name, strings.Join(append([]string{d.Name}, chain...), " → ")))
				continue
			}
			if depth := 1 + callDepth(name, byName, nil); depth > MaxCallDepth {
				add(SeverityError, at, fmt.Sprintf("step %q: calls through %q nest deeper than %d", step.ID, name, MaxCallDepth))
			}
			out = append(out, callInputDiagnostics(&root, i, step, child)...)
			out = append(out, callOutputDiagnostics(&root, d, i, step, child)...)
		}
	}
	return out
}

// callsOf are the workflows any of a definition's steps may run.
func callsOf(d *Definition) []string {
	var calls []string
	for _, step := range d.Steps {
		if step.Type != WorkflowRunStepType {
			continue
		}
		called, _ := CalledWorkflows(step, d.Inputs)
		calls = append(calls, called...)
	}
	return calls
}

// callChainBack is a chain of calls from name that reaches target, name
// first and target last, or nil.
func callChainBack(name, target string, byName map[string]*Definition, visiting []string) []string {
	if name == target {
		return []string{name}
	}
	definition, ok := byName[name]
	if !ok || slices.Contains(visiting, name) {
		return nil
	}
	for _, next := range callsOf(definition) {
		if chain := callChainBack(next, target, byName, append(visiting, name)); chain != nil {
			return append([]string{name}, chain...)
		}
	}
	return nil
}

// callDepth is the longest chain of calls from name, in calls; a cycle
// away from the definition checked is cut where it repeats.
func callDepth(name string, byName map[string]*Definition, visiting []string) int {
	definition, ok := byName[name]
	if !ok || slices.Contains(visiting, name) {
		return 0
	}
	deepest := 0
	for _, next := range callsOf(definition) {
		deepest = max(deepest, 1+callDepth(next, byName, append(visiting, name)))
	}
	return deepest
}

func callInputDiagnostics(root *yaml.Node, index int, step Step, child *Definition) []Diagnostic {
	var out []Diagnostic
	given, _ := step.With["inputs"].(map[string]any)
	warn := func(path, message string) {
		diagnostic := diagnosticAt(root, path, message)
		diagnostic.Severity = SeverityWarning
		out = append(out, diagnostic)
	}
	for _, key := range sortedKeys(given) {
		if _, declared := child.Inputs[key]; !declared {
			warn(fmt.Sprintf("steps[%d].with.inputs.%s", index, key), fmt.Sprintf("step %q passes input %q, which workflow %q does not declare", step.ID, key, child.Name))
		}
	}
	for _, key := range sortedKeys(child.Inputs) {
		input := child.Inputs[key]
		if _, passed := given[key]; !passed && input.Required && input.Default == nil {
			warn(fmt.Sprintf("steps[%d].with", index), fmt.Sprintf("step %q does not pass input %q, which workflow %q requires", step.ID, key, child.Name))
		}
	}
	return out
}

func callOutputDiagnostics(root *yaml.Node, d *Definition, index int, step Step, child *Definition) []Diagnostic {
	known := map[string]bool{}
	for name := range child.Outputs {
		known[name] = true
	}
	for name := range readableOutputs(step) {
		known[name] = true
	}
	read := map[string]bool{}
	for _, later := range d.Steps[index+1:] {
		for name := range stepOutputsUsed(later)[step.ID] {
			read[name] = true
		}
	}
	outputs := map[string]any{}
	for key, output := range d.Outputs {
		outputs[key] = output.Value
	}
	for name := range stepOutputsUsed(Step{With: outputs})[step.ID] {
		read[name] = true
	}
	var out []Diagnostic
	for _, name := range sortedKeys(read) {
		if known[name] {
			continue
		}
		diagnostic := diagnosticAt(root, fmt.Sprintf("steps[%d].with.workflow", index), fmt.Sprintf("output %q of step %q is read, and workflow %q does not declare it", name, step.ID, child.Name))
		diagnostic.Severity = SeverityWarning
		out = append(out, diagnostic)
	}
	return out
}
