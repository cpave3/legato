package service

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// stackGit runs git in an agent's working copy whose pushes go through the
// fixture's proxy, as a session's git would with Legato's helper answering.
func (f githubFixture) stackGit(t *testing.T, work string, args ...string) (string, error) {
	t.Helper()
	helper := "!f() { echo username=x-access-token; echo password=" + f.env["LEGATO_GITHUB_TOKEN"] + "; }; f"
	full := append([]string{"-c", "credential.helper=", "-c", "credential.helper=" + helper, "-c", "user.name=a", "-c", "user.email=a@b"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = work
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestOneGrantOnTheTasksBranchPatternCoversAWholeStack(t *testing.T) {
	f := newGitHubFixture(t)
	ctx := context.Background()
	res, err := f.broker.Request(ctx, AccessRequest{TaskID: "task-1", ExecutionID: "ex-1", Credential: "gh",
		Operations: []string{"git.push"}, Resources: []string{"github:cpave3/legato@refs/heads/legato/task-1-*"}})
	if err != nil || res.Status != "pending" {
		t.Fatalf("request = %+v, %v", res, err)
	}
	approval := f.pending(t)
	if approval.Options[0].ID != "session" {
		t.Fatalf("the pattern was not decided by the legato/** rule: %+v", approval.Options)
	}
	if _, err := f.approvals.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "session", By: "web"}); err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	remote := f.proxy.URL + "/c/gh/git/cpave3/legato.git"
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"commit", "-q", "--allow-empty", "-m", "base"},
		{"checkout", "-q", "-b", "legato/task-1-a"},
		{"commit", "-q", "--allow-empty", "-m", "a"},
		{"checkout", "-q", "-b", "legato/task-1-b"},
		{"commit", "-q", "--allow-empty", "-m", "b"},
		{"remote", "add", "origin", remote},
	} {
		if out, err := f.stackGit(t, work, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if out, err := f.stackGit(t, work, "push", "origin", "legato/task-1-a", "legato/task-1-b"); err != nil {
		t.Fatalf("the stack was not pushed under the grant: %v\n%s", err, out)
	}

	// A restack rewrites both branches; st pushes them with --force-with-lease.
	for _, args := range [][]string{
		{"checkout", "-q", "legato/task-1-a"},
		{"commit", "-q", "--amend", "--allow-empty", "-m", "a, reworked"},
		{"checkout", "-q", "legato/task-1-b"},
		{"rebase", "-q", "--onto", "legato/task-1-a", "origin/legato/task-1-a"},
	} {
		if out, err := f.stackGit(t, work, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if out, err := f.stackGit(t, work, "push", "--force-with-lease", "origin", "legato/task-1-a", "legato/task-1-b"); err != nil {
		t.Fatalf("the restacked stack was not force-pushed under the grant: %v\n%s", err, out)
	}
	if list, _ := f.approvals.List(ctx, store.ApprovalFilter{Status: store.ApprovalPending}); len(list) != 0 {
		t.Fatalf("the person was asked again: %+v", list)
	}
	want, _ := f.stackGit(t, work, "rev-parse", "legato/task-1-b")
	if got := ghGit(t, f.github.bare, "rev-parse", "legato/task-1-b"); strings.TrimSpace(got) != strings.TrimSpace(want) {
		t.Fatalf("GitHub has legato/task-1-b at %q, want %q", got, want)
	}

	// The grant stops at the task's own branches, one path segment deep.
	for _, ref := range []string{"legato/task-2-a", "legato/task-1-a/deeper"} {
		out, err := f.stackGit(t, work, "push", "origin", "HEAD:refs/heads/"+ref)
		if err == nil || !strings.Contains(out, "pending ap-") {
			t.Fatalf("a push of %s was not held for the person: %v\n%s", ref, err, out)
		}
	}
}
