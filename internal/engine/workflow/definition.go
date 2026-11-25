// Package workflow parses and resolves reusable workflow definitions.
package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the version of the published JSON Schema, and of the
// files Legato writes until schema version 2 has its own (LEG-484).
const SchemaVersion = 1

// SchemaVersion2 is the version that nests steps and names how each starts
// with start:. Parse reads a version 1 file into the same model.
const SchemaVersion2 = 2

// How a step starts (Step.Start).
const (
	StartAutomatic = "automatic"
	StartManual    = "manual"
	// StartAgentEnded is internal: only a version 1 file's trigger:
	// agent_ended reads as it. The step waits as a manual one does, and
	// starts by itself when the workflow task's agent ends.
	StartAgentEnded = "agent_ended"
)

type Definition struct {
	SchemaVersion int              `yaml:"schema_version" json:"schema_version"`
	Name          string           `yaml:"name" json:"name"`
	Title         string           `yaml:"title" json:"title"`
	RunTitle      string           `yaml:"run_title,omitempty" json:"run_title,omitempty"`
	Description   string           `yaml:"description,omitempty" json:"description,omitempty"`
	AfterStart    string           `yaml:"after_start,omitempty" json:"after_start,omitempty"`
	Inputs        map[string]Input `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	On            Triggers         `yaml:"on,omitempty" json:"on,omitempty"`
	// ChatHelp lists the workflow in ${{ event.commands }} of the chat
	// triggers on the bots its own chat triggers run on.
	ChatHelp bool   `yaml:"chat_help,omitempty" json:"chat_help,omitempty"`
	Steps    []Step `yaml:"steps" json:"steps"`
	Source   string `yaml:"-" json:"source"`
	// fromFile is set while a file just decoded is checked: only then is
	// start in a version 1 file the file's own key, and refused.
	fromFile bool
}

type Input struct {
	Type        string `yaml:"type" json:"type"`
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
	Default     any    `yaml:"default,omitempty" json:"default,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Choices     []any  `yaml:"choices,omitempty" json:"choices,omitempty"`
	Secret      bool   `yaml:"secret,omitempty" json:"secret,omitempty"`
}

type Step struct {
	ID    string `yaml:"id" json:"id"`
	Title string `yaml:"title" json:"title"`
	Type  string `yaml:"type" json:"type"`
	// Trigger is a version 1 file's key only: Parse reads it into Start and
	// clears it, and a version 1 definition is marshalled with it again.
	Trigger string `yaml:"trigger,omitempty" json:"-"`
	// Start is how the step starts: StartAutomatic, StartManual or, from a
	// version 1 file, StartAgentEnded. Parse fills it.
	Start string `yaml:"start,omitempty" json:"start" legato:"v2"`
	// If is checked before the step runs: when it is false the step is
	// skipped, and its outputs are missing (LEG-486).
	If *Condition `yaml:"if,omitempty" json:"if,omitempty" legato:"v2"`
	// OnFailure is what a failure of the step does: OnFailureFail (the
	// default) fails the run, OnFailureContinue makes the failure the
	// step's outputs and goes on, OnFailurePause holds the run at a gate
	// with Retry, Skip and Stop.
	OnFailure string            `yaml:"on_failure,omitempty" json:"on_failure,omitempty" legato:"v2"`
	With      map[string]any    `yaml:"with" json:"with"`
	Outputs   map[string]Output `yaml:"outputs,omitempty" json:"outputs,omitempty"`
}

// What a step's failure does (Step.OnFailure).
const (
	OnFailureFail     = "fail"
	OnFailureContinue = "continue"
	OnFailurePause    = "pause"
)

// FailureOutputs are the outputs a step with on_failure: continue has when
// it fails: status (failed), error and error_code. A later step may read
// them; they are missing when the step succeeds.
var FailureOutputs = []string{"status", "error", "error_code"}

// Failure is what a step's on_failure says, fail when it says nothing.
func (s Step) Failure() string {
	if s.OnFailure == "" {
		return OnFailureFail
	}
	return s.OnFailure
}

// UnmarshalJSON reads a step as a run keeps it. A run started before schema
// version 2 kept trigger, which reads as start.
func (s *Step) UnmarshalJSON(data []byte) error {
	type plain Step
	var decoded struct {
		plain
		LegacyTrigger string `json:"trigger"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*s = Step(decoded.plain)
	if s.Start == "" {
		s.Start = decoded.LegacyTrigger
	}
	if s.Start == "" {
		s.Start = StartAutomatic
	}
	return nil
}

// MarshalYAML writes the definition as a file of its own schema version: a
// version 1 file says how a step starts with trigger, a version 2 file with
// start, and neither writes automatic.
func (d Definition) MarshalYAML() (any, error) {
	type plain Definition
	out := plain(d)
	out.Steps = make([]Step, len(d.Steps))
	for i, step := range d.Steps {
		start := step.Start
		if start == "" {
			start = step.Trigger
		}
		if start == StartAutomatic {
			start = ""
		}
		step.Trigger, step.Start = "", ""
		if d.SchemaVersion == SchemaVersion {
			step.Trigger = start
		} else {
			step.Start = start
		}
		out.Steps[i] = step
	}
	return out, nil
}

// StepPath is the path of the step id in the scope at parent ("" for the
// run's own steps): a top-level step's path is its ID, a nested one's is
// its scope's path, a slash and its ID.
func StepPath(parent, id string) string {
	if parent == "" {
		return id
	}
	return parent + "/" + id
}

// SplitStepPath is StepPath undone: the scope's path and the step's ID.
func SplitStepPath(path string) (parent, id string) {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i], path[i+1:]
	}
	return "", path
}

type Output struct {
	Type        string `yaml:"type" json:"type"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Path        string `yaml:"path,omitempty" json:"path,omitempty"`
	MediaType   string `yaml:"media_type,omitempty" json:"media_type,omitempty"`
}

type ResolvedStep struct {
	ID      string
	Title   string
	Type    string
	With    map[string]any
	Outputs map[string]Output
}

func Parse(source string, data []byte) (*Definition, error) {
	var definition Definition
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&definition); err != nil {
		return nil, fmt.Errorf("parse workflow %s: %w", source, err)
	}
	definition.Source = source
	definition.fromFile = true
	err := definition.Validate()
	definition.fromFile = false
	if err != nil {
		return nil, fmt.Errorf("validate workflow %s: %w", source, err)
	}
	return &definition, nil
}

// Validate checks the definition and returns its first problem, a
// *PathError naming where it is.
func (d *Definition) Validate() error {
	if problems := d.problems(); len(problems) > 0 {
		return problems[0]
	}
	return nil
}

