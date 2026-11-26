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
	if m == nil {
		return ""
	}
	prs, err := m(ctx)
	if err != nil {
		log.Printf("reading pull_requests.rules: %v", err)
		return ""
	}
	return prs.ModeFor(repo)
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
	if m == nil {
		return false
	}
	prs, err := m(ctx)
	if err != nil {
		log.Printf("reading pull_requests.rules: %v", err)
		return false
	}
	return prs.ReadyAfterReview(repo)
}

// pullRequestModePrompt is the launch prompt section for a repository's
// pull request mode; "" when the agent chooses.
func pullRequestModePrompt(repo, mode string) string {
	name := strings.TrimPrefix(repo, "github.com/")
	switch mode {
	case config.PullRequestsDraft:
		return "## Pull requests\n\nOpen pull requests in " + name + " as drafts (`gh pr create --draft`): Legato refuses any other there."
	case config.PullRequestsReady:
		return "## Pull requests\n\nOpen pull requests in " + name + " ready for review, not as drafts."
	}
	return ""
}

// pullRequestDraftRefusal is the broker's reason for refusing a pull request
// that is not a draft in a repository whose mode is draft.
func pullRequestDraftRefusal(repo string) string {
	return repo + " opens pull requests as drafts (pull_requests.rules in Legato's settings): open it with gh pr create --draft, or \"draft\": true"
}
