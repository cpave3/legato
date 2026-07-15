// Package git runs read-only git queries against a task's worktree.
package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// execCommand is injectable for tests that exercise error paths.
var execCommand = exec.CommandContext

// Commit is one commit reachable from HEAD but not from the base branch.
type Commit struct {
	SHA     string
	Subject string
	Body    string
	Author  string
	When    time.Time
}

// MergeBase returns the fork point between HEAD and the given base branch.
func MergeBase(ctx context.Context, dir, base string) (string, error) {
	out, err := runGit(ctx, dir, "merge-base", base, "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// CommitsAhead lists the commits in base..HEAD, oldest first.
func CommitsAhead(ctx context.Context, dir, base string) ([]Commit, error) {
	// %x00 separates fields, %x01 separates records — commit bodies may
	// contain any printable text, so newline-delimited parsing is unsafe.
	out, err := runGit(ctx, dir, "log", "--reverse", "--topo-order",
		"--format=%H%x00%s%x00%b%x00%an%x00%at%x01", base+"..HEAD")
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, record := range strings.Split(out, "\x01") {
		record = strings.TrimLeft(record, "\n")
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, "\x00", 5)
		if len(fields) != 5 {
			return nil, fmt.Errorf("malformed git log record: %q", record)
		}
		unix, err := strconv.ParseInt(strings.TrimSpace(fields[4]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing commit timestamp: %w", err)
		}
		commits = append(commits, Commit{
			SHA:     fields[0],
			Subject: fields[1],
			Body:    strings.TrimSpace(fields[2]),
			Author:  fields[3],
			When:    time.Unix(unix, 0),
		})
	}
	return commits, nil
}

// ShowCommit returns the unified diff introduced by a single commit, without
// the commit message header.
func ShowCommit(ctx context.Context, dir, sha string) (string, error) {
	return runGit(ctx, dir, "show", "--format=", "--patch", sha)
}

// DiffWorktree returns the uncommitted changes in the worktree as a unified
// diff: staged + unstaged changes to tracked files, plus untracked files
// rendered as additions.
// StatusPorcelain returns the worktree and index state in Git's stable,
// machine-readable short format.
func StatusPorcelain(ctx context.Context, dir string) (string, error) {
	return runGit(ctx, dir, "status", "--porcelain")
}

func DiffWorktree(ctx context.Context, dir string) (string, error) {
	tracked, err := runGit(ctx, dir, "diff", "HEAD")
	if err != nil {
		return "", err
	}
	untrackedList, err := runGit(ctx, dir, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(tracked)
	for _, path := range strings.Split(strings.TrimSpace(untrackedList), "\n") {
		if path == "" {
			continue
		}
		// --no-index exits 1 when the files differ; that is the expected
		// outcome here, not a failure.
		patch, err := runGitExitOK(ctx, dir, 1, "diff", "--no-index", "--", "/dev/null", path)
		if err != nil {
			return "", err
		}
		b.WriteString(patch)
	}
	return b.String(), nil
}

// CommitExists reports whether the SHA still resolves to a commit in the
// repository's object store (it may have left the branch history after a
// rebase or amend yet still be renderable).
func CommitExists(ctx context.Context, dir, sha string) bool {
	_, err := runGit(ctx, dir, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

// DiffFiles returns the diff between base and the current worktree limited to
// the given paths. Used to render note steps that anchor to files rather than
// a commit.
func DiffFiles(ctx context.Context, dir, base string, files []string) (string, error) {
	args := append([]string{"diff", base, "--"}, files...)
	return runGit(ctx, dir, args...)
}

// runGitExitOK runs git treating the given non-zero exit code as success.
func runGitExitOK(ctx context.Context, dir string, okCode int, args ...string) (string, error) {
	cmd := execCommand(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == okCode {
			return string(out), nil
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := execCommand(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
