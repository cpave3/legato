package service

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Scopes of a run's review loop: the whole stack the named pull request is
// in, or that pull request alone.
const (
	ReviewScopeStack = "stack"
	ReviewScopePR    = "pr"
)

// reviewRunPR is one pull request a run's loop reviews, and the branch of
// its clone that is put at the pull request's head.
type reviewRunPR struct {
	Number int    `json:"number"`
	Branch string `json:"branch"`
}

// prs are the run's pull requests, bottom of the stack first. A run started
// before stacks keeps only PR and Branch: a stack of one.
func (r *reviewRun) prs() []reviewRunPR {
	if len(r.Stack) > 0 {
		return r.Stack
	}
	return []reviewRunPR{{Number: r.PR, Branch: r.Branch}}
}

func (r *reviewRun) numbers() []int {
	var out []int
	for _, p := range r.prs() {
		out = append(out, p.Number)
	}
	return out
}

// reviewStackName names pull requests of a repository: owner/name#12, or
// owner/name#137–#141 for a stack, bottom to top.
func reviewStackName(repo string, numbers []int) string {
	switch len(numbers) {
	case 0:
		return repo
	case 1:
		return fmt.Sprintf("%s#%d", repo, numbers[0])
	}
	return fmt.Sprintf("%s#%d–#%d", repo, numbers[0], numbers[len(numbers)-1])
}

// reviewScope reads the step's scope: stack when it is not set.
func reviewScope(with map[string]any) (string, error) {
	scope, _ := with["scope"].(string)
	switch scope = strings.TrimSpace(strings.ToLower(scope)); scope {
	case "", ReviewScopeStack:
		return ReviewScopeStack, nil
	case ReviewScopePR:
		return ReviewScopePR, nil
	}
	return "", fmt.Errorf("scope is stack or pr, not %q", scope)
}

// openRunPRs are the run's pull requests still open on GitHub, in stack
// order; one that closed or merged drops out.
func (c *ReviewRunCaller) openRunPRs(ctx context.Context, run *reviewRun) ([]ReviewGitHubPR, error) {
	var out []ReviewGitHubPR
	for _, p := range run.prs() {
		pr, state, err := c.opts.GitHub.PullRequest(ctx, run.Repo, p.Number)
		if err != nil {
			return nil, err
		}
		if state == "open" {
			pr.Number = p.Number
			out = append(out, pr)
		}
	}
	return out, nil
}

// pinnedHeads are the heads the loop's last pass reviewed, by pull request.
func (c *ReviewRunCaller) pinnedHeads(ctx context.Context, taskID string) map[int]string {
	out := map[int]string{}
	loop, err := c.engine.db.GetHandoffReview(ctx, taskID)
	if err != nil || loop == nil {
		return out
	}
	prs, err := c.engine.db.ListHandoffReviewPRs(ctx, taskID, loop.Pass)
	if err != nil {
		return out
	}
	for _, pr := range prs {
		out[pr.Number] = pr.HeadSHA
	}
	return out
}

// moved says whether an open pull request's head is not the one the last
// pass pinned (or it was not pinned at all).
func moved(open []ReviewGitHubPR, pinned map[int]string) bool {
	for _, pr := range open {
		if pinned[pr.Number] != pr.HeadSHA {
			return true
		}
	}
	return false
}

// headsKey is the heads of open pull requests in one string, which the quiet
// time watches: any push to any of them starts it again.
func headsKey(open []ReviewGitHubPR) string {
	parts := make([]string, 0, len(open))
	for _, pr := range open {
		parts = append(parts, fmt.Sprintf("%d:%s", pr.Number, pr.HeadSHA))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// runStack is what a run started on a pull request reviews: with scope
// stack, the stack GitHub's chain gives from its branch (that pull request
// alone when it is in none, a fork's say); with scope pr, the pull request.
func (c *ReviewRunCaller) runStack(ctx context.Context, repo string, pr ReviewGitHubPR, scope string) ([]ReviewGitHubPR, error) {
	if scope != ReviewScopeStack {
		return []ReviewGitHubPR{pr}, nil
	}
	stack, err := FindReviewStack(ctx, c.engine.opts.GitHub, repo, []string{pr.Branch}, nil)
	if err != nil {
		return nil, fmt.Errorf("finding the stack of #%d: %w", pr.Number, err)
	}
	if !slices.ContainsFunc(stack.PRs, func(p ReviewGitHubPR) bool { return p.Number == pr.Number }) {
		return []ReviewGitHubPR{pr}, nil
	}
	return stack.PRs, nil
}
