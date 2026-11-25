package service

import (
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
