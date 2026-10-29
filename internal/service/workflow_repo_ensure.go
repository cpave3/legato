package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// RepoEnsureExecutor prepares the checkout a job works in. Without a
// repository it does nothing and passes the working directory on.
type RepoEnsureExecutor struct{}

func NewRepoEnsureExecutor() *RepoEnsureExecutor { return &RepoEnsureExecutor{} }

func (e *RepoEnsureExecutor) Type() string { return "repo.ensure" }

func (e *RepoEnsureExecutor) Start(_ context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	repo, err := workflowString(step.With, "repo", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	workingDir, err := workflowString(step.With, "working_dir", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if strings.TrimSpace(repo) == "" {
		return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"working_dir": workingDir}}, nil
	}
	return WorkflowExecutionResult{}, fmt.Errorf("repository %s: Legato cannot check out a repository for a job yet; leave repo empty and give working_dir", repo)
}

func (e *RepoEnsureExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }
