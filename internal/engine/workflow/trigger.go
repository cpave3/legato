package workflow

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

type Triggers struct {
	TaskEnteredColumn  *TaskEnteredColumnTrigger `yaml:"task.entered_column,omitempty" json:"task.entered_column,omitempty"`
	Schedule           *ScheduleTrigger          `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	PRChangesRequested *PRTrigger                `yaml:"pr.changes_requested,omitempty" json:"pr.changes_requested,omitempty"`
	ChatMessage        *ChatMessageTrigger       `yaml:"chat.message,omitempty" json:"chat.message,omitempty"`
	ChatMention        *ChatMentionTrigger       `yaml:"chat.mention,omitempty" json:"chat.mention,omitempty"`
}

// TriggerKinds is every kind of trigger a definition's on: may declare, in
// the order lists and the graph give them.
var TriggerKinds = []string{TriggerTaskEnteredColumn, TriggerSchedule, TriggerPRChangesRequested, TriggerChatMessage, TriggerChatMention}

// The kinds of trigger, as a definition's on: names them.
const (
	TriggerTaskEnteredColumn = "task.entered_column"
)

// TriggerInfo is one trigger a definition declares: its kind, and whether
// it is on when no Legato has switched it (its enabled field, true when
// absent).
type TriggerInfo struct {
	Kind             string `json:"trigger"`
	EnabledByDefault bool   `json:"file_enabled"`
	// MaxRunsPerDay is the loop stop's limit for one item.
	MaxRunsPerDay int `json:"max_runs_per_day"`
}

// DefaultMaxRunsPerDay is how many runs a trigger may start for one item in
// a day before the loop stop holds the next and asks the operator.
const DefaultMaxRunsPerDay = 3

// TriggerCommon is what every kind of trigger may say.
type TriggerCommon struct {
	Enabled       *bool
	Confirm       bool
	MaxRunsPerDay int
	Inputs        map[string]any
}

// Limit is the trigger's runs per item per day.
func (c TriggerCommon) Limit() int {
	if c.MaxRunsPerDay > 0 {
		return c.MaxRunsPerDay
	}
	return DefaultMaxRunsPerDay
}

// Common is what a declared trigger of a kind says in common with the
// others; ok is false when the definition has none of that kind.
func (t Triggers) Common(kind string) (TriggerCommon, bool) {
	switch kind {
	case TriggerTaskEnteredColumn:
		if c := t.TaskEnteredColumn; c != nil {
			return TriggerCommon{Enabled: c.Enabled, Confirm: c.Confirm, MaxRunsPerDay: c.MaxRunsPerDay, Inputs: c.Inputs}, true
		}
	case TriggerSchedule:
		if c := t.Schedule; c != nil {
			return TriggerCommon{Enabled: c.Enabled, Confirm: c.Confirm, MaxRunsPerDay: c.MaxRunsPerDay, Inputs: c.Inputs}, true
		}
	case TriggerPRChangesRequested:
		if c := t.PRChangesRequested; c != nil {
			return c.common(), true
		}
	case TriggerChatMessage, TriggerChatMention:
		if c := t.Chat(kind); c != nil {
			return c.common(), true
		}
	}
	return TriggerCommon{}, false
}

// List is the triggers a definition declares, in a fixed order.
func (t Triggers) List() []TriggerInfo {
	var out []TriggerInfo
	for _, kind := range TriggerKinds {
		if common, ok := t.Common(kind); ok {
			out = append(out, TriggerInfo{Kind: kind, EnabledByDefault: enabledByDefault(common.Enabled), MaxRunsPerDay: common.Limit()})
		}
	}
	return out
}

func validateMaxRunsPerDay(n int) error {
	if n < 0 {
		return fmt.Errorf("max_runs_per_day must be 1 or more")
	}
	return nil
}

func enabledByDefault(enabled *bool) bool { return enabled == nil || *enabled }

type TaskEnteredColumnTrigger struct {
	Column string `yaml:"column" json:"column"`
	// Enabled false makes the trigger start paused: it starts nothing until
	// a Legato switches it on (per instance, never in this file).
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Confirm bool  `yaml:"confirm,omitempty" json:"confirm,omitempty"`
	// MaxRunsPerDay is how many runs the trigger may start for one task in
	// a day before the loop stop holds the next; 0 is DefaultMaxRunsPerDay.
	MaxRunsPerDay int `yaml:"max_runs_per_day,omitempty" json:"max_runs_per_day,omitempty"`
	// HandedOff lets the trigger run on a card a main handed off to this
	// Legato (a worker), which by default it skips: the main's own
	// workflows run when the task returns.
	HandedOff  bool           `yaml:"handed_off,omitempty" json:"handed_off,omitempty"`
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
		return within("column", fmt.Errorf("task.entered_column column is required"))
	}
	if err := validateMaxRunsPerDay(t.MaxRunsPerDay); err != nil {
		return within("max_runs_per_day", fmt.Errorf("task.entered_column %w", err))
	}
	if err := validateTriggerInputs(t.Inputs, inputs); err != nil {
		return err
	}
	if t.Conditions != nil {
		if err := t.Conditions.validate(); err != nil {
			return within("conditions", err)
		}
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

// ConditionFields are the fields a column-entry trigger's conditions may
// compare, besides task.metadata.<key>.
var ConditionFields = []string{"task.id", "task.title", "task.description", "task.status", "task.priority", "task.provider", "task.remote_id", "task.has_active_agent", "task.has_chimera_session", "event.from_column", "event.to_column"}

func validConditionField(field string) bool {
	if slices.Contains(ConditionFields, field) {
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

// validateTriggerInputs checks that a trigger names only declared inputs.
func validateTriggerInputs(values map[string]any, inputs map[string]Input) error {
	for _, key := range sortedKeys(values) {
		if _, ok := inputs[key]; !ok {
			return within("inputs."+key, fmt.Errorf("trigger input %q is not declared", key))
		}
	}
	return nil
}
