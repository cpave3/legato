package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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

// macroPRHeader is the line a macro started on a pull request opens with,
// when a step gives repo and pr_number; "" otherwise.
func macroPRHeader(with map[string]any) string {
	repo, _ := workflowString(with, "repo", false)
	number := fmt.Sprint(with["pr_number"])
	if strings.TrimSpace(repo) == "" || with["pr_number"] == nil || number == "" || number == "0" {
		return ""
	}
	return fmt.Sprintf("Pull request %s#%s: https://github.com/%s/pull/%s\n\n", repo, number, repo, number)
}

// stepPR is the pull request a step names with repo and pr_number, or nil.
func stepPR(with map[string]any) *MacroPR {
	repo, _ := workflowString(with, "repo", false)
	number, _ := strconv.Atoi(fmt.Sprint(with["pr_number"]))
	if strings.TrimSpace(repo) == "" || number <= 0 {
		return nil
	}
	return &MacroPR{Repo: repo, Number: number}
}
