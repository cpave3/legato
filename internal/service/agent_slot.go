package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/cpave3/legato/internal/engine/store"
)

// SlotReviewer is the named slot a review pass's reviewer runs in.
const SlotReviewer = "reviewer"

// slotName is what a named slot may be called: tmux keeps it in a session
// name as it is (it rewrites '.' and ':'), and it is short.
var slotName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// ValidateAgentSlot checks a slot name; "" is the main slot.
func ValidateAgentSlot(slot string) error {
	if slot == "" || slot == store.SlotMain || slotName.MatchString(slot) {
		return nil
	}
	return fmt.Errorf("agent slot %q: a slot is named with lowercase letters, digits and dashes", slot)
}

// ErrNamedSlotRefused is the refusal of a request from the person to start an
// agent in a named slot: only Legato starts agents there.
var ErrNamedSlotRefused = errors.New("only Legato starts agents in a named slot; the person's agents run in the main slot")

// IsNamedSlot reports whether slot is a named slot, not the main one.
func IsNamedSlot(slot string) bool { return slot != "" && slot != store.SlotMain }

// AgentSlotKey names one slot of a task where a name must be unique: the
// task ID for its main slot, task_slot for a named one. tmux sessions,
// agent folders and Chimera session names are made from it.
func AgentSlotKey(taskID, slot string) string {
	if !IsNamedSlot(slot) {
		return taskID
	}
	return taskID + "_" + slot
}

// AgentTmuxSession is the tmux session of a task's slot: legato-<task> for
// the main slot, legato-<task>_<slot> for a named one.
func AgentTmuxSession(taskID, slot string) string {
	return "legato-" + AgentSlotKey(taskID, slot)
}

type agentSlotKey struct{}

// WithAgentSlot addresses the agent of one of a task's slots through
// AgentService methods that take only a task ID (messages, capture, attach,
// the native endpoint). Without it they address the main slot.
func WithAgentSlot(ctx context.Context, slot string) context.Context {
	return context.WithValue(ctx, agentSlotKey{}, slot)
}

// AgentSlotFrom is the slot ctx addresses, store.SlotMain by default.
func AgentSlotFrom(ctx context.Context) string {
	if slot, _ := ctx.Value(agentSlotKey{}).(string); slot != "" {
		return slot
	}
	return store.SlotMain
}

// slotSession is the running session of the slot ctx addresses.
func slotSession(ctx context.Context, s *store.Store, taskID string) (*store.AgentSession, error) {
	return s.GetAgentSessionInSlot(ctx, taskID, AgentSlotFrom(ctx))
}

// SlotAgents acts on the agent of one of a task's slots.
type SlotAgents interface {
	// KillAgentInSlot stops the agent of one slot, leaving the others.
	KillAgentInSlot(ctx context.Context, taskID, slot string) error
	// GetAgentSessionInSlot returns the running session of one slot.
	GetAgentSessionInSlot(ctx context.Context, taskID, slot string) (*store.AgentSession, error)
	// GetTaskResumeSessionInSlot is the conversation one slot resumes.
	GetTaskResumeSessionInSlot(ctx context.Context, taskID, slot, agentKind string) (*TaskResumeSession, error)
}

// TaskAgentsStopper stops every slot of a task: what a task-level end
// (Return, Recall, Take back, delete, archive) does.
type TaskAgentsStopper interface {
	StopTaskAgents(ctx context.Context, taskID string) error
}

func (a *agentService) KillAgentInSlot(ctx context.Context, taskID, slot string) error {
	return a.stopAgentInSlot(ctx, taskID, slot, true)
}

func (a *agentService) GetAgentSessionInSlot(ctx context.Context, taskID, slot string) (*store.AgentSession, error) {
	return a.store.GetAgentSessionInSlot(ctx, taskID, slot)
}

func (a *agentService) StopTaskAgents(ctx context.Context, taskID string) error {
	sessions, err := a.store.ListTaskAgentSessions(ctx, taskID)
	if err != nil {
		return err
	}
	var errs []error
	for _, session := range sessions {
		if session.Slot == store.SlotMain {
			continue
		}
		if err := a.stopAgentInSlot(ctx, taskID, session.Slot, true); err != nil {
			errs = append(errs, err)
		}
	}
	// The main slot last, and always, so its ended rows are swept as before.
	if err := a.stopAgentInSlot(ctx, taskID, store.SlotMain, true); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
