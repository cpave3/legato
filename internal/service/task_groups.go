package service

import (
	"context"
	"strings"
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

// groupName is a group as stored: trimmed, and nil for none.
func groupName(group string) *string {
	group = strings.TrimSpace(group)
	if group == "" {
		return nil
	}
	return &group
}

func (a *agentService) SetTaskGroup(ctx context.Context, id string, group *string) error {
	taskID, _ := SplitAgentID(id)
	task, err := a.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	task.Group = nil
	if group != nil {
		task.Group = groupName(*group)
	}
	return a.store.UpdateTask(ctx, *task)
}

func (a *agentService) ListTaskGroups(ctx context.Context) ([]string, error) {
	return a.store.ListTaskGroups(ctx)
}

func (a *agentService) RenameTaskGroup(ctx context.Context, from, to string) (int64, error) {
	return a.store.RenameTaskGroup(ctx, strings.TrimSpace(from), groupName(to))
}
