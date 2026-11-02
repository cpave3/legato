package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// RepoCache makes a repository's cache ready (RepositoryService).
type RepoCache interface {
	Ensure(ctx context.Context, repo string) (string, error)
}

// RepoEnsureExecutor prepares what a job works in. Without a repository it
// does nothing and passes the working directory on. With one, it makes the
// repository's cache ready and passes the repository and ref on: the
// agent step starts its agent in a clone made for the agent's execution.
type RepoEnsureExecutor struct {
	cache RepoCache
}

// NewRepoEnsureExecutor builds the step; cache is nil when Legato has no
// GitHub credential to clone with.
func NewRepoEnsureExecutor(cache RepoCache) *RepoEnsureExecutor {
	return &RepoEnsureExecutor{cache: cache}
}

func (e *RepoEnsureExecutor) Type() string { return "repo.ensure" }

func (e *RepoEnsureExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	repo, err := workflowString(step.With, "repo", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	ref, err := workflowString(step.With, "ref", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	branch, err := workflowString(step.With, "branch", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	workingDir, err := workflowString(step.With, "working_dir", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	repo, ref, branch = strings.TrimSpace(repo), strings.TrimSpace(ref), strings.TrimSpace(branch)
	if repo == "" {
		return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"working_dir": workingDir, "repo": "", "ref": "", "branch": ""}}, nil
	}
	if strings.TrimSpace(workingDir) != "" {
		return WorkflowExecutionResult{}, fmt.Errorf("a job works in a clone of %s or in %s, not both", repo, workingDir)
	}
	if e.cache == nil {
		return WorkflowExecutionResult{}, fmt.Errorf("%s: Legato clones repositories for jobs with a github credential, and none is configured", repo)
	}
	if _, err := e.cache.Ensure(ctx, repo); err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"working_dir": "", "repo": repo, "ref": ref, "branch": branch}}, nil
}

func (e *RepoEnsureExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }
