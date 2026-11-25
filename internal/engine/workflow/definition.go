// Package workflow parses and resolves reusable workflow definitions.
package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
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

var expressionPattern = regexp.MustCompile(`\$\{\{\s*([^}]+?)\s*\}\}`)

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
	Start   string            `yaml:"start,omitempty" json:"start" legato:"v2"`
	With    map[string]any    `yaml:"with" json:"with"`
	Outputs map[string]Output `yaml:"outputs,omitempty" json:"outputs,omitempty"`
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
	for _, match := range expressionPattern.FindAllStringSubmatch(d.RunTitle, -1) {
		expr := strings.TrimSpace(match[1])
		if expr == "workflow.task" || strings.HasPrefix(expr, "workflow.task.") {
			add("run_title", fmt.Errorf("run_title: workflow.task is unavailable before the workflow task is created"))
			break
		}
	}
	if err := validateReferences(d.RunTitle, d.Inputs, nil, nil); err != nil {
		add("run_title", fmt.Errorf("run_title: %w", err))
	}
	seen := make(map[string]bool, len(d.Steps))
	availableOutputs := make(map[string]map[string]bool, len(d.Steps))
	for i := range d.Steps {
		step := &d.Steps[i]
		if err := validateStep(step, i, d.SchemaVersion, d.fromFile, seen, d.Inputs, availableOutputs); err != nil {
			problems = append(problems, err)
		}
		seen[step.ID] = true
		availableOutputs[step.ID] = readableOutputs(*step)
	}
	return problems
}

// validateStep checks one step against the steps before it: its first
// problem, or nil.
func validateStep(step *Step, index, version int, fromFile bool, seen map[string]bool, inputs map[string]Input, availableOutputs map[string]map[string]bool) *PathError {
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
	matches := expressionPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) != 1 || matches[0][0] != 0 || matches[0][1] != len(text) {
		return false
	}
	parts := strings.Split(strings.TrimSpace(text[matches[0][2]:matches[0][3]]), ".")
	if len(parts) == 2 && parts[0] == "workflow" && parts[1] == "task" {
		return true
	}
	if len(parts) != 3 || parts[0] != "workflow" || parts[1] != "inputs" {
		return false
	}
	input, ok := inputs[parts[2]]
	return ok && input.Type == "task"
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