// problems lists every problem Validate can find, in the order it checks
// them: the first is the error Validate returns. Each step gives at most
// one.
func (d *Definition) problems() []*PathError {
	var problems []*PathError
	add := func(path string, err error) { problems = append(problems, within(path, err)) }
	if d.SchemaVersion != SchemaVersion && d.SchemaVersion != SchemaVersion2 {
		add("schema_version", fmt.Errorf("schema_version must be %d or %d", SchemaVersion, SchemaVersion2))
	}
	if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Title) == "" {
		path := "name"
		if strings.TrimSpace(d.Name) != "" {
			path = "title"
		}
		add(path, fmt.Errorf("name and title are required"))
	}
	if len(d.Steps) == 0 {
		add("steps", fmt.Errorf("at least one step is required"))
	}
	if d.AfterStart != "" && d.AfterStart != "workflow" && d.AfterStart != "agent" && d.AfterStart != "stay" {
		add("after_start", fmt.Errorf("after_start must be workflow, agent, or stay"))
	}
	if d.AfterStart == "agent" {
		taskInputs, requiredTaskInputs := 0, 0
		for _, input := range d.Inputs {
			if input.Type == "task" {
				taskInputs++
				if input.Required {
					requiredTaskInputs++
				}
			}
		}
		if taskInputs != 1 || requiredTaskInputs != 1 {
			add("after_start", fmt.Errorf("after_start agent requires exactly one required task input"))
		}
	}
	if d.On.PRChangesRequested != nil {
		if err := d.On.PRChangesRequested.Validate(TriggerPRChangesRequested, d.Inputs); err != nil {
			add("on."+TriggerPRChangesRequested, err)
		}
	}
	for _, kind := range []string{TriggerChatMessage, TriggerChatMention} {
		if chat := d.On.Chat(kind); chat != nil {
			if err := chat.Validate(kind, d.Inputs); err != nil {
				add("on."+kind, err)
			}
		}
	}
	if d.On.ChatButton != nil {
		if err := d.On.ChatButton.Validate(d.Inputs); err != nil {
			add("on."+TriggerChatButton, err)
		}
	}
	if d.On.Schedule != nil {
		if err := d.On.Schedule.Validate(d.Inputs); err != nil {
			add("on.schedule", err)
		}
	}
	if d.On.TaskEnteredColumn != nil {
		if err := d.On.TaskEnteredColumn.Validate(d.Inputs); err != nil {
			add("on.task.entered_column", err)
		}
	}
	for _, key := range sortedKeys(d.Inputs) {
		if input := d.Inputs[key]; !validValueType(input.Type, false) {
			add("inputs."+key+".type", fmt.Errorf("input %q has unsupported type %q", key, input.Type))
		}
	}
	if readsWorkflowTask(d.RunTitle) {
		add("run_title", fmt.Errorf("run_title: workflow.task is unavailable before the workflow task is created"))
	}
	if err := validateReferences(d.RunTitle, d.Inputs, nil, nil); err != nil {
		add("run_title", fmt.Errorf("run_title: %w", err))
	}
	seen := make(map[string]bool, len(d.Steps))
	availableOutputs := make(map[string]map[string]string, len(d.Steps))
	agentSteps := make(map[string]bool, len(d.Steps))
	for i := range d.Steps {
		step := &d.Steps[i]
		if err := validateStep(step, i, d.SchemaVersion, d.fromFile, seen, d.Inputs, availableOutputs); err != nil {
			problems = append(problems, err)
		} else if err := checkAgentFrom(*step, agentSteps); err != nil {
			problems = append(problems, within(fmt.Sprintf("steps[%d]", i), fmt.Errorf("step %q: %w", step.ID, err)))
		}
		agentSteps[step.ID] = agentStepTypes[step.Type]
		seen[step.ID] = true
		availableOutputs[step.ID] = readableOutputs(*step)
	}
	return problems
}

// validateStep checks one step against the steps before it: its first
// problem, or nil.
func validateStep(step *Step, index, version int, fromFile bool, seen map[string]bool, inputs map[string]Input, availableOutputs map[string]map[string]string) *PathError {
	at := fmt.Sprintf("steps[%d]", index)
	if step.ID == "" || seen[step.ID] {
		return within(at+".id", fmt.Errorf("step IDs must be non-empty and unique"))
	}
	if err := normalizeStart(step, version, fromFile); err != nil {
		return within(at+"."+err.key, err.err)
	}
	if _, known := catalogByType[step.Type]; !known {
		return within(at+".type", fmt.Errorf("step %q has unsupported type %q", step.ID, step.Type))
	}
	if err := validateControl(*step, version, inputs, availableOutputs); err != nil {
		return within(at+"."+err.key, err.err)
	}
	if err := validateStepContract(*step, inputs); err != nil {
		return within(at, fmt.Errorf("step %q: %w", step.ID, err))
	}
	for _, field := range sortedKeys(step.With) {
		if err := validateReferences(step.With[field], inputs, step.Outputs, availableOutputs); err != nil {
			return within(at+".with."+field, fmt.Errorf("step %q field %q: %w", step.ID, field, err))
		}
	}
	for _, key := range sortedKeys(step.Outputs) {
		output := step.Outputs[key]
		if !validValueType(output.Type, true) {
			return within(at+".outputs."+key+".type", fmt.Errorf("step %q output %q has unsupported type %q", step.ID, key, output.Type))
		}
		if output.Type == "file" {
			if output.Path == "" || !safeRelativePath(output.Path) {
				return within(at+".outputs."+key+".path", fmt.Errorf("step %q output %q requires a safe relative path", step.ID, key))
			}
			if err := validateReferences(output.Path, inputs, nil, availableOutputs); err != nil {
				return within(at+".outputs."+key+".path", fmt.Errorf("step %q output %q path: %w", step.ID, key, err))
			}
		}
	}
	return nil
}

