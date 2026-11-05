package service

import (
	"context"
	"errors"
	"strings"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/stackgraph"
	"github.com/cpave3/legato/internal/engine/store"
)

// branchGrantOperations are what a branch grant allows on its branches:
// pushing them and opening draft pull requests from them. Not pr.create (a
// pull request that is not a draft), pr.write, merging, the default branch,
// tags or deleting a branch.
var branchGrantOperations = []string{"git.push", credential.OperationPRCreateDraft}

// BranchGrant allows one session, without asking the person, to push some
// branches of a GitHub repository and to open draft pull requests from
// them: what a make-a-change handoff is approved for.
type BranchGrant struct {
	TaskID      string
	ExecutionID string
	// Repo is the GitHub repository, owner/name.
	Repo string
	// Branches are branch names, or patterns of one segment
	// (legato/k7q2-*), without refs/heads/.
	Branches []string
	// By names who allowed it, for the audit log: "handoff <handoff ID>".
	By string
}

// GrantBranches records a session grant of git.push and pr.create-draft on
// each of the grant's branches, on every GitHub credential whose
// repositories list the repository, and returns how many credentials it
// granted on (none when no credential lists it). Each use is still decided
// by the credential's rules first: a rule that refuses the default branch,
// tags or deleting branches refuses them whatever is granted, and a
// credential whose rules name no pr.create-draft never allows it. The
// grant ends with the session, or when the person ends it.
func (b *CredentialBroker) GrantBranches(ctx context.Context, g BranchGrant) (int, error) {
	if _, err := repoOf("github:" + g.Repo); err != nil {
		return 0, err
	}
	if len(g.Branches) == 0 {
		return 0, errors.New("a branch grant names at least one branch")
	}
	resources := make([]string, 0, len(g.Branches))
	for _, branch := range g.Branches {
		if !stackgraph.ValidBranchName(strings.TrimSuffix(branch, "*")) && !stackgraph.ValidBranchName(branch) {
			return 0, errors.New("a branch grant names " + branch + ", which is not a branch")
		}
		resources = append(resources, "github:"+g.Repo+"@refs/heads/"+branch)
	}
	granted := 0
	for _, c := range b.all() {
		if c.profile.Git == "" || !repoListed(c.config.Settings.Repositories, g.Repo) {
			continue
		}
		req := AccessRequest{TaskID: g.TaskID, ExecutionID: g.ExecutionID, Credential: c.config.Name,
			Operations: branchGrantOperations, Resources: resources}
		if err := b.grant(ctx, req, grantSession, 0, "", g.By); err != nil {
			return granted, err
		}
		granted++
	}
	return granted, nil
}

// HandoffGrantBranches are the branches a make-a-change handoff's session
// may push without asking: the given branch, when the work starts from one,
// then legato/<main task id> and legato/<main task id>-<a few words>.
func HandoffGrantBranches(mainTaskID, given string) []string {
	base := HandoffBranchBase(mainTaskID)
	var out []string
	if given != "" && given != base {
		out = append(out, given)
	}
	return append(out, base, base+"-*")
}

// HandoffBranchBase is the branch a handed-off task's work is named after:
// legato/<main task id>, lowercased as the main names its branch.
func HandoffBranchBase(mainTaskID string) string {
	return "legato/" + strings.ToLower(mainTaskID)
}

// SessionBranches is what an agent session is told of the branches it
// works on. The zero value is an ordinary session's: branches named
// legato/<its own task ID>-<a few words>, each push asked for.
type SessionBranches struct {
	// Base is the branch a handed-off task's work is named after,
	// legato/<main task id>: the session works on it and names new
	// branches Base-<a few words>.
	Base string
	// Given is the branch already on GitHub that the work starts from (a
	// handoff from a pushed branch), "" for a new branch: the session works
	// on it, only adds commits to it, and stacks new branches on top of it.
	Given string
	// Granted says pushing those branches and opening draft pull requests
	// from them is already allowed (a make-a-change handoff).
	Granted bool
}

// HandoffSessionBranches is what the session of a worker's task is told of
// its branches while the task has an open handoff: the main task's branch,
// granted for a make-a-change handoff. ok is false for any other task, and
// with no store.
func HandoffSessionBranches(ctx context.Context, db *store.Store, taskID string) (SessionBranches, bool) {
	if db == nil || taskID == "" {
		return SessionBranches{}, false
	}
	open, err := db.ListOpenHandoffs(ctx)
	if err != nil {
		return SessionBranches{}, false
	}
	for _, h := range open {
		if h.TaskID == taskID {
			return handoffSessionBranches(&h), true
		}
	}
	return SessionBranches{}, false
}

func handoffSessionBranches(h *store.Handoff) SessionBranches {
	branches := SessionBranches{Base: HandoffBranchBase(h.MainTaskID), Granted: h.Purpose == HandoffMakeChange}
	if h.Kind == HandoffPushedBranch {
		branches.Given = h.Branch
	}
	return branches
}
