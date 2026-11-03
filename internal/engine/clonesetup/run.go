package clonesetup

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunOptions are where and how the setup's commands run.
type RunOptions struct {
	// Dir is the clone, where every command runs.
	Dir string
	// Base is the ref the clone was made from; the trunk when empty.
	Base string
	// Primary is the repository's main checkout, "" without one.
	Primary string
	// Env is the commands' environment before the variables Run adds; the
	// process's own when nil.
	Env []string
	// Out receives what the commands write, and a line before each.
	Out io.Writer
}

// Run runs the commands in order with sh -c in the clone, as Yggdrasil runs
// post_create hooks, and stops at the first that fails. Each gets the
// variables Yggdrasil gives (YG_WORKTREE, YG_BRANCH, YG_BRANCH_NEW, YG_TRUNK,
// YG_BASE, YG_PRIMARY, YG_REPO, YG_EVENT=post_create, YG_AGENT_OWNED=1), so
// a .yggdrasil.toml's hooks work unchanged, and LEGATO_CLONE=1, so a hook
// can tell it runs in a clone Legato made.
func Run(ctx context.Context, commands []string, o RunOptions) error {
	if len(commands) == 0 {
		return nil
	}
	out := o.Out
	if out == nil {
		out = io.Discard
	}
	env := o.Env
	if env == nil {
		env = os.Environ()
	}
	env = append(append([]string{}, env...), variables(ctx, o)...)
	for i, command := range commands {
		fmt.Fprintf(out, "legato setup [%d/%d]: %s\n", i+1, len(commands), command)
		cmd := exec.CommandContext(ctx, "sh", "-c", command)
		cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = o.Dir, env, out, out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("setup command %d failed: %q: %w", i+1, command, err)
		}
	}
	return nil
}

// variables are Yggdrasil's, as they stand for this clone, read from its
// git: the branch checked out, whether GitHub has it, the default branch.
func variables(ctx context.Context, o RunOptions) []string {
	git := func(args ...string) string {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = o.Dir
		b, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	branch := git("symbolic-ref", "--quiet", "--short", "HEAD")
	trunk := strings.TrimPrefix(git("symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"), "origin/")
	if trunk == "" {
		trunk = "main"
	}
	isNew := "0"
	if branch != "" && git("rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch) == "" {
		isNew = "1"
	}
	base := o.Base
	if base == "" {
		base = trunk
	}
	repo := git("rev-parse", "--git-common-dir")
	if repo != "" && !filepath.IsAbs(repo) {
		repo = filepath.Join(o.Dir, repo)
	}
	return []string{
		"YG_WORKTREE=" + o.Dir, "YG_PRIMARY=" + o.Primary, "YG_BRANCH=" + branch, "YG_TRUNK=" + trunk,
		"YG_BASE=" + base, "YG_REPO=" + repo, "YG_PROFILE=", "YG_EVENT=post_create", "YG_AGENT_OWNED=1",
		"YG_BRANCH_NEW=" + isNew, "LEGATO_CLONE=1",
	}
}
