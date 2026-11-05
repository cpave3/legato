package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// grantHandoffBranches grants the fixture's session what a make-a-change
// handoff of main task K7Q2 is approved for.
func grantHandoffBranches(t *testing.T, f githubFixture, given string) {
	t.Helper()
	n, err := f.broker.GrantBranches(context.Background(), BranchGrant{TaskID: "task-1", ExecutionID: "ex-1", Repo: "cpave3/legato",
		Branches: HandoffGrantBranches("K7Q2", given), By: "handoff hf-1"})
	if err != nil || n != 1 {
		t.Fatalf("GrantBranches = %d, %v", n, err)
	}
}

func (f githubFixture) noApprovals(t *testing.T) {
	t.Helper()
	list, _ := f.approvals.List(context.Background(), store.ApprovalFilter{Status: store.ApprovalPending})
	if len(list) != 0 {
		t.Fatalf("the person was asked: %+v", list)
	}
}

// openPull asks the proxy's REST route to open a pull request.
func (f githubFixture) openPull(t *testing.T, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", f.proxy.URL+"/c/gh/api/repos/cpave3/legato/pulls", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+f.env["LEGATO_GITHUB_TOKEN"])
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(data)
}

// token asks the proxy's token route, as gh's wrapper does.
func (f githubFixture) token(t *testing.T, body string) (int, map[string]string) {
	t.Helper()
	req, _ := http.NewRequest("POST", f.proxy.URL+"/c/gh/token", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+f.env["LEGATO_GITHUB_TOKEN"])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestHandoffBranchesAreTheMainTasksBranchAndItsStack(t *testing.T) {
	if got := HandoffGrantBranches("K7Q2", ""); strings.Join(got, " ") != "legato/k7q2 legato/k7q2-*" {
		t.Fatalf("new branch: %v", got)
	}
	if got := HandoffGrantBranches("K7Q2", "feature/login"); strings.Join(got, " ") != "feature/login legato/k7q2 legato/k7q2-*" {
		t.Fatalf("given branch: %v", got)
	}
}

func TestAHandoffGrantPushesTheMainTasksBranchesWithoutAsking(t *testing.T) {
	f := newGitHubFixture(t)
	grantHandoffBranches(t, f, "")
	for _, branch := range []string{"legato/k7q2", "legato/k7q2-login-fix"} {
		if out, err := f.push(t, "HEAD:refs/heads/"+branch); err != nil {
			t.Fatalf("push %s: %v\n%s", branch, err, out)
		}
	}
	f.noApprovals(t)
	if got := ghGit(t, f.github.bare, "branch", "--list"); !strings.Contains(got, "legato/k7q2-login-fix") {
		t.Fatalf("branches = %q", got)
	}
}

func TestAHandoffGrantOpensDraftPullRequestsWithoutAsking(t *testing.T) {
	f := newGitHubFixture(t)
	grantHandoffBranches(t, f, "")
	if status, body := f.openPull(t, `{"head":"legato/k7q2-login-fix","base":"main","title":"Fix","draft":true}`); status != 201 {
		t.Fatalf("draft pull request: %d %s", status, body)
	}
	// gh pr create --draft asks for a token for that one branch.
	status, body := f.token(t, `{"repo":"cpave3/legato","operation":"pr.create-draft","branch":"legato/k7q2"}`)
	if perms := f.github.tokens[body["token"]]; status != 200 || perms["pull_requests"] != "write" || perms["contents"] != "read" {
		t.Fatalf("gh's draft token: %d %v, permissions %v", status, body, perms)
	}
	f.noApprovals(t)
}

func TestAHandoffGrantAllowsNothingWider(t *testing.T) {
	f := newGitHubFixture(t)
	grantHandoffBranches(t, f, "")
	// The default branch and tags stay refused, without asking.
	for _, refspec := range []string{"HEAD:refs/heads/main", "HEAD:refs/tags/v1"} {
		if out, err := f.push(t, refspec); err == nil || strings.Contains(out, "pending ap-") {
			t.Fatalf("push %s was not refused: %v\n%s", refspec, err, out)
		}
	}
	f.noApprovals(t)
	// Other branches, a further /, a pull request that is not a draft, and
	// gh's other pull request writes ask as they do for any session.
	asks := []func() (bool, string){
		func() (bool, string) {
			out, err := f.push(t, "HEAD:refs/heads/legato/other-task")
			return err != nil && strings.Contains(out, "pending ap-"), out
		},
		func() (bool, string) {
			out, err := f.push(t, "HEAD:refs/heads/legato/k7q2-a/b")
			return err != nil && strings.Contains(out, "pending ap-"), out
		},
		func() (bool, string) {
			status, body := f.openPull(t, `{"head":"legato/k7q2","base":"main","title":"Fix"}`)
			return status == 403 && strings.Contains(body, "pending"), body
		},
		func() (bool, string) {
			status, body := f.openPull(t, `{"head":"legato/other","base":"main","title":"Fix","draft":true}`)
			return status == 403 && strings.Contains(body, "pending"), body
		},
		func() (bool, string) {
			status, body := f.token(t, `{"repo":"cpave3/legato","operation":"pr.write"}`)
			return status == 403 && body["status"] == "pending", body["error"]
		},
	}
	for i, ask := range asks {
		if asked, out := ask(); !asked {
			t.Fatalf("use %d was not held for the person:\n%s", i, out)
		}
	}
}

func TestAHandoffGrantIsAStandingGrantThePersonCanSeeAndEnd(t *testing.T) {
	f := newGitHubFixture(t)
	grantHandoffBranches(t, f, "")
	grants, err := f.broker.Grants(context.Background())
	if err != nil || len(grants) != 1 {
		t.Fatalf("grants = %+v, %v", grants, err)
	}
	g := grants[0]
	if g.Mode != "session" || strings.Join(g.Operations, ",") != "git.push,pr.create-draft" ||
		strings.Join(g.Resources, " ") != "github:cpave3/legato@refs/heads/legato/k7q2 github:cpave3/legato@refs/heads/legato/k7q2-*" {
		t.Fatalf("grant = %+v", g)
	}
	audit, _ := f.broker.Audit(context.Background(), store.CredentialAuditFilter{})
	if len(audit) == 0 || audit[0].Event != "grant" || audit[0].DecidedBy != "handoff hf-1" {
		t.Fatalf("audit = %+v", audit)
	}
	if err := f.broker.EndGrant(context.Background(), g.ID, "web"); err != nil {
		t.Fatal(err)
	}
	if out, err := f.push(t, "HEAD:refs/heads/legato/k7q2"); err == nil || !strings.Contains(out, "pending ap-") {
		t.Fatalf("an ended grant still pushes: %v\n%s", err, out)
	}
}

func TestABranchGrantOnARepositoryNoCredentialListsGrantsNothing(t *testing.T) {
	f := newGitHubFixture(t)
	n, err := f.broker.GrantBranches(context.Background(), BranchGrant{TaskID: "task-1", ExecutionID: "ex-1", Repo: "acme/other",
		Branches: HandoffGrantBranches("K7Q2", ""), By: "handoff hf-1"})
	if err != nil || n != 0 {
		t.Fatalf("GrantBranches = %d, %v", n, err)
	}
	if grants, _ := f.broker.Grants(context.Background()); len(grants) != 0 {
		t.Fatalf("grants = %+v", grants)
	}
}
