package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ExactFetcher fetches an exact commit from a GitHub repository into a local ref.
type ExactFetcher struct {
	ExecCommand func(context.Context, string, ...string) *exec.Cmd
}

func (f ExactFetcher) Fetch(ctx context.Context, primaryDir, repo, sha, ref string) error {
	run := f.ExecCommand
	if run == nil {
		run = exec.CommandContext
	}
	url := "https://github.com/" + repo + ".git"
	cmd := run(ctx, "git", "fetch", "--no-tags", url, sha+":"+ref)
	cmd.Dir = primaryDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("fetching exact PR head: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
