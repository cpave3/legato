package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TaskGroups manages the groups agents are listed in. A group belongs to the
// task: every agent of the task is in it. The agent service implements it.
type TaskGroups interface {
	// SetTaskGroup puts the task of id (a task ID, or an agent ID that names
	// a slot) in group; nil or a blank name takes it out of any group.
	SetTaskGroup(ctx context.Context, id string, group *string) error
	// ListTaskGroups names each group a task is in, in name order.
	ListTaskGroups(ctx context.Context) ([]string, error)
	// RenameTaskGroup moves every task in from to to, and says how many it
	// moved. A blank to takes them out of any group: the group is gone.
	RenameTaskGroup(ctx context.Context, from, to string) (int64, error)
}

// ErrInvalidTaskGroup is a group name that is not one short line.
var ErrInvalidTaskGroup = errors.New("invalid group name")

// maxTaskGroupName is the longest a group's name may be, in characters.
const maxTaskGroupName = 60

// groupName is a group as stored: trimmed, and nil for none. A name longer
// than maxTaskGroupName or with a control character (a line break, a tab)
// is ErrInvalidTaskGroup: it heads a section of every agent list.
func groupName(group string) (*string, error) {
	group = strings.TrimSpace(group)
	if group == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(group) > maxTaskGroupName {
		return nil, fmt.Errorf("%w: at most %d characters", ErrInvalidTaskGroup, maxTaskGroupName)
	}
	if strings.ContainsFunc(group, unicode.IsControl) {
		return nil, fmt.Errorf("%w: one line, with no tabs or control characters", ErrInvalidTaskGroup)
	}
	return &group, nil
}

// SetTaskGroup writes the group alone, after RefuseAway: a task away on a
// worker keeps its group, as it keeps its workspace.
func (a *agentService) SetTaskGroup(ctx context.Context, id string, group *string) error {
	taskID, _ := SplitAgentID(id)
	var name *string
	if group != nil {
		var err error
		if name, err = groupName(*group); err != nil {
			return err
		}
	}
	if err := RefuseAway(ctx, a.store, taskID); err != nil {
		return err
	}
	return a.store.SetTaskGroup(ctx, taskID, name)
}

func (a *agentService) ListTaskGroups(ctx context.Context) ([]string, error) {
	return a.store.ListTaskGroups(ctx)
}

func (a *agentService) RenameTaskGroup(ctx context.Context, from, to string) (int64, error) {
	name, err := groupName(to)
	if err != nil {
		return 0, err
	}
	return a.store.RenameTaskGroup(ctx, strings.TrimSpace(from), name)
}
