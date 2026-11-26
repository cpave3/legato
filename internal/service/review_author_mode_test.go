package service

import (
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// D8: a pull request is the agent's only when Legato's author App opened
// it. An author GitHub names that is not one of Legato's Apps (a person,
// or another App such as Dependabot) is a person's, whatever the branch's
// source; the source stands in only while no author is recorded.
func TestAPullRequestIsTheAgentsOnlyWhenLegatosAppOpenedIt(t *testing.T) {
	apps := []string{"legato-author[bot]"}
	for _, tc := range []struct {
		name, author, source string
		want                 bool
	}{
		{"Legato's App, the card's checkout", "legato-author[bot]", BranchSourceCodeContext, true},
		{"Legato's App, the agent's push", "legato-author[bot]", BranchSourceAgent, true},
		{"a person, the agent's push", "octocat", BranchSourceAgent, false},
		{"another App, the card's checkout", "dependabot[bot]", BranchSourceCodeContext, false},
		{"another App, the agent's push", "dependabot[bot]", BranchSourceAgent, false},
		{"another App, a stack", "renovate[bot]", BranchSourceStack, false},
		{"not read yet, the agent's push", "", BranchSourceAgent, true},
		{"not read yet, a stack", "", BranchSourceStack, true},
		{"not read yet, the card's checkout", "", BranchSourceCodeContext, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := PRMetaView{Repo: "acme/widget", PRNumber: 12}
			rows := []store.TaskBranch{{Repo: "acme/widget", Source: tc.source, PR: &store.PRMeta{PRNumber: 12, Author: tc.author}}}
			if got := agentAuthoredPRs(rows, []PRMetaView{pr}, apps); got != tc.want {
				t.Fatalf("agent = %v, want %v", got, tc.want)
			}
		})
	}
}