func wholeTaskReference(value any, inputs map[string]Input) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	inner, ok := wholeExpression(text)
	if !ok {
		return false
	}
	expression, err := ParseExpression(inner)
	if err != nil || len(expression.Operands) != 1 {
		return false
	}
	path := expression.Operands[0].Path
	parts := pathNames(path)
	if len(parts) != len(path) {
		return false
	}
	if len(parts) == 2 && parts[0] == "workflow" && parts[1] == "task" {
		return true
	}
	if len(parts) != 3 || parts[0] != "workflow" || parts[1] != "inputs" {
		return false
	}
	input, ok := inputs[parts[2]]
	return ok && input.Type == "task"
}

// readsWorkflowTask reports whether text has a ${{ }} that reads
// workflow.task.
func readsWorkflowTask(text string) bool {
	for _, match := range findExpressions(text) {
		expression, err := ParseExpression(match.inner)
		if err != nil {
			continue
		}
		for _, operand := range expression.Operands {
			if names := pathNames(operand.Path); len(names) >= 2 && names[0] == "workflow" && names[1] == "task" {
				return true
			}
		}
	}
	return false
}

func validValueType(valueType string, file bool) bool {
	return valueType == "string" || valueType == "integer" || valueType == "boolean" || valueType == "json" || (!file && (valueType == "task" || valueType == "repository")) || (file && valueType == "file")
}

func validTaskField(field string) bool { return slices.Contains(TaskFields, field) }

func validRepositoryField(field string) bool { return slices.Contains(RepositoryFields, field) }

func safeRelativePath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validateReferences(value any, inputs map[string]Input, current map[string]Output, previous map[string]map[string]string) error {
	switch nested := value.(type) {
	case map[string]any:
		for _, key := range sortedKeys(nested) {
			if err := validateReferences(nested[key], inputs, current, previous); err != nil {
				return err
			}
		}
		return nil
	case []any:
		for _, item := range nested {
			if err := validateReferences(item, inputs, current, previous); err != nil {
				return err
			}
		}
		return nil
	}
	text, ok := value.(string)
	if !ok {
		return nil
	}
	_, whole := wholeExpression(text)
	for _, match := range findExpressions(text) {
		expression, err := ParseExpression(match.inner)
		if err != nil {
			return err
		}
		types := make([]string, len(expression.Operands))
		for i, operand := range expression.Operands {
			if operand.IsLiteral {
				types[i] = literalType(operand.Literal)
				continue
			}
			if types[i], err = pathType(operand.Path, inputs, current, previous); err != nil {
				return err
			}
		}
		if whole && len(types) > 1 {
			// A whole value keeps its type, so both sides of ?? must have
			// the same one; within longer text they are rendered as text.
			for _, other := range types[1:] {
				if !typesMatch(types[0], other) {
					return fmt.Errorf("%s: both sides of ?? must have the same type, not %s and %s", match.inner, types[0], other)
				}
			}
		}
	}
	return nil
}

// literalType is the declared type a literal matches; "" (null) matches
// any.
func literalType(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case int64:
		return "integer"
	}
	return ""
}

func typesMatch(a, b string) bool {
	return a == "" || b == "" || a == "json" || b == "json" || a == b
}

