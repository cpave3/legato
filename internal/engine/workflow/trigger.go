package workflow

import (
	"fmt"
	"reflect"
	"strings"
)

type Triggers struct {
	TaskEnteredColumn *TaskEnteredColumnTrigger `yaml:"task.entered_column,omitempty" json:"task.entered_column,omitempty"`
}

type TaskEnteredColumnTrigger struct {
	Column     string         `yaml:"column" json:"column"`
	Confirm    bool           `yaml:"confirm,omitempty" json:"confirm,omitempty"`
	Inputs     map[string]any `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Conditions *Condition     `yaml:"conditions,omitempty" json:"conditions,omitempty"`
}

type Condition struct {
	All       []Condition `yaml:"all,omitempty" json:"all,omitempty"`
	Any       []Condition `yaml:"any,omitempty" json:"any,omitempty"`
	Not       *Condition  `yaml:"not,omitempty" json:"not,omitempty"`
	Field     string      `yaml:"field,omitempty" json:"field,omitempty"`
	Exists    *bool       `yaml:"exists,omitempty" json:"exists,omitempty"`
	Equals    any         `yaml:"equals,omitempty" json:"equals,omitempty"`
	NotEquals any         `yaml:"not_equals,omitempty" json:"not_equals,omitempty"`
	In        []any       `yaml:"in,omitempty" json:"in,omitempty"`
}

func (t *TaskEnteredColumnTrigger) Validate(inputs map[string]Input) error {
	if strings.TrimSpace(t.Column) == "" {
		return fmt.Errorf("task.entered_column column is required")
	}
	for key := range t.Inputs {
		if _, ok := inputs[key]; !ok {
			return fmt.Errorf("trigger input %q is not declared", key)
		}
	}
	if t.Conditions != nil {
		return t.Conditions.validate()
	}
	return nil
}

func (c Condition) validate() error {
	branches := 0
	if len(c.All) > 0 {
		branches++
	}
	if len(c.Any) > 0 {
		branches++
	}
	if c.Not != nil {
		branches++
	}
	if c.Field != "" {
		branches++
	}
	if branches != 1 {
		return fmt.Errorf("condition requires exactly one of all, any, not, or field")
	}
	for _, child := range append(append([]Condition{}, c.All...), c.Any...) {
		if err := child.validate(); err != nil {
			return err
		}
	}
	if c.Not != nil {
		return c.Not.validate()
	}
	if c.Field != "" {
		if !validConditionField(c.Field) {
			return fmt.Errorf("unsupported condition field %q", c.Field)
		}
		ops := 0
		if c.Exists != nil {
			ops++
		}
		if c.Equals != nil {
			ops++
		}
		if c.NotEquals != nil {
			ops++
		}
		if c.In != nil {
			ops++
		}
		if ops != 1 {
			return fmt.Errorf("condition field %q requires exactly one operator", c.Field)
		}
	}
	return nil
}

func validConditionField(field string) bool {
	switch field {
	case "task.id", "task.title", "task.description", "task.status", "task.priority", "task.provider", "task.remote_id", "task.has_active_agent", "event.from_column", "event.to_column":
		return true
	}
	return strings.HasPrefix(field, "task.metadata.") && len(strings.TrimPrefix(field, "task.metadata.")) > 0
}

func (t *TaskEnteredColumnTrigger) Matches(task map[string]any) (bool, error) {
	return t.MatchesEvent(task, nil)
}

func (t *TaskEnteredColumnTrigger) MatchesEvent(task, event map[string]any) (bool, error) {
	if t.Conditions == nil {
		return true, nil
	}
	return t.Conditions.matches(task, event)
}

func (c Condition) UsesField(field string) bool {
	if c.Field == field {
		return true
	}
	for _, child := range c.All {
		if child.UsesField(field) {
			return true
		}
	}
	for _, child := range c.Any {
		if child.UsesField(field) {
			return true
		}
	}
	return c.Not != nil && c.Not.UsesField(field)
}

func (c Condition) matches(task, event map[string]any) (bool, error) {
	if len(c.All) > 0 {
		for _, child := range c.All {
			ok, err := child.matches(task, event)
			if err != nil || !ok {
				return ok, err
			}
		}
		return true, nil
	}
	if len(c.Any) > 0 {
		for _, child := range c.Any {
			ok, err := child.matches(task, event)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	if c.Not != nil {
		ok, err := c.Not.matches(task, event)
		return !ok, err
	}
	var value any
	var exists bool
	if strings.HasPrefix(c.Field, "event.") {
		if event == nil {
			return false, fmt.Errorf("event fields require transition context")
		}
		value, exists = lookupTaskField(event, strings.TrimPrefix(c.Field, "event."))
	} else {
		value, exists = lookupTaskField(task, strings.TrimPrefix(c.Field, "task."))
	}
	if c.Exists != nil {
		return exists == *c.Exists, nil
	}
	if c.Equals != nil {
		return exists && reflect.DeepEqual(value, c.Equals), nil
	}
	if c.NotEquals != nil {
		return !exists || !reflect.DeepEqual(value, c.NotEquals), nil
	}
	for _, candidate := range c.In {
		if exists && reflect.DeepEqual(value, candidate) {
			return true, nil
		}
	}
	return false, nil
}

func lookupTaskField(task map[string]any, path string) (any, bool) {
	var current any = task
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
