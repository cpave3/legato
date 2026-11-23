package service

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/gitrepo"
	"github.com/cpave3/legato/internal/engine/store"
)

// workflowsFixture is the GitHub fixture with a first commit on GitHub's
// main, the repository cache Legato reads pushes against, and an agent's
// clone of the repository.
type workflowsFixture struct {
	githubFixture
	work string
}

func newWorkflowsFixture(t *testing.T) workflowsFixture {
	t.Helper()
	f := newGitHubFixture(t)
	seed := t.TempDir()
	ghGit(t, seed, "init", "-q", "-b", "main")
	writeFile(t, seed, "README", "hello\n")
	writeFile(t, seed, ".github/workflows/ci.yml", "on: push\n")
	ghGit(t, seed, "add", ".")
	ghGit(t, seed, "-c", "user.name=a", "-c", "user.email=a@b", "commit", "-q", "-m", "first")
	ghGit(t, seed, "push", "-q", f.github.bare, "main")

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.github.srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSL_CAINFO", ca)
	repos := NewRepositoryService(RepositoryServiceOptions{DataDir: t.TempDir(), MaxAge: time.Minute, Tokens: f.channels,
		Git: gitrepo.Runner{AskPass: exe}, Store: f.store})
	f.channels.SetPushReader(repos)

	work := t.TempDir()
	ghGit(t, "", "clone", "-q", f.github.bare, work)
	return workflowsFixture{githubFixture: f, work: work}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commit commits a file in the agent's clone.
func (f workflowsFixture) commit(t *testing.T, name, content string) {
	t.Helper()
	writeFile(t, f.work, name, content)
	ghGit(t, f.work, "add", ".")
	ghGit(t, f.work, "-c", "user.name=a", "-c", "user.email=a@b", "commit", "-q", "-m", "change "+name)
}

// push pushes the agent's clone through the proxy.
func (f workflowsFixture) push(t *testing.T, refspec string) (string, error) {
	t.Helper()
	helper := "!f() { echo username=x-access-token; echo password=" + f.env["LEGATO_GITHUB_TOKEN"] + "; }; f"
	cmd := exec.Command("git", "-c", "credential.helper=", "-c", "credential.helper="+helper, "push", f.proxy.URL+"/c/gh/git/cpave3/legato.git", refspec)
	cmd.Dir = f.work
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (f workflowsFixture) allow(t *testing.T, approval Approval, option string) {
	t.Helper()
	if _, err := f.approvals.Decide(context.Background(), approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: option, By: "web"}); err != nil {
		t.Fatal(err)
	}
}

func approvalDetails(t *testing.T, approval Approval) credentialRequestDetails {
	t.Helper()
	var d credentialRequestDetails
	if err := json.Unmarshal(approval.Details, &d); err != nil {
		t.Fatal(err)
	}
	return d
}

func (f workflowsFixture) lastPush(t *testing.T) map[string]any {
	t.Helper()
	f.github.mu.Lock()
	defer f.github.mu.Unlock()
	if len(f.github.pushes) == 0 {
		t.Fatal("nothing reached GitHub")
	}
	return f.github.pushes[len(f.github.pushes)-1]
}

func TestAPushThatChangesAWorkflowIsAskedForOnceASessionAndPushedWithTheWorkflowsPermission(t *testing.T) {
	f := newWorkflowsFixture(t)
	f.commit(t, ".github/workflows/x.yml", "on: push\n")

	out, err := f.push(t, "HEAD:refs/heads/legato/task-1-ci")
	if err == nil || !strings.Contains(out, "pending ap-") {
		t.Fatalf("the push was not held for the operator: %v\n%s", err, out)
	}
	approval := f.pending(t)
	d := approvalDetails(t, approval)
	if len(d.Operations) != 1 || d.Operations[0] != "git.push-workflows" || d.Resources[0] != "github:cpave3/legato@refs/heads/legato/task-1-ci" {
		t.Fatalf("details = %+v", d)
	}
	if !strings.Contains(d.Reason, ".github/workflows/x.yml") {
		t.Fatalf("the approval does not name the workflow: %q", d.Reason)
	}
	if approval.Options[0].ID != "session" {
		t.Fatalf("options = %+v", approval.Options)
	}
	f.allow(t, approval, "session")

	if out, err := f.push(t, "HEAD:refs/heads/legato/task-1-ci"); err != nil {
		t.Fatalf("the allowed push failed: %v\n%s", err, out)
	}
	if perms := f.lastPush(t); perms["workflows"] != "write" || perms["contents"] != "write" {
		t.Fatalf("the push carried a token with %v", perms)
	}
	f.github.mu.Lock()
	defer f.github.mu.Unlock()
	for token, perms := range f.github.tokens {
		if perms["contents"] == "write" || perms["workflows"] == "write" {
			t.Fatalf("write token %s is still live", token)
		}
	}
}

func TestAnOrdinaryPushNeverCarriesTheWorkflowsPermission(t *testing.T) {
	f := newWorkflowsFixture(t)
	f.commit(t, "README", "changed\n")

	if out, err := f.push(t, "HEAD:refs/heads/legato/task-1-docs"); err == nil {
		t.Fatalf("the push was not held: %s", out)
	}
	approval := f.pending(t)
	if d := approvalDetails(t, approval); d.Operations[0] != "git.push" {
		t.Fatalf("details = %+v", d)
	}
	f.allow(t, approval, "session")
	if out, err := f.push(t, "HEAD:refs/heads/legato/task-1-docs"); err != nil {
		t.Fatalf("push failed: %v\n%s", err, out)
	}
	if perms := f.lastPush(t); perms["contents"] != "write" || perms["workflows"] != nil {
		t.Fatalf("the push carried a token with %v", perms)
	}
}

// Allowing ordinary pushes to a branch for the session does not allow
// workflow changes there.
func TestAPushGrantDoesNotCoverWorkflowChanges(t *testing.T) {
	f := newWorkflowsFixture(t)
	f.commit(t, "README", "changed\n")
	if _, err := f.push(t, "HEAD:refs/heads/legato/task-1-x"); err == nil {
		t.Fatal("the push was not held")
	}
	f.allow(t, f.pending(t), "session")
	if out, err := f.push(t, "HEAD:refs/heads/legato/task-1-x"); err != nil {
		t.Fatalf("push failed: %v\n%s", err, out)
	}

	f.commit(t, ".github/workflows/x.yml", "on: push\n")
	out, err := f.push(t, "HEAD:refs/heads/legato/task-1-x")
	if err == nil || !strings.Contains(out, "pending ap-") {
		t.Fatalf("a workflow change went on a push grant: %v\n%s", err, out)
	}
	if d := approvalDetails(t, f.pending(t)); d.Operations[0] != "git.push-workflows" {
		t.Fatalf("details = %+v", d)
	}
}

func TestAWorkflowsPushToAnotherBranchIsAskedEachTimeAndSigned(t *testing.T) {
	f := newWorkflowsFixture(t)
	f.commit(t, ".github/workflows/x.yml", "on: push\n")

	if out, err := f.push(t, "HEAD:refs/heads/feature/ci"); err == nil || !strings.Contains(out, "pending ap-") {
		t.Fatalf("err %v\n%s", err, out)
	}
	approval := f.pending(t)
	if d := approvalDetails(t, approval); d.Operations[0] != "git.push-workflows" || !d.Signed {
		t.Fatalf("details = %+v", d)
	}
	for _, o := range approval.Options {
		if o.ID == "session" || o.ID == "window" {
			t.Fatalf("options = %+v", approval.Options)
		}
	}
}

func TestAWorkflowsPushToTheDefaultBranchIsRefusedWithoutAsking(t *testing.T) {
	f := newWorkflowsFixture(t)
	f.commit(t, ".github/workflows/x.yml", "on: push\n")

	if out, err := f.push(t, "HEAD:refs/heads/main"); err == nil || !strings.Contains(out, "gh does not allow git.push-default") {
		t.Fatalf("err %v\n%s", err, out)
	}
	if list, _ := f.approvals.List(context.Background(), store.ApprovalFilter{Status: store.ApprovalPending}); len(list) != 0 {
		t.Fatalf("the operator was asked: %+v", list)
	}
}

func TestAnAppWithoutTheWorkflowsPermissionIsRefusedBeforeAnyoneIsAsked(t *testing.T) {
	f := newWorkflowsFixture(t)
	f.github.mu.Lock()
	delete(f.github.appPermissions, "workflows")
	f.github.mu.Unlock()
	f.commit(t, ".github/workflows/x.yml", "on: push\n")

	out, err := f.push(t, "HEAD:refs/heads/legato/task-1-ci")
	if err == nil {
		t.Fatalf("the push went:\n%s", out)
	}
	for _, want := range []string{"Workflows", "Read and write", ".github/workflows/x.yml"} {
		if !strings.Contains(out, want) {
			t.Errorf("git's output does not say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "request_access") || strings.Contains(out, "pending") {
		t.Errorf("the agent was told to ask:\n%s", out)
	}
	if list, _ := f.approvals.List(context.Background(), store.ApprovalFilter{Status: store.ApprovalPending}); len(list) != 0 {
		t.Fatalf("the operator was asked: %+v", list)
	}

	// Once the App has the permission, the next push is asked for.
	f.github.mu.Lock()
	f.github.appPermissions["workflows"] = "write"
	f.github.mu.Unlock()
	if out, err := f.push(t, "HEAD:refs/heads/legato/task-1-ci"); err == nil || !strings.Contains(out, "pending ap-") {
		t.Fatalf("err %v\n%s", err, out)
	}
}