// pathType checks that a path can be read where its references are, and
// says its declared type: string, integer, boolean, json, task,
// repository or file.
func pathType(path []PathPart, inputs map[string]Input, current map[string]Output, previous map[string]map[string]string) (string, error) {
	parts := pathNames(path)
	rest := path[len(parts):]
	expr := pathString(path)
	taskPath := func(fields []string, rest []PathPart, describe func(string) error) (string, error) {
		if !validTaskField(fields[0]) || (fields[0] != "metadata" && (len(fields) > 1 || len(rest) > 0)) {
			return "", describe(pathString(append(namesOf(fields), rest...)))
		}
		if fields[0] == "metadata" {
			return "json", nil
		}
		return "string", nil
	}
	switch {
	case len(parts) == 2 && parts[0] == "workflow" && parts[1] == "task":
		if len(rest) > 0 {
			return "", fmt.Errorf("workflow task has no field path %q", pathString(rest))
		}
		return "task", nil
	case len(parts) >= 3 && parts[0] == "workflow" && parts[1] == "task":
		return taskPath(parts[2:], rest, func(field string) error {
			return fmt.Errorf("workflow task has no field path %q", field)
		})
	case len(parts) >= 3 && parts[0] == "workflow" && parts[1] == "inputs":
		input, ok := inputs[parts[2]]
		if !ok {
			return "", fmt.Errorf("input %q is not declared", parts[2])
		}
		fields := parts[3:]
		if len(fields) == 0 && len(rest) == 0 {
			return input.Type, nil
		}
		noField := fmt.Errorf("input %q has no field path %q", parts[2], strings.TrimPrefix(pathString(path[3:]), "."))
		switch {
		case input.Type == "json":
			return "json", nil
		case input.Type == "task" && len(fields) > 0:
			return taskPath(fields, rest, func(string) error { return noField })
		case input.Type == "repository" && len(fields) == 1 && len(rest) == 0 && validRepositoryField(fields[0]):
			return "string", nil
		}
		return "", noField
	case len(parts) >= 4 && parts[0] == "steps" && parts[2] == "outputs":
		outputs, ok := previous[parts[1]]
		if !ok {
			return "", fmt.Errorf("step %q is not an earlier step", parts[1])
		}
		outputType, ok := outputs[parts[3]]
		if !ok {
			return "", fmt.Errorf("step %q output %q is not declared", parts[1], parts[3])
		}
		after := path[4:]
		switch {
		case len(after) == 0:
			return outputType, nil
		case len(after) == 1 && !after[0].IsIndex && after[0].Name == "path":
			return "string", nil
		case outputType == "json":
			return "json", nil
		}
		return "", fmt.Errorf("output %q has no field %q", parts[3], strings.TrimPrefix(pathString(after), "."))
	case len(parts) == 3 && len(rest) == 0 && parts[0] == "outputs" && parts[2] == "path":
		if output, ok := current[parts[1]]; !ok || output.Type != "file" {
			return "", fmt.Errorf("file output %q is not declared", parts[1])
		}
		return "string", nil
	}
	return "", fmt.Errorf("unsupported expression %q", expr)
}

func namesOf(names []string) []PathPart {
	parts := make([]PathPart, len(names))
	for i, name := range names {
		parts[i] = PathPart{Name: name}
	}
	return parts
}

func (d *Definition) NormalizeInputs(raw map[string]string) (map[string]any, error) {
	out := make(map[string]any, len(d.Inputs))
	for key := range raw {
		if _, ok := d.Inputs[key]; !ok {
			return nil, fmt.Errorf("unknown workflow input %q", key)
		}
	}
	for key, spec := range d.Inputs {
		value, supplied := raw[key]
		// An empty number or boolean, such as a form's blank field, is
		// one not given: its default applies.
		if supplied && strings.TrimSpace(value) == "" && (spec.Type == "integer" || spec.Type == "boolean") {
			supplied = false
		}
		if !supplied {
			if spec.Default != nil {
				out[key] = spec.Default
				continue
			}
			if spec.Required {
				return nil, fmt.Errorf("workflow input %q is required", key)
			}
			continue
		}
		normalized, err := parseValue(spec.Type, value)
		if err != nil {
			return nil, fmt.Errorf("workflow input %q: %w", key, err)
		}
		out[key] = normalized
	}
	return out, nil
}

