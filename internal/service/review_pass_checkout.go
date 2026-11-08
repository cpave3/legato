package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cpave3/legato/internal/engine/gitrepo"
	"github.com/cpave3/legato/internal/engine/stackgraph"
	"github.com/cpave3/legato/internal/engine/store"
)

// GitReviewCheckout reads the author's job clone with git on the host:
// without hooks, fsmonitor or system configuration.
type GitReviewCheckout struct {
	Store *store.Store
}

func (GitReviewCheckout) git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// Branches are the card's job clone and its stack's branches, bottom
// first, or the branch it has checked out when it has no stack.
func (c GitReviewCheckout) Branches(ctx context.Context, taskID, repo string) (string, []string, error) {
	if c.Store == nil {
		return "", nil, errors.New("no store to find the card's clone in")
	}
	clone, err := c.Store.LiveJobCloneForTask(ctx, taskID, repo)
	if err != nil {
		return "", nil, fmt.Errorf("the card has no live clone of %s: %w", repo, err)
	}
	if stack, err := stackgraph.Read(clone.Path); err == nil && len(stack.Branches) > 0 {
		branches := make([]string, 0, len(stack.Branches))
		for _, b := range stack.Branches {
			branches = append(branches, b.Name)
		}
		return clone.Path, branches, nil
	}
	branch, _, err := gitrepo.Head(clone.Path)
	if err != nil || branch == "" {
		return "", nil, fmt.Errorf("the clone at %s is on no branch: %v", clone.Path, err)
	}
	return clone.Path, []string{branch}, nil
}

func (c GitReviewCheckout) Tip(ctx context.Context, dir, branch string) (string, error) {
	out, err := c.git(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch+"^{commit}")
	return strings.TrimSpace(out), err
}

func (c GitReviewCheckout) State(ctx context.Context, dir string) (string, bool, error) {
	head, err := c.git(ctx, dir, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", false, err
	}
	status, err := c.git(ctx, dir, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(head), strings.TrimSpace(status) == "", nil
}

func (c GitReviewCheckout) Pin(ctx context.Context, dir, ref, sha string) error {
	_, err := c.git(ctx, dir, "update-ref", ref, sha)
	return err
}

// Changed reads git diff -U0 between the commits: each hunk's lines in to,
// and for a deletion the lines around where it was.
func (c GitReviewCheckout) Changed(ctx context.Context, dir, from, to string) (map[string][][2]int, error) {
	out, err := c.git(ctx, dir, "-c", "core.quotePath=false", "diff", "-U0", "--no-color", "--no-ext-diff", from, to)
	if err != nil {
		return nil, err
	}
	changed := map[string][][2]int{}
	path := ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			path = ""
			if name := strings.TrimPrefix(line, "+++ "); strings.HasPrefix(name, "b/") {
				path = strings.TrimPrefix(name, "b/")
			}
		case strings.HasPrefix(line, "@@ ") && path != "":
			start, count, ok := hunkLines(line)
			if !ok {
				continue
			}
			if count == 0 {
				changed[path] = append(changed[path], [2]int{max(start, 1), start + 1})
				continue
			}
			changed[path] = append(changed[path], [2]int{start, start + count - 1})
		}
	}
	return changed, nil
}

// hunkLines reads the new side of a hunk header, "@@ -a,b +c,d @@".
func hunkLines(header string) (start, count int, ok bool) {
	fields := strings.Fields(header)
	if len(fields) < 3 || !strings.HasPrefix(fields[2], "+") {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(fields[2], "+")
	count = 1
	if i := strings.IndexByte(spec, ','); i >= 0 {
		n, err := strconv.Atoi(spec[i+1:])
		if err != nil {
			return 0, 0, false
		}
		count, spec = n, spec[:i]
	}
	start, err := strconv.Atoi(spec)
	if err != nil {
		return 0, 0, false
	}
	return start, count, true
}

func (c GitReviewCheckout) Lines(ctx context.Context, dir, sha, path string) (int, bool, error) {
	if _, err := c.git(ctx, dir, "cat-file", "-e", sha+":"+path); err != nil {
		return 0, false, nil
	}
	out, err := c.git(ctx, dir, "cat-file", "blob", sha+":"+path)
	if err != nil {
		return 0, false, err
	}
	n := strings.Count(out, "\n")
	if out != "" && !strings.HasSuffix(out, "\n") {
		n++
	}
	return n, true, nil
}
