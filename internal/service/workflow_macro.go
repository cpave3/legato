package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// workflowMacros resolves a workflow step's macro when the step runs, so an
// edited macro applies to the next run.
type workflowMacros interface {
	Get(ctx context.Context, name string) (MacroEntry, error)
	Message(ctx context.Context, text, taskID string, pr *MacroPR) (string, error)
}

var errWorkflowMacrosMissing = errors.New("macros are not available to workflows here")

// stepMacro reads with.macro: the macro, or ok false when the step gives
// a prompt instead.
func stepMacro(ctx context.Context, macros workflowMacros, with map[string]any) (MacroEntry, bool, error) {
	name, err := workflowString(with, "macro", false)
	if err != nil || strings.TrimSpace(name) == "" {
		return MacroEntry{}, false, err
	}
	if macros == nil {
		return MacroEntry{}, true, errWorkflowMacrosMissing
	}
	macro, err := macros.Get(ctx, name)
	if err != nil {
		return MacroEntry{}, true, fmt.Errorf("macro %q: %w", name, err)
	}
	return macro, true, nil
}

// stepAgent is the agent tool and tier a step asks for: its own, else its
// macro's when it names none.
func stepAgent(with map[string]any, macro MacroEntry) (kind, tier string) {
	kind, _ = workflowString(with, "agent", false)
	tier, _ = workflowString(with, "tier", false)
	if kind == "" && tier == "" {
		kind, tier = macro.Agent, macro.Tier
	}
	return kind, tier
}