func parseValue(valueType, value string) (any, error) {
	switch valueType {
	case "string", "task", "repository":
		return value, nil
	case "integer":
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("must be an integer")
		}
		return parsed, nil
	case "boolean":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("must be a boolean")
		}
		return parsed, nil
	case "json":
		var parsed any
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.UseNumber()
		if err := decoder.Decode(&parsed); err != nil {
			return nil, fmt.Errorf("must be valid JSON: %w", err)
		}
		return parsed, nil
	default:
		return nil, fmt.Errorf("unsupported type %q", valueType)
	}
}

// ErrMissing is a whole value that reads nothing: an input not given, a
// skipped step's output, a field or an item that is not there. A with
// field whose whole value is missing is left out.
var ErrMissing = errMissing

// ResolveFromInputs resolves a value that refers only to workflow inputs. A
// value that refers to anything else (a step's outputs, the workflow task)
// returns an error, and a whole value that is missing ErrMissing.
func ResolveFromInputs(value any, inputs map[string]any) (any, error) {
	return resolveScope{inputs: inputs}.value(value)
}

func (d *Definition) ResolveRunTitle(inputs map[string]any) (string, error) {
	if d.RunTitle == "" {
		return d.Title, nil
	}
	resolved, err := resolveScope{inputs: inputs}.value(d.RunTitle)
	if err != nil && !errors.Is(err, errMissing) {
		return "", fmt.Errorf("resolve run_title: %w", err)
	}
	title, ok := resolved.(string)
	if err != nil || !ok || strings.TrimSpace(title) == "" {
		return "", fmt.Errorf("resolved run_title must be a non-empty string")
	}
	return title, nil
}

// step is the step of that ID, or nil.
func (d *Definition) step(stepID string) *Step {
	for i := range d.Steps {
		if d.Steps[i].ID == stepID {
			return &d.Steps[i]
		}
	}
	return nil
}

func (d *Definition) ResolveStep(stepID string, inputs, workflowTask map[string]any, outputs map[string]map[string]any) (*ResolvedStep, error) {
	selected := d.step(stepID)
	if selected == nil {
		return nil, fmt.Errorf("workflow step %q not found", stepID)
	}
	scope := resolveScope{inputs: inputs, task: workflowTask, outputs: outputs}
	resolvedOutputs := make(map[string]Output, len(selected.Outputs))
	for key, output := range selected.Outputs {
		if output.Type == "file" {
			resolved, err := scope.value(output.Path)
			if err != nil {
				return nil, fmt.Errorf("step %q output %q path: %w", stepID, key, err)
			}
			path, ok := resolved.(string)
			if !ok || !safeRelativePath(path) {
				return nil, fmt.Errorf("step %q output %q resolved to an unsafe path", stepID, key)
			}
			output.Path = path
		}
		resolvedOutputs[key] = output
	}
	scope.current = resolvedOutputs
	with := make(map[string]any, len(selected.With))
	for key, value := range selected.With {
		resolved, err := scope.value(value)
		if errors.Is(err, errMissing) {
			// A whole value that is missing leaves the field out.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("step %q field %q: %w", stepID, key, err)
		}
		with[key] = resolved
	}
	return &ResolvedStep{ID: selected.ID, Title: selected.Title, Type: selected.Type, With: with, Outputs: resolvedOutputs}, nil
}

// StepRuns checks a step's if: against the run so far: false when the
// step is to be skipped. A step with no if: runs.
func (d *Definition) StepRuns(stepID string, inputs, workflowTask map[string]any, outputs map[string]map[string]any) (bool, error) {
	selected := d.step(stepID)
	if selected == nil {
		return false, fmt.Errorf("workflow step %q not found", stepID)
	}
	if selected.If == nil {
		return true, nil
	}
	scope := resolveScope{inputs: inputs, task: workflowTask, outputs: outputs}
	runs, err := selected.If.evaluate(func(field string) (any, bool, error) {
		path, err := ParsePath(field)
		if err != nil {
			return nil, false, err
		}
		value, err := scope.path(path)
		if errors.Is(err, errMissing) {
			return nil, false, nil
		}
		return value, err == nil, err
	})
	if err != nil {
		return false, fmt.Errorf("step %q if: %w", stepID, err)
	}
	return runs, nil
}

