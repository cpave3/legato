// Package workflow parses and resolves reusable workflow definitions.
package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const SchemaVersion = 1

var expressionPattern = regexp.MustCompile(`\$\{\{\s*([^}]+?)\s*\}\}`)

type Definition struct {
	SchemaVersion int              `yaml:"schema_version" json:"schema_version"`
	Name          string           `yaml:"name" json:"name"`
	Title         string           `yaml:"title" json:"title"`
	Description   string           `yaml:"description,omitempty" json:"description,omitempty"`
	Inputs        map[string]Input `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Steps         []Step           `yaml:"steps" json:"steps"`
	Source        string           `yaml:"-" json:"source"`
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
	ID      string            `yaml:"id" json:"id"`
	Title   string            `yaml:"title" json:"title"`
	Type    string            `yaml:"type" json:"type"`
	Trigger string            `yaml:"trigger,omitempty" json:"trigger"`
	With    map[string]any    `yaml:"with" json:"with"`
	Outputs map[string]Output `yaml:"outputs,omitempty" json:"outputs,omitempty"`
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
	if err := definition.Validate(); err != nil {
		return nil, fmt.Errorf("validate workflow %s: %w", source, err)
	}
	return &definition, nil
}

func (d *Definition) Validate() error {
	if d.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %d", SchemaVersion)
	}
	if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Title) == "" {
		return fmt.Errorf("name and title are required")
	}
	if len(d.Steps) == 0 {
		return fmt.Errorf("at least one step is required")
	}
	for key, input := range d.Inputs {
		if !validValueType(input.Type, false) {
			return fmt.Errorf("input %q has unsupported type %q", key, input.Type)
		}
	}
	seen := make(map[string]bool, len(d.Steps))
	availableOutputs := make(map[string]map[string]bool, len(d.Steps))
	for i := range d.Steps {
		step := &d.Steps[i]
		if step.ID == "" || seen[step.ID] {
			return fmt.Errorf("step IDs must be non-empty and unique")
		}
		seen[step.ID] = true
		if step.Trigger == "" {
			step.Trigger = "automatic"
		}
		if step.Trigger != "automatic" && step.Trigger != "manual" {
			return fmt.Errorf("step %q has unsupported trigger %q", step.ID, step.Trigger)
		}
		if step.Type != "github.pr.prepare" && step.Type != "agent.run" && step.Type != "worktree.remove" && step.Type != "task.note" {
			return fmt.Errorf("step %q has unsupported type %q", step.ID, step.Type)
		}
		for field, value := range step.With {
			if err := validateReferences(value, d.Inputs, step.Outputs, availableOutputs); err != nil {
				return fmt.Errorf("step %q field %q: %w", step.ID, field, err)
			}
		}
		declared := make(map[string]bool, len(step.Outputs))
		for key, output := range step.Outputs {
			if !validValueType(output.Type, true) {
				return fmt.Errorf("step %q output %q has unsupported type %q", step.ID, key, output.Type)
			}
			if output.Type == "file" && (output.Path == "" || !safeRelativePath(output.Path)) {
				return fmt.Errorf("step %q output %q requires a safe relative path", step.ID, key)
			}
			declared[key] = true
		}
		availableOutputs[step.ID] = declared
	}
	return nil
}

func validValueType(valueType string, file bool) bool {
	return valueType == "string" || valueType == "integer" || valueType == "boolean" || valueType == "json" || (!file && valueType == "task") || (file && valueType == "file")
}

func validTaskField(field string) bool {
	switch field {
	case "id", "title", "description", "status", "priority", "provider", "remote_id":
		return true
	}
	return false
}

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
	text, ok := value.(string)
	if !ok {
		return nil
	}
	for _, match := range expressionPattern.FindAllStringSubmatch(text, -1) {
		expr := strings.TrimSpace(match[1])
		parts := strings.Split(expr, ".")
		switch {
		case len(parts) == 3 && parts[0] == "workflow" && parts[1] == "inputs":
			if _, ok := inputs[parts[2]]; !ok {
				return fmt.Errorf("input %q is not declared", parts[2])
			}
		case len(parts) == 4 && parts[0] == "workflow" && parts[1] == "inputs":
			input, ok := inputs[parts[2]]
			if !ok {
				return fmt.Errorf("input %q is not declared", parts[2])
			}
			if input.Type != "task" || !validTaskField(parts[3]) {
				return fmt.Errorf("input %q has no field %q", parts[2], parts[3])
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
	case "string", "task":
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

func (d *Definition) ResolveStep(stepID string, inputs map[string]any, outputs map[string]map[string]any) (*ResolvedStep, error) {
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
	with := make(map[string]any, len(selected.With))
	for key, value := range selected.With {
		resolved, err := resolveValue(value, selected.Outputs, inputs, outputs)
		if err != nil {
			return nil, fmt.Errorf("step %q field %q: %w", stepID, key, err)
		}
		with[key] = resolved
	}
	return &ResolvedStep{ID: selected.ID, Title: selected.Title, Type: selected.Type, With: with, Outputs: selected.Outputs}, nil
}

func resolveValue(value any, current map[string]Output, inputs map[string]any, outputs map[string]map[string]any) (any, error) {
	text, ok := value.(string)
	if !ok {
		return value, nil
	}
	matches := expressionPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text, nil
	}
	if len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(text) {
		return resolveExpression(strings.TrimSpace(text[matches[0][2]:matches[0][3]]), current, inputs, outputs)
	}
	var result strings.Builder
	last := 0
	for _, match := range matches {
		result.WriteString(text[last:match[0]])
		resolved, err := resolveExpression(strings.TrimSpace(text[match[2]:match[3]]), current, inputs, outputs)
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

func resolveExpression(expr string, current map[string]Output, inputs map[string]any, outputs map[string]map[string]any) (any, error) {
	parts := strings.Split(expr, ".")
	if len(parts) >= 3 && len(parts) <= 4 && parts[0] == "workflow" && parts[1] == "inputs" {
		value, ok := inputs[parts[2]]
		if !ok {
			return nil, fmt.Errorf("input %q is unavailable", parts[2])
		}
		if len(parts) == 3 {
			return value, nil
		}
		task, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("input %q is not a task", parts[2])
		}
		field, ok := task[parts[3]]
		if !ok {
			return nil, fmt.Errorf("task input %q field %q is unavailable", parts[2], parts[3])
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
