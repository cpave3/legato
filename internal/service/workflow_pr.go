package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/store"
)

type workflowPRFetcher interface {
	FetchPRByNumber(owner, repo string, number int) (*github.PRStatus, error)
}
type workflowExactFetcher interface {
	Fetch(context.Context, string, string, string, string) error
}
type workflowWorktrees interface {
	Create(context.Context, string, string, string, string) (*store.TaskWorktree, error)
	Remove(context.Context, string, bool) error
}
type workflowPRLinker interface {
	LinkPR(context.Context, string, string, string, int) error
}

type UnavailableGitHubPRPrepareExecutor struct{ err error }

func NewUnavailableGitHubPRPrepareExecutor(err error) *UnavailableGitHubPRPrepareExecutor {
	if err == nil {
		err = fmt.Errorf("run gh auth login or configure github.token")
	}
	return &UnavailableGitHubPRPrepareExecutor{err: err}
}
func (e *UnavailableGitHubPRPrepareExecutor) Type() string { return "github.pr.prepare" }
func (e *UnavailableGitHubPRPrepareExecutor) Start(context.Context, ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	return WorkflowExecutionResult{}, fmt.Errorf("GitHub authentication unavailable: %w", e.err)
}
func (e *UnavailableGitHubPRPrepareExecutor) Resume(context.Context, store.WorkflowStepAttempt, map[string]any) (WorkflowExecutionResult, error) {
	return WorkflowExecutionResult{}, fmt.Errorf("GitHub authentication unavailable: %w", e.err)
}
func (e *UnavailableGitHubPRPrepareExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error {
	return nil
}

type GitHubPRPrepareExecutor struct {
	github    workflowPRFetcher
	exact     workflowExactFetcher
	worktrees workflowWorktrees
	links     workflowPRLinker
}

func NewGitHubPRPrepareExecutor(g workflowPRFetcher, exact workflowExactFetcher, worktrees workflowWorktrees, links workflowPRLinker) *GitHubPRPrepareExecutor {
	return &GitHubPRPrepareExecutor{github: g, exact: exact, worktrees: worktrees, links: links}
}
func (e *GitHubPRPrepareExecutor) Type() string { return "github.pr.prepare" }

func (e *GitHubPRPrepareExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	repo, err := workflowString(step.With, "repo", true)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return WorkflowExecutionResult{}, fmt.Errorf("repo must be owner/name")
	}
	number, err := workflowInt(step.With["number"])
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("number: %w", err)
	}
	primaryDir, err := workflowString(step.With, "primary_dir", true)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	base, _ := workflowString(step.With, "base_branch", false)
	if base == "" {
		base = "main"
	}
	status, err := e.github.FetchPRByNumber(parts[0], parts[1], number)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if status.HeadSHA == "" || status.HeadRepo == "" {
		return WorkflowExecutionResult{}, fmt.Errorf("PR #%d did not include exact head identity", number)
	}
	ref := "refs/legato/workflows/" + step.RunID + "/pr-" + strconv.Itoa(number)
	if err := e.exact.Fetch(ctx, primaryDir, status.HeadRepo, status.HeadSHA, ref); err != nil {
		return WorkflowExecutionResult{}, err
	}
	branch := "legato-" + step.RunID + "-pr-" + strconv.Itoa(number)
	worktree, err := e.worktrees.Create(ctx, step.TaskID, primaryDir, branch, ref)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if err := e.links.LinkPR(ctx, step.TaskID, parts[0], parts[1], number); err != nil {
		_ = e.worktrees.Remove(ctx, step.TaskID, false)
		return WorkflowExecutionResult{}, fmt.Errorf("linking workflow task to PR: %w", err)
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{
		"worktree_path": worktree.Path, "branch": worktree.Branch, "base_branch": base, "head_sha": status.HeadSHA,
		"metadata": map[string]any{"number": status.Number, "title": status.Title, "url": status.URL, "head_repo": status.HeadRepo, "head_branch": status.HeadBranch},
	}}, nil
}
func (e *GitHubPRPrepareExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error {
	return nil
}

func workflowInt(value any) (int, error) {
	switch value := value.(type) {
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case float64:
		return int(value), nil
	default:
		return 0, fmt.Errorf("must be an integer")
	}
}