func validateReferences(value any, inputs map[string]Input, current map[string]Output, previous map[string]map[string]bool) error {
	switch nested := value.(type) {
	case map[string]any:
		for _, item := range nested {
			if err := validateReferences(item, inputs, current, previous); err != nil {
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
	for _, match := range expressionPattern.FindAllStringSubmatch(text, -1) {
		expr := strings.TrimSpace(match[1])
		parts := strings.Split(expr, ".")
		switch {
		case len(parts) == 2 && parts[0] == "workflow" && parts[1] == "task":
		case len(parts) >= 3 && parts[0] == "workflow" && parts[1] == "task":
			valid := validTaskField(parts[2]) && (len(parts) == 3 || parts[2] == "metadata")
			if !valid {
				return fmt.Errorf("workflow task has no field path %q", strings.Join(parts[2:], "."))
			}
		case len(parts) == 3 && parts[0] == "workflow" && parts[1] == "inputs":
			if _, ok := inputs[parts[2]]; !ok {
				return fmt.Errorf("input %q is not declared", parts[2])
			}
		case len(parts) >= 4 && parts[0] == "workflow" && parts[1] == "inputs":
			input, ok := inputs[parts[2]]
			if !ok {
				return fmt.Errorf("input %q is not declared", parts[2])
			}
			valid := input.Type == "task" && validTaskField(parts[3]) && (len(parts) == 4 || parts[3] == "metadata")
			valid = valid || (input.Type == "repository" && len(parts) == 4 && validRepositoryField(parts[3]))
			if !valid {
				return fmt.Errorf("input %q has no field path %q", parts[2], strings.Join(parts[3:], "."))
			}
		case (len(parts) == 4 || len(parts) == 5) && parts[0] == "steps" && parts[2] == "outputs":
			outputs, ok := previous[parts[1]]
			if !ok {
				return fmt.Errorf("step %q is not an earlier step", parts[1])
			}
			if !outputs[parts[3]] {
				return fmt.Errorf("step %q output %q is not declared", parts[1], parts[3])
			}
			if len(parts) == 5 && parts[4] != "path" {
				return fmt.Errorf("output %q has no field %q", parts[3], parts[4])
			}
		case len(parts) == 3 && parts[0] == "outputs" && parts[2] == "path":
			if output, ok := current[parts[1]]; !ok || output.Type != "file" {
				return fmt.Errorf("file output %q is not declared", parts[1])
			}
		default:
			return fmt.Errorf("unsupported expression %q", expr)
		}
	}
	return nil
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

// ResolveFromInputs resolves a value that refers only to workflow inputs. A
// value that refers to anything else (a step's outputs, the workflow task)
// returns an error.
func ResolveFromInputs(value any, inputs map[string]any) (any, error) {
	return resolveValue(value, nil, inputs, nil, nil)
}

func (d *Definition) ResolveRunTitle(inputs map[string]any) (string, error) {
	if d.RunTitle == "" {
		return d.Title, nil
	}
	resolved, err := resolveValue(d.RunTitle, nil, inputs, nil, nil)
	if err != nil {
		return "", fmt.Errorf("resolve run_title: %w", err)
	}
	title, ok := resolved.(string)
	if !ok || strings.TrimSpace(title) == "" {
		return "", fmt.Errorf("resolved run_title must be a non-empty string")
	}
	return title, nil
}

func (d *Definition) ResolveStep(stepID string, inputs, workflowTask map[string]any, outputs map[string]map[string]any) (*ResolvedStep, error) {
	var selected *Step
	for i := range d.Steps {
		if d.Steps[i].ID == stepID {
			selected = &d.Steps[i]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("workflow step %q not found", stepID)
	}
	resolvedOutputs := make(map[string]Output, len(selected.Outputs))
	for key, output := range selected.Outputs {
		if output.Type == "file" {
			resolved, err := resolveValue(output.Path, nil, inputs, workflowTask, outputs)
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
	with := make(map[string]any, len(selected.With))
	for key, value := range selected.With {
		resolved, err := resolveValue(value, resolvedOutputs, inputs, workflowTask, outputs)
		if err != nil {
			return nil, fmt.Errorf("step %q field %q: %w", stepID, key, err)
		}
		with[key] = resolved
	}
	return &ResolvedStep{ID: selected.ID, Title: selected.Title, Type: selected.Type, With: with, Outputs: resolvedOutputs}, nil
}

func resolveValue(value any, current map[string]Output, inputs, workflowTask map[string]any, outputs map[string]map[string]any) (any, error) {
	switch nested := value.(type) {
	case map[string]any:
		resolved := make(map[string]any, len(nested))
		for key, item := range nested {
			value, err := resolveValue(item, current, inputs, workflowTask, outputs)
			if err != nil {
				return nil, err
			}
			resolved[key] = value
		}
		return resolved, nil
	case []any:
		resolved := make([]any, len(nested))
		for i, item := range nested {
			value, err := resolveValue(item, current, inputs, workflowTask, outputs)
			if err != nil {
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
	matches := expressionPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text, nil
	}
	if len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(text) {
		return resolveExpression(strings.TrimSpace(text[matches[0][2]:matches[0][3]]), current, inputs, workflowTask, outputs)
	}
	var result strings.Builder
	last := 0
	for _, match := range matches {
		result.WriteString(text[last:match[0]])
		resolved, err := resolveExpression(strings.TrimSpace(text[match[2]:match[3]]), current, inputs, workflowTask, outputs)
		if err != nil {
			return nil, err
		}
		if structured, ok := resolved.(map[string]any); ok {
			encoded, _ := json.Marshal(structured)
			result.Write(encoded)
		} else if structured, ok := resolved.([]any); ok {
			encoded, _ := json.Marshal(structured)
			result.Write(encoded)
		} else {
			result.WriteString(fmt.Sprint(resolved))
		}
		last = match[1]
	}
	result.WriteString(text[last:])
	return result.String(), nil
}

func resolveExpression(expr string, current map[string]Output, inputs, workflowTask map[string]any, outputs map[string]map[string]any) (any, error) {
	parts := strings.Split(expr, ".")
	if len(parts) >= 2 && parts[0] == "workflow" && parts[1] == "task" {
		if workflowTask == nil {
			return nil, fmt.Errorf("workflow task is unavailable")
		}
		if len(parts) == 2 {
			return workflowTask, nil
		}
		var field any = workflowTask
		for _, part := range parts[2:] {
			object, ok := field.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("workflow task field path %q is unavailable", strings.Join(parts[2:], "."))
			}
			field, ok = object[part]
			if !ok {
				return nil, fmt.Errorf("workflow task field path %q is unavailable", strings.Join(parts[2:], "."))
			}
		}
		return field, nil
	}
	if len(parts) >= 3 && parts[0] == "workflow" && parts[1] == "inputs" {
		value, ok := inputs[parts[2]]
		if !ok {
			return nil, fmt.Errorf("input %q is unavailable", parts[2])
		}
		if len(parts) == 3 {
			return value, nil
		}
		current, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("input %q is not structured", parts[2])
		}
		var field any = current
		for _, part := range parts[3:] {
			object, ok := field.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("input %q field path %q is unavailable", parts[2], strings.Join(parts[3:], "."))
			}
			field, ok = object[part]
			if !ok {
				return nil, fmt.Errorf("input %q field path %q is unavailable", parts[2], strings.Join(parts[3:], "."))
			}
		}
		return field, nil
	}
	if (len(parts) == 4 || len(parts) == 5) && parts[0] == "steps" && parts[2] == "outputs" {
		stepOutputs, ok := outputs[parts[1]]
		if !ok {
			return nil, fmt.Errorf("step %q outputs are unavailable", parts[1])
		}
		value, ok := stepOutputs[parts[3]]
		if !ok {
			return nil, fmt.Errorf("step %q output %q is unavailable", parts[1], parts[3])
		}
		if len(parts) == 4 {
			return value, nil
		}
		structured, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("step %q output %q has no field %q", parts[1], parts[3], parts[4])
		}
		field, ok := structured[parts[4]]
		if !ok {
			return nil, fmt.Errorf("step %q output %q field %q is unavailable", parts[1], parts[3], parts[4])
		}
		return field, nil
	}
	if len(parts) == 3 && parts[0] == "outputs" && parts[2] == "path" {
		output, ok := current[parts[1]]
		if !ok || output.Type != "file" {
			return nil, fmt.Errorf("file output %q is not declared", parts[1])
		}
		return output.Path, nil
	}
	return nil, fmt.Errorf("unsupported expression %q", expr)
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

// readableOutputs are the outputs later steps may read of a step: those it
// declares and its executor's catalog outputs, which count as declared
// without repeating them.
func readableOutputs(step Step) map[string]bool {
	readable := make(map[string]bool, len(step.Outputs))
	for key := range step.Outputs {
		readable[key] = true
	}
	if spec, ok := catalogByType[step.Type]; ok {
		for _, output := range spec.Outputs {
			readable[output.Name] = true
		}
	}
	return readable
}