// resolveScope is what ${{ }} reads when a step runs: the inputs, the
// workflow task (nil before it exists), the outputs of the steps before
// (succeeded, or failed under continue), and the step's own file outputs.
type resolveScope struct {
	current map[string]Output
	inputs  map[string]any
	task    map[string]any
	outputs map[string]map[string]any
}

// value resolves a with value: a whole ${{ }} keeps its value's type, one
// within longer text is rendered as text. A whole value that is missing is
// errMissing; within text it is an error.
func (s resolveScope) value(value any) (any, error) {
	switch nested := value.(type) {
	case map[string]any:
		resolved := make(map[string]any, len(nested))
		for key, item := range nested {
			value, err := s.value(item)
			if errors.Is(err, errMissing) {
				continue
			}
			if err != nil {
				return nil, err
			}
			resolved[key] = value
		}
		return resolved, nil
	case []any:
		resolved := make([]any, len(nested))
		for i, item := range nested {
			value, err := s.value(item)
			if err != nil && !errors.Is(err, errMissing) {
				return nil, err
			}
			resolved[i] = value
		}
		return resolved, nil
	}
	text, ok := value.(string)
	if !ok {
		return value, nil
	}
	matches := findExpressions(text)
	if len(matches) == 0 {
		return text, nil
	}
	if len(matches) == 1 && matches[0].start == 0 && matches[0].end == len(text) {
		return s.expression(matches[0].inner)
	}
	var result strings.Builder
	last := 0
	for _, match := range matches {
		result.WriteString(text[last:match.start])
		resolved, err := s.expression(match.inner)
		if errors.Is(err, errMissing) {
			return nil, fmt.Errorf("%s is missing: give it a fallback with ??", match.inner)
		}
		if err != nil {
			return nil, err
		}
		switch structured := resolved.(type) {
		case map[string]any, []any:
			encoded, _ := json.Marshal(structured)
			result.Write(encoded)
		case nil:
		default:
			result.WriteString(fmt.Sprint(resolved))
		}
		last = match.end
	}
	result.WriteString(text[last:])
	return result.String(), nil
}

// expression resolves the inside of a ${{ }}: each operand of ?? in turn,
// until one is neither missing nor null.
func (s resolveScope) expression(text string) (any, error) {
	expression, err := ParseExpression(text)
	if err != nil {
		return nil, err
	}
	var last any
	lastErr := errMissing
	for _, operand := range expression.Operands {
		if operand.IsLiteral {
			last, lastErr = operand.Literal, nil
		} else {
			last, lastErr = s.path(operand.Path)
			if lastErr != nil && !errors.Is(lastErr, errMissing) {
				return nil, lastErr
			}
		}
		if lastErr == nil && last != nil {
			return last, nil
		}
	}
	return last, lastErr
}

// path reads one path; errMissing when it reads nothing.
func (s resolveScope) path(path []PathPart) (any, error) {
	parts := pathNames(path)
	switch {
	case len(parts) >= 2 && parts[0] == "workflow" && parts[1] == "task":
		if s.task == nil {
			return nil, fmt.Errorf("workflow task is unavailable")
		}
		return reachOrMissing(s.task, path[2:])
	case len(parts) >= 3 && parts[0] == "workflow" && parts[1] == "inputs":
		value, ok := s.inputs[parts[2]]
		if !ok {
			return nil, errMissing
		}
		return reachOrMissing(value, path[3:])
	case len(parts) >= 4 && parts[0] == "steps" && parts[2] == "outputs":
		stepOutputs, ok := s.outputs[parts[1]]
		if !ok {
			return nil, errMissing
		}
		value, ok := stepOutputs[parts[3]]
		if !ok {
			return nil, errMissing
		}
		return reachOrMissing(value, path[4:])
	case len(parts) == 3 && len(path) == 3 && parts[0] == "outputs" && parts[2] == "path":
		output, ok := s.current[parts[1]]
		if !ok || output.Type != "file" {
			return nil, fmt.Errorf("file output %q is not declared", parts[1])
		}
		return output.Path, nil
	}
	return nil, fmt.Errorf("unsupported expression %q", pathString(path))
}

