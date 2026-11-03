package clonesetup_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/clonesetup"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// cloneOf is a clone of a repository whose default branch is trunk, on
// branch: one GitHub has (tracked), or a new one.
func cloneOf(t *testing.T, trunk, branch string, onOrigin bool) string {
	t.Helper()
	origin := t.TempDir()
	git(t, origin, "init", "-q", "-b", trunk)
	git(t, origin, "commit", "-q", "--allow-empty", "-m", "first")
	if onOrigin {
		git(t, origin, "branch", branch)
	}
	clone := filepath.Join(t.TempDir(), "repo")
	git(t, filepath.Dir(clone), "clone", "-q", origin, clone)
	if onOrigin {
		git(t, clone, "checkout", "-q", branch)
	} else {
		git(t, clone, "checkout", "-q", "-b", branch)
	}
	return clone
}

func TestRunGivesTheCommandsYggdrasilsVariables(t *testing.T) {
	clone := cloneOf(t, "main", "legato/abc", false)
	var out bytes.Buffer

	err := clonesetup.Run(context.Background(), []string{
		`echo "wt=$YG_WORKTREE branch=$YG_BRANCH new=$YG_BRANCH_NEW trunk=$YG_TRUNK base=$YG_BASE primary=$YG_PRIMARY"`,
		`echo "event=$YG_EVENT agent=$YG_AGENT_OWNED legato=$LEGATO_CLONE pwd=$(pwd)"`,
	}, clonesetup.RunOptions{Dir: clone, Base: "release/2", Primary: "/home/me/app", Out: &out})
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}

	for _, want := range []string{
		"wt=" + clone + " branch=legato/abc new=1 trunk=main base=release/2 primary=/home/me/app",
		"event=post_create agent=1 legato=1 pwd=" + clone,
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

func TestABranchGitHubHasIsNotNewAndTheBaseDefaultsToTheTrunk(t *testing.T) {
	clone := cloneOf(t, "trunk", "feature", true)
	var out bytes.Buffer

	if err := clonesetup.Run(context.Background(), []string{`echo "new=$YG_BRANCH_NEW base=$YG_BASE trunk=$YG_TRUNK"`},
		clonesetup.RunOptions{Dir: clone, Out: &out}); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out.String(), "new=0 base=trunk trunk=trunk") {
		t.Errorf("out = %s", out.String())
	}
}

func TestRunStopsAtTheFirstFailure(t *testing.T) {
	clone := cloneOf(t, "main", "b", false)
	var out bytes.Buffer

	err := clonesetup.Run(context.Background(), []string{"echo one", "exit 3", "echo three"}, clonesetup.RunOptions{Dir: clone, Out: &out})

	if err == nil || !strings.Contains(err.Error(), "command 2") || !strings.Contains(err.Error(), "exit 3") {
		t.Errorf("err = %v, want one naming command 2", err)
	}
	if strings.Contains(out.String(), "three") {
		t.Errorf("ran on after a failure:\n%s", out.String())
	}
}
