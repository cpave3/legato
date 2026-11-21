package service

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
)

func (f githubFixture) pullRequestMode(mode string) {
	f.broker.SetPullRequestModes(pullRequestRules(config.PullRequestRule{Match: "CPave3/Legato", Mode: mode}))
}

// With draft, a pull request that is not a draft is refused at once, by
// the API and by gh, with a reason that names the setting and --draft;
// drafts are allowed as before.
func TestADraftRepositoryRefusesPullRequestsThatAreNotDrafts(t *testing.T) {
	f := newGitHubFixture(t)
	f.pullRequestMode(config.PullRequestsDraft)
	grantHandoffBranches(t, f, "")
	status, body := f.openPull(t, `{"head":"legato/k7q2","base":"main","title":"Fix"}`)
	if status != 403 || !strings.Contains(body, "pull_requests.rules") || !strings.Contains(body, "--draft") || strings.Contains(body, "pending") {
		t.Fatalf("pull request that is not a draft: %d %s", status, body)
	}
	tokenStatus, out := f.token(t, `{"repo":"cpave3/legato","operation":"pr.create","branch":"legato/k7q2"}`)
	if tokenStatus != 403 || out["status"] != "denied" || !strings.Contains(out["error"], "gh pr create --draft") {
		t.Fatalf("gh's token: %d %v", tokenStatus, out)
	}
	if status, body := f.openPull(t, `{"head":"legato/k7q2","base":"main","title":"Fix","draft":true}`); status != 201 {
		t.Fatalf("draft pull request: %d %s", status, body)
	}
	f.noApprovals(t)
}

// With ready, a handoff's grant covers opening pull requests that are not
// drafts from its own branches: no more asking than for drafts.
func TestAReadyRepositorysHandoffOpensReadyPullRequestsWithoutAsking(t *testing.T) {
	f := newGitHubFixture(t)
	f.pullRequestMode(config.PullRequestsReady)
	grantHandoffBranches(t, f, "")
	if status, body := f.openPull(t, `{"head":"legato/k7q2-login-fix","base":"main","title":"Fix"}`); status != 201 {
		t.Fatalf("ready pull request: %d %s", status, body)
	}
	// gh pr create, without --draft, asks for pr.create on its branch.
	status, body := f.token(t, `{"repo":"cpave3/legato","operation":"pr.create","branch":"legato/k7q2"}`)
	if perms := f.github.tokens[body["token"]]; status != 200 || perms["pull_requests"] != "write" || perms["contents"] != "read" {
		t.Fatalf("gh's token: %d %v, permissions %v", status, body, perms)
	}
	// Drafts are still allowed.
	if status, body := f.openPull(t, `{"head":"legato/k7q2","base":"main","title":"Fix","draft":true}`); status != 201 {
		t.Fatalf("draft pull request: %d %s", status, body)
	}
	f.noApprovals(t)
	// Other branches still ask.
	if status, body := f.openPull(t, `{"head":"legato/other","base":"main","title":"Fix"}`); status != 403 || !strings.Contains(body, "pending") {
		t.Fatalf("another branch: %d %s", status, body)
	}
}

// Without a mode, a handoff's grant covers drafts only, as before.
func TestWithoutAModeAHandoffGrantCoversDraftsOnly(t *testing.T) {
	f := newGitHubFixture(t)
	f.pullRequestMode(config.PullRequestsReady)
	f.broker.SetPullRequestModes(nil)
	grantHandoffBranches(t, f, "")
	if status, body := f.openPull(t, `{"head":"legato/k7q2","base":"main","title":"Fix"}`); status != 403 || !strings.Contains(body, "pending") {
		t.Fatalf("ready pull request was not held for the person: %d %s", status, body)
	}
}

// The operator's own rules still come first: a credential whose rules do
// not allow pr.create refuses it, whatever the mode grants.
func TestTheOperatorsRulesWinOverAReadyMode(t *testing.T) {
	f := newGitHubFixtureWith(t, func(c *config.CredentialConfig) {
		c.Policy = []config.CredentialRule{
			{Operation: config.StringList{"git.read", "api.read"}, Resource: "github:cpave3/legato", Approve: "auto"},
			{Operation: config.StringList{"git.push", "pr.create-draft"}, Resource: "github:cpave3/legato@refs/heads/legato/**", Approve: "session"},
			{Operation: config.StringList{"pr.create"}, Approve: "deny"},
		}
	})
	f.pullRequestMode(config.PullRequestsReady)
	grantHandoffBranches(t, f, "")
	if status, body := f.openPull(t, `{"head":"legato/k7q2","base":"main","title":"Fix"}`); status != 403 || strings.Contains(body, "pending") {
		t.Fatalf("ready pull request was not refused: %d %s", status, body)
	}
	if status, body := f.openPull(t, `{"head":"legato/k7q2","base":"main","title":"Fix","draft":true}`); status != 201 {
		t.Fatalf("draft pull request: %d %s", status, body)
	}
	f.noApprovals(t)
}
