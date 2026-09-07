package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxHandoffGitOutput = 4 << 20

var fullObjectID = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ValidateRepositoryPath rejects paths that could escape or ambiguously address
// a repository object. Git paths always use forward slashes.
func ValidateRepositoryPath(name string) error {
	if name == "" || strings.ContainsRune(name, 0) || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return fmt.Errorf("unsafe repository path %q", name)
	}
	clean := path.Clean(name)
	if clean != name || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("unsafe repository path %q", name)
	}
	return nil
}

// ReadBlobExcerpt reads a 1-based inclusive line range from a regular file at
// an exact commit. It never follows worktree symlinks or submodules.
func ReadBlobExcerpt(ctx context.Context, dir, commit, name string, first, last, maxBytes int) (string, error) {
	if !fullObjectID.MatchString(commit) {
		return "", fmt.Errorf("invalid commit ID")
	}
	if err := ValidateRepositoryPath(name); err != nil {
		return "", err
	}
	if first < 1 || last < first {
		return "", fmt.Errorf("invalid line range %d-%d", first, last)
	}
	entry, err := runGitBounded(ctx, dir, 8192, "--literal-pathspecs", "ls-tree", "-z", commit, "--", name)
	if err != nil {
		return "", err
	}
	entry = strings.TrimSuffix(entry, "\x00")
	parts := strings.SplitN(entry, "\t", 2)
	if len(parts) != 2 || parts[1] != name {
		return "", fmt.Errorf("source %q is not an exact regular file", name)
	}
	fields := strings.Fields(parts[0])
	if len(fields) != 3 || fields[1] != "blob" || !fullObjectID.MatchString(fields[2]) {
		return "", fmt.Errorf("source %q is not a regular blob at %s", name, commit)
	}
	if fields[0] != "100644" && fields[0] != "100755" {
		return "", fmt.Errorf("source %q has unsupported Git mode %s", name, fields[0])
	}
	blob, err := runGitBounded(ctx, dir, maxHandoffGitOutput, "cat-file", "blob", fields[2])
	if err != nil {
		return "", fmt.Errorf("reading source %q: %w", name, err)
	}
	if !utf8.ValidString(blob) || strings.ContainsRune(blob, 0) {
		return "", fmt.Errorf("source %q is not UTF-8 text", name)
	}
	lines := strings.Split(blob, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if last > len(lines) {
		return "", fmt.Errorf("source %q has %d lines; requested %d-%d", name, len(lines), first, last)
	}
	excerpt := strings.Join(lines[first-1:last], "\n")
	if len(excerpt) > maxBytes {
		return "", fmt.Errorf("source excerpt exceeds %d bytes", maxBytes)
	}
	return excerpt, nil
}

// ChangedFiles returns the canonical path for every file in base..head. For a
// rename Git reports the destination path; deleted files retain the old path.
func ChangedFiles(ctx context.Context, dir, base, head string) ([]string, error) {
	if !fullObjectID.MatchString(base) || !fullObjectID.MatchString(head) {
		return nil, fmt.Errorf("invalid commit ID")
	}
	out, err := runGitBounded(ctx, dir, maxHandoffGitOutput, "diff", "--no-ext-diff", "--no-textconv", "--name-only", "-z", base+".."+head, "--")
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimSuffix(out, "\x00"), "\x00")
	if len(parts) == 1 && parts[0] == "" {
		return []string{}, nil
	}
	if len(parts) > 4096 {
		return nil, fmt.Errorf("diff contains too many files")
	}
	for _, name := range parts {
		if err := ValidateRepositoryPath(name); err != nil {
			return nil, err
		}
	}
	return parts, nil
}

// IsAncestor reports whether older is an ancestor of newer.
func IsAncestor(ctx context.Context, dir, older, newer string) (bool, error) {
	if !fullObjectID.MatchString(older) || !fullObjectID.MatchString(newer) {
		return false, fmt.Errorf("invalid commit ID")
	}
	cmd := execCommand(ctx, "git", "merge-base", "--is-ancestor", older, newer)
	cmd.Dir = dir
	var stderr limitedBuffer
	stderr.limit = 4096
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base: %w: %s", err, strings.TrimSpace(stderr.String()))
}

type limitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedBuffer) String() string { return b.buffer.String() }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		return 0, fmt.Errorf("Git output exceeds %d bytes", b.limit)
	}
	if len(p) > remaining {
		_, _ = b.buffer.Write(p[:remaining])
		return remaining, fmt.Errorf("Git output exceeds %d bytes", b.limit)
	}
	return b.buffer.Write(p)
}

func runGitBounded(ctx context.Context, dir string, limit int, args ...string) (string, error) {
	cmd := execCommand(ctx, "git", args...)
	cmd.Dir = dir
	stdout := &limitedBuffer{limit: limit}
	stderr := &limitedBuffer{limit: 4096}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
