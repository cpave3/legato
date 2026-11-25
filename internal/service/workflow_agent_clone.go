package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// Clones are explicit (spec LEG-482 §5.2, LEG-485). In a version 2 run an
// agent.run step with repo gets a fresh clone at ref, never one its task
// already has; clone: {from: <step>} joins the clone an earlier agent step
// worked in, and clone: <ID> a clone by its ID. A branch the step names is
// made as legato/<task>-<name>, the prefix the GitHub credential grants,
// and granted to the agent's session for pushes and draft pull requests
// (GrantBranches), so it pushes without asking.

// BranchGranter grants a session pushes and draft pull requests on some
// branches: the credential broker.
type BranchGranter interface {
	GrantBranches(ctx context.Context, g BranchGrant) (int, error)
}

// SetBranchGrants lets version 2 steps grant their branches.
func (e *AgentRunExecutor) SetBranchGrants(grants BranchGranter) { e.grants = grants }

// WorkflowBranch is the branch a version 2 step's branch name makes for a
// task: legato/<task>-<name>, or the name when it already says legato/.
func WorkflowBranch(taskID, name string) string {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "legato/") {
		return name
	}
	return "legato/" + strings.ToLower(taskID) + "-" + name
}

// stepCloneOptions makes a version 2 step's clone explicit: the one it
// names, else a fresh one, on a legato/ branch.
func stepCloneOptions(step ResolvedWorkflowStep, taskID string, opts *AgentSpawnOptions) {
	if step.SchemaVersion < 2 {
		return
	}
	switch {
	case step.Clone != nil:
		opts.Clone = step.Clone.ID
		if opts.Repo == "" {
			opts.Repo = step.Clone.Repo
		}
	case opts.Repo != "":
		opts.OwnClone = true
	}
	opts.Branch = WorkflowBranch(taskID, opts.Branch)
}

// grantBranch grants the session the branch a version 2 step named. A grant
// that fails is logged: the session then asks as any other would.
func (e *AgentRunExecutor) grantBranch(ctx context.Context, step ResolvedWorkflowStep, taskID string, opts AgentSpawnOptions, session *store.AgentSession) {
	if e.grants == nil || step.SchemaVersion < 2 || opts.Branch == "" || opts.Repo == "" || session == nil || session.ExecutionID == "" {
		return
	}
	if _, err := e.grants.GrantBranches(ctx, BranchGrant{TaskID: taskID, ExecutionID: session.ExecutionID, Repo: opts.Repo,
		Branches: []string{opts.Branch}, By: "workflow run " + step.RunID + " step " + step.StepID}); err != nil {
		log.Printf("workflow run %s step %s: granting %s: %v", step.RunID, step.StepID, opts.Branch, err)
	}
}

// resolveStepClone finds the job clone an agent.run step's with.clone
// names: an earlier step's clone_id ({from: <step>}), or a clone ID. Null,
// 0 or no clone is a fresh one.
func (s *WorkflowService) resolveStepClone(ctx context.Context, prior map[string]map[string]any, step *ResolvedWorkflowStep) error {
	value, ok := step.With["clone"]
	if step.Type != "agent.run" || !ok || value == nil {
		return nil
	}
	if from, isFrom := workflowengine.AgentFrom(value); isFrom {
		value = prior[from]["clone_id"]
		if cloneNumber(value) == 0 {
			return fmt.Errorf("with clone: step %s worked in no job clone", from)
		}
	}
	id := cloneNumber(value)
	if id == 0 {
		if text, isText := value.(string); isText && strings.TrimSpace(text) != "" && strings.TrimSpace(text) != "0" {
			return fmt.Errorf("with clone: %q is not a clone ID", text)
		}
		return nil
	}
	clone, err := s.store.JobCloneByID(ctx, id)
	if err != nil {
		return fmt.Errorf("with clone: job clone %d: %w", id, err)
	}
	step.Clone = clone
	return nil
}

// cloneNumber is a clone ID as a step's outputs or an expression give it.
func cloneNumber(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	}
	return 0
}
