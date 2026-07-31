package service

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// CheckoutInspector reads repository identity from an explicit local checkout.
type CheckoutInspector interface {
	InspectCodeContext(directory string) (*store.CodeContext, error)
}

type GitCheckoutInspector struct {
	execCommand func(name string, args ...string) *exec.Cmd
}

func NewGitCheckoutInspector() *GitCheckoutInspector {
	return &GitCheckoutInspector{execCommand: exec.Command}
}

var (
	checkoutSSHRemote   = regexp.MustCompile(`(?:ssh://)?git@github\.com[:/]([^/]+)/(.+?)(?:\.git)?$`)
	checkoutHTTPSRemote = regexp.MustCompile(`https://github\.com/([^/]+)/(.+?)(?:\.git)?$`)
)

func (g *GitCheckoutInspector) InspectCodeContext(directory string) (*store.CodeContext, error) {
	remote, err := g.git(directory, "remote", "get-url", "origin")
	if err != nil {
		return nil, err
	}
	repo := ""
	for _, pattern := range []*regexp.Regexp{checkoutSSHRemote, checkoutHTTPSRemote} {
		if match := pattern.FindStringSubmatch(remote); match != nil {
			repo = match[1] + "/" + strings.TrimSuffix(match[2], ".git")
			break
		}
	}
	if repo == "" {
		return nil, fmt.Errorf("checkout origin is not a GitHub repository")
	}
	branch, err := g.git(directory, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return nil, err
	}
	sha, err := g.git(directory, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	return &store.CodeContext{Repo: repo, Branch: branch, HeadSHA: sha, WorkingDir: directory}, nil
}

func (g *GitCheckoutInspector) git(directory string, args ...string) (string, error) {
	cmd := g.execCommand("git", args...)
	cmd.Dir = directory
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(output)), err)
	}
	return strings.TrimSpace(string(output)), nil
}
