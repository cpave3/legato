package service

import (
	"context"
	"slices"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// agentAuthoredPRs says whether a card's pull requests are its agent's, so
// that a review loop on them asks the card's agent to fix the findings
// (agent_author): only when Legato's author App opened every one of them
// (spec LEG-559 D8), so a coworker's or the operator's own pull request is
// never handed to an agent. rows are the card's tracked branches with their
// pull requests; appLogins are the logins of Legato's own GitHub Apps.
//
// GitHub's word decides when Legato has it (PRMeta.Author, kept by PR
// tracking): one of Legato's Apps (learned, or github.app_logins) is the
// agent; a person or any other App is not. Only before a refresh has read
// it does the branch's tracked source stand in: a branch the card's agent
// pushed or opened a pull request from through Legato's credential proxy,
// which acts as the author App (agent), or a Staccato stack of one of its
// job clones, which push through that proxy (stack). The branch of the
// card's checkout (code_context) never does, since the operator may have
// opened its pull request from their own checkout.
func agentAuthoredPRs(rows []store.TaskBranch, prs []PRMetaView, appLogins []string) bool {
	if len(prs) == 0 {
		return false
	}
	for _, pr := range prs {
		row, ok := trackedRowOf(rows, pr)
		if !ok || !agentOpened(row, appLogins) {
			return false
		}
	}
	return true
}

func trackedRowOf(rows []store.TaskBranch, pr PRMetaView) (store.TaskBranch, bool) {
	for _, row := range rows {
		if row.PR != nil && row.PR.PRNumber == pr.PRNumber && strings.EqualFold(row.Repo, pr.Repo) {
			return row, true
		}
	}
	return store.TaskBranch{}, false
}

func agentOpened(row store.TaskBranch, appLogins []string) bool {
	author := strings.TrimSpace(row.PR.Author)
	if author != "" {
		// GitHub named who opened it: one of Legato's Apps is the agent;
		// a person, or any other App (Dependabot, say), is not.
		return anyLogin(appLogins, author)
	}
	switch row.Source {
	case BranchSourceAgent, BranchSourceStack:
		return true
	}
	return false
}

// ReviewAuthors gives the card menu, the TUI and GET /api/review-loops the
// agent_author a review started now would take (agentAuthoredPRs), from
// what PR tracking keeps: no GitHub read. appLogins are the configured
// logins of Legato's own Apps (github.app_logins); those learned from the
// reviews Legato posted are read at each ask.
type ReviewAuthors struct {
	db        *store.Store
	appLogins []string
}

// NewReviewAuthors reads db, with the configured App logins.
func NewReviewAuthors(db *store.Store, appLogins []string) *ReviewAuthors {
	return &ReviewAuthors{db: db, appLogins: botLogins(appLogins)}
}

// logins are Legato's own Apps' logins: those learned, then configured.
func (r *ReviewAuthors) logins(ctx context.Context) []string {
	learned, _ := r.db.GitHubAppLogins(ctx)
	return append(learned, r.appLogins...)
}

// Card says whether a card's pull requests prs are its agent's, from its
// tracked branches rows.
func (r *ReviewAuthors) Card(ctx context.Context, rows []store.TaskBranch, prs []PRMetaView) bool {
	if r == nil {
		return false
	}
	return agentAuthoredPRs(rows, prs, r.logins(ctx))
}

// AgentAuthor says whether repo's pull request number is an agent's: the
// board card that tracks it says so (its first such card, by ID). A pull
// request no card tracks is a person's.
func (r *ReviewAuthors) AgentAuthor(ctx context.Context, repo string, number int) bool {
	if r == nil {
		return false
	}
	tracked, err := r.db.ListTrackedTaskBranches(ctx)
	if err != nil {
		return false
	}
	ids := make([]string, 0, len(tracked))
	for id := range tracked {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	pr := PRMetaView{Repo: repo, PRNumber: number}
	for _, id := range ids {
		if _, ok := trackedRowOf(tracked[id], pr); ok {
			return agentAuthoredPRs(tracked[id], []PRMetaView{pr}, r.logins(ctx))
		}
	}
	return false
}
