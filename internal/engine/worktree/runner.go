// Package worktree runs the external worktree manager.
package worktree

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner invokes Yggdrasil's yg executable.
type Runner struct {
	binary string
}

func NewRunner(binary string) *Runner {
	return &Runner{binary: binary}
}

// Create creates an agent-owned worktree and returns its path.
func (r *Runner) Create(ctx context.Context, taskID, primaryDir, branch, baseBranch string) (string, error) {
	cmd := exec.CommandContext(ctx, r.binary, "new", branch, "--agent-owned", "--print-path", "--base", baseBranch)
	cmd.Dir = primaryDir
	cmd.Env = append(os.Environ(), "LEGATO_TASK_ID="+taskID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("creating worktree with yg: %w: %s", err, strings.TrimSpace(string(output)))
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line, nil
		}
	}
	return "", fmt.Errorf("yggdrasil produced no path")
}

// Remove removes a worktree while preserving Yggdrasil's safety checks.
func (r *Runner) Remove(ctx context.Context, primaryDir, branch string) error {
	cmd := exec.CommandContext(ctx, r.binary, "remove", branch)
	cmd.Dir = primaryDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("removing worktree with yg: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
