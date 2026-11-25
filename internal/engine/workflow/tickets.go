package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// The ticket and board steps (spec LEG-482 §6, LEG-488).
const (
	TicketImportStepType = "ticket.import"
	TicketsListStepType  = "tickets.list"
	TasksListStepType    = "tasks.list"
)

// Ticket states tickets.list takes.
const (
	TicketStateOpen   = "open"
	TicketStateClosed = "closed"
	TicketStateAny    = "any"
)

// DefaultListLimit is how many items tickets.list and tasks.list give when
// the step names no limit, and MaxListLimit the most they give.
const (
	DefaultListLimit = 50
	MaxListLimit     = 200
)

// TicketProviders are the providers the ticket steps name.
var TicketProviders = []string{"burnlite", "jira"}

// TaskWhereFields are what tasks.list's where reads of each task, besides
// task.metadata.<path>: a task reference's fields and its workspace's name.
var TaskWhereFields = append(append([]string{}, TaskFields...), "workspace")

// TaskCondition reads tasks.list's where: a condition, as a file or a
// resolved with gives it, whose every field is a task.* path.
func TaskCondition(value any) (*Condition, error) {
	if _, ok := value.(map[string]any); !ok {
		return nil, fmt.Errorf("where must be a condition: all, any, not or field")
	}
	condition, err := decodeCondition(value)
	if err != nil {
		return nil, fmt.Errorf("where: %w", err)
	}
	if err := condition.validate(checkTaskWhereField); err != nil {
		return nil, fmt.Errorf("where: %w", err)
	}
	return &condition, nil
}

// conditionAtLoad puts a value of the operator's type in place of each
// in, exists or empty given as ${{ }}, whose type is known only once it
// resolves: TaskCondition checks the resolved value when the step runs, and
// the expression is checked with the rest of with.
func conditionAtLoad(value any) any {
	condition, ok := value.(map[string]any)
	if !ok {
		return value
	}
	out := make(map[string]any, len(condition))
	for key, item := range condition {
		switch key {
		case "all", "any":
			if list, ok := item.([]any); ok {
				children := make([]any, len(list))
				for i, child := range list {
					children[i] = conditionAtLoad(child)
				}
				item = children
			}
		case "not":
			item = conditionAtLoad(item)
		case "in", "exists", "empty":
			if text, ok := item.(string); ok && HasExpression(text) {
				item = map[string]any{"in": []any{}, "exists": true, "empty": true}[key]
			}
		}
		out[key] = item
	}
	return out
}

// decodeCondition reads a condition as a file or a resolved with gives it:
// a map of a Condition's keys and no others.
func decodeCondition(value any) (Condition, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return Condition{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var condition Condition
	if err := decoder.Decode(&condition); err != nil {
		return Condition{}, err
	}
	return condition, nil
}

func checkTaskWhereField(field string) error {
	path, err := ParsePath(field)
	if err != nil {
		return err
	}
	names := pathNames(path)
	if len(names) < 2 || names[0] != "task" {
		return fmt.Errorf("where reads task.<field> or task.metadata.<path>, not %q", field)
	}
	if !slices.Contains(TaskWhereFields, names[1]) {
		return fmt.Errorf("a task has no field %q: %s, or metadata.<path>", names[1], strings.Join(TaskWhereFields, ", "))
	}
	if names[1] != "metadata" && len(path) > 2 {
		return fmt.Errorf("task.%s has no field path %q", names[1], strings.TrimPrefix(pathString(path[2:]), "."))
	}
	return nil
}

// MatchTask checks the condition against one task, read as task.<path>.
func (c Condition) MatchTask(task map[string]any) (bool, error) {
	return c.evaluate(func(field string) (any, bool, error) {
		path, err := ParsePath(field)
		if err != nil {
			return nil, false, err
		}
		if len(path) == 0 || path[0].Name != "task" {
			return nil, false, fmt.Errorf("where reads task.* only, not %q", field)
		}
		value, ok := reach(task, path[1:])
		return value, ok, nil
	})
}

// labelsValid reports whether a value is a list of label names: non-empty
// text without commas or line breaks.
func labelsValid(value any) bool {
	list, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range list {
		name, ok := item.(string)
		if !ok || strings.TrimSpace(name) == "" || strings.ContainsAny(name, ",\n\r") {
			return false
		}
	}
	return true
}