func reachOrMissing(value any, rest []PathPart) (any, error) {
	if found, ok := reach(value, rest); ok {
		return found, nil
	}
	return nil, errMissing
}

// TriggerAgentEnded is a version 1 file's trigger: agent_ended, read as
// StartAgentEnded.
const TriggerAgentEnded = StartAgentEnded

// WaitsForPerson reports whether the run pauses before this step until it is
// started: by hand, or for an agent_ended step also by its agent ending.
func (s Step) WaitsForPerson() bool {
	return s.Start == StartManual || s.Start == StartAgentEnded
}

type startProblem struct {
	key string
	err error
}

// normalizeStart reads how the step starts from its file's key (trigger in
// version 1, start in version 2) into Start, and refuses the other key. A
// definition checked again after Parse has Start filled already.
func normalizeStart(step *Step, version int, fromFile bool) *startProblem {
	if version == SchemaVersion {
		if fromFile && step.Start != "" {
			return &startProblem{"start", fmt.Errorf("step %q: start is a schema_version 2 key; a version 1 file says trigger", step.ID)}
		}
		start := step.Trigger
		if start == "" {
			start = step.Start
		}
		if start == "" {
			start = StartAutomatic
		}
		if start != StartAutomatic && start != StartManual && start != StartAgentEnded {
			return &startProblem{"trigger", fmt.Errorf("step %q has unsupported trigger %q", step.ID, start)}
		}
		step.Start, step.Trigger = start, ""
		return nil
	}
	if step.Trigger != "" {
		return &startProblem{"trigger", fmt.Errorf("step %q: trigger is a schema_version 1 key; say start: automatic or manual", step.ID)}
	}
	if step.Start == "" {
		step.Start = StartAutomatic
	}
	if step.Start != StartAutomatic && step.Start != StartManual {
		return &startProblem{"start", fmt.Errorf("step %q has unsupported start %q: automatic or manual", step.ID, step.Start)}
	}
	return nil
}

// readableOutputs are the outputs later steps may read of a step, with
// their types: those it declares and its executor's catalog outputs, which
// count as declared without repeating them, and with on_failure: continue
// the failure's (FailureOutputs).
func readableOutputs(step Step) map[string]string {
	readable := make(map[string]string, len(step.Outputs))
	if spec, ok := catalogByType[step.Type]; ok {
		for _, output := range spec.Outputs {
			readable[output.Name] = output.Type
		}
	}
	for key, output := range step.Outputs {
		readable[key] = output.Type
	}
	if step.OnFailure == OnFailureContinue {
		for _, name := range FailureOutputs {
			if _, declared := readable[name]; !declared {
				readable[name] = "string"
			}
		}
	}
	return readable
}

// validateControl checks a step's if: and on_failure, which only a schema
// version 2 file says.
func validateControl(step Step, version int, inputs map[string]Input, previous map[string]map[string]string) *startProblem {
	if version == SchemaVersion {
		if step.If != nil {
			return &startProblem{"if", fmt.Errorf("step %q: if is a schema_version 2 key", step.ID)}
		}
		if step.OnFailure != "" {
			return &startProblem{"on_failure", fmt.Errorf("step %q: on_failure is a schema_version 2 key", step.ID)}
		}
		return nil
	}
	switch step.OnFailure {
	case "", OnFailureFail, OnFailureContinue, OnFailurePause:
	default:
		return &startProblem{"on_failure", fmt.Errorf("step %q has unsupported on_failure %q: fail, continue or pause", step.ID, step.OnFailure)}
	}
	if step.If != nil {
		err := step.If.validate(func(field string) error {
			path, err := ParsePath(field)
			if err != nil {
				return err
			}
			_, err = pathType(path, inputs, nil, previous)
			return err
		})
		if err != nil {
			return &startProblem{"if", fmt.Errorf("step %q if: %w", step.ID, err)}
		}
	}
	return nil
}
