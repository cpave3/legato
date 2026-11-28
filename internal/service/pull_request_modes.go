package service

import (
	"context"
	"log"
	"strings"

	"github.com/cpave3/legato/config"
)

// PullRequestModeSource says how a repository opens pull requests:
// config.PullRequestsDraft, Ready or Agent, "" when no rule names it.
type PullRequestModeSource interface {
	PullRequestMode(ctx context.Context, repo string) string
}

// PullRequestModes reads the pull_requests rules each time it is asked, so
// a rule saved in Settings applies to the next agent and the next use.
type PullRequestModes func(ctx context.Context) (config.PullRequestsConfig, error)

// PullRequestMode implements PullRequestModeSource. A nil PullRequestModes,
// or rules that cannot be read, give "".
func (m PullRequestModes) PullRequestMode(ctx context.Context, repo string) string {
	prs, ok := m.rules(ctx)
	if !ok {
		return ""
	}
	return prs.ModeFor(repo)
}

// rules reads the pull_requests rules; false with no PullRequestModes or
// rules that cannot be read (logged), so each question gives the answer
// that changes nothing.
func (m PullRequestModes) rules(ctx context.Context) (config.PullRequestsConfig, bool) {
	if m == nil {
		return config.PullRequestsConfig{}, false
	}
	prs, err := m(ctx)
	if err != nil {
		log.Printf("reading pull_requests.rules: %v", err)
		return config.PullRequestsConfig{}, false
	}
	return prs, true
}

// ReadyAfterReviewSource says whether a repository's handed-off stacks are
// marked ready once review passes approve them: a draft rule with
// ready_after_review (LEG-456).
type ReadyAfterReviewSource interface {
	ReadyAfterReview(ctx context.Context, repo string) bool
}

// ReadyAfterReview implements ReadyAfterReviewSource. A nil
// PullRequestModes, or rules that cannot be read, give false: the stack is
// left a draft.
func (m PullRequestModes) ReadyAfterReview(ctx context.Context, repo string) bool {
	prs, ok := m.rules(ctx)
	return ok && prs.ReadyAfterReview(repo)
}

// pullRequestModePrompt is the launch prompt section for a repository's
// pull request mode; "" when the agent chooses.
func pullRequestModePrompt(repo, mode string) string {
	name := strings.TrimPrefix(repo, "github.com/")
	switch mode {
	case config.PullRequestsDraft:
		return "## Pull requests\n\nOpen pull requests in " + name + " as drafts (`gh pr create --draft`): Legato refuses any other there."
	case config.PullRequestsReady:
		return "## Pull requests\n\nOpen pull requests in " + name + " ready for review, not as drafts. " + reviewLoopKeepsDraft
	}
	return ""
}

// reviewLoopKeepsDraft tells an author who opens pull requests ready not
// to undo the draft an agent's review loop keeps until approval.
const reviewLoopKeepsDraft = "While a Legato review loop holds one, Legato keeps it a draft until it approves the head: do not mark it ready yourself."

// pullRequestDraftRefusal is the broker's reason for refusing a pull request
// that is not a draft in a repository whose mode is draft.
func pullRequestDraftRefusal(repo string) string {
	return repo + " opens pull requests as drafts (pull_requests.rules in Legato's settings): open it with gh pr create --draft, or \"draft\": true"
}
