package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/gitrepo"
	"github.com/cpave3/legato/internal/engine/store"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repoUpstream is a folder of bare repositories standing in for GitHub,
// each with a commit on main and a branch feature/x.
type repoUpstream struct{ root string }

func newRepoUpstream(t *testing.T, repos ...string) repoUpstream {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	g := repoUpstream{root: t.TempDir()}
	for _, repo := range repos {
		work := t.TempDir()
		gitIn(t, work, "init", "-q", "-b", "main")
		if err := os.WriteFile(filepath.Join(work, "README"), []byte(repo+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, work, "add", "README")
		gitIn(t, work, "commit", "-q", "-m", "first")
		gitIn(t, work, "branch", "feature/x")
		gitIn(t, work, "clone", "-q", "--bare", work, filepath.Join(g.root, repo+".git"))
	}
	return g
}

func (g repoUpstream) url(repo string) string { return "file://" + filepath.Join(g.root, repo+".git") }

// push adds a commit to a branch of the fake and returns it.
func (g repoUpstream) push(t *testing.T, repo, branch string) string {
	t.Helper()
	work := t.TempDir()
	gitIn(t, work, "clone", "-q", "-b", branch, g.url(repo), ".")
	gitIn(t, work, "commit", "-q", "--allow-empty", "-m", "more")
	gitIn(t, work, "push", "-q", "origin", branch)
	return gitIn(t, work, "rev-parse", "HEAD")
}

// fakeTokens gives every repository of the fake a token, or refuses with
// refuse.
type fakeTokens struct {
	gh     repoUpstream
	refuse error
	proxy  bool
}

func (f fakeTokens) GitReadToken(_ context.Context, repo string) ([]byte, string, error) {
	if f.refuse != nil {
		return nil, "", f.refuse
	}
	return []byte("ghs_test"), f.gh.url(repo), nil
}

func (f fakeTokens) PushesThroughProxy(string) bool { return f.proxy }

func (f fakeTokens) CanRead(string) error { return f.refuse }

// countingGit is the real git runner, counting clones and fetches.
type countingGit struct {
	gitrepo.Runner
	clones, fetches atomic.Int32
	slow            time.Duration
}

func (c *countingGit) CloneBare(ctx context.Context, url, dest string, token []byte, progress func(gitrepo.Progress)) error {
	c.clones.Add(1)
	time.Sleep(c.slow)
	return c.Runner.CloneBare(ctx, url, dest, token, progress)
}

func (c *countingGit) Fetch(ctx context.Context, dir string, token []byte, progress func(gitrepo.Progress)) error {
	c.fetches.Add(1)
	return c.Runner.Fetch(ctx, dir, token, progress)
}

type repoFixture struct {
	svc   *RepositoryService
	git   *countingGit
	gh    repoUpstream
	store *store.Store
	data  string
	now   *time.Time
}

func newRepoFixture(t *testing.T, tokens RepoTokenSource, gh repoUpstream) *repoFixture {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	// file:// upstreams never ask for a password: the helper never runs.
	f := &repoFixture{git: &countingGit{Runner: gitrepo.Runner{AskPass: "/bin/false"}}, gh: gh, store: s, data: t.TempDir(), now: &now}
	f.svc = NewRepositoryService(RepositoryServiceOptions{
		DataDir: f.data, MaxAge: 5 * time.Minute, Tokens: tokens, Git: f.git, Store: s,
		Now: func() time.Time { return *f.now },
	})
	return f
}

func TestEnsureClonesIntoTheCache(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh}, gh)

	path, err := f.svc.Ensure(context.Background(), "acme/widget")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if want := filepath.Join(f.data, "repos", "acme", "widget.git"); path != want {
		t.Fatalf("path = %s, want %s", path, want)
	}
	if gitIn(t, "", "--git-dir", path, "rev-parse", "--verify", "refs/heads/feature/x") == "" {
		t.Fatal("the cache has no feature/x")
	}
}

func TestEnsureFetchesOnlyWhenOlderThanMaxAge(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh}, gh)
	ctx := context.Background()
	if _, err := f.svc.Ensure(ctx, "acme/widget"); err != nil {
		t.Fatal(err)
	}
	newer := gh.push(t, "acme/widget", "main")

	*f.now = f.now.Add(4 * time.Minute)
	path, err := f.svc.Ensure(ctx, "acme/widget")
	if err != nil {
		t.Fatal(err)
	}
	if f.git.fetches.Load() != 0 {
		t.Fatal("Ensure fetched a cache fetched 4 minutes ago")
	}

	*f.now = f.now.Add(2 * time.Minute)
	if _, err := f.svc.Ensure(ctx, "acme/widget"); err != nil {
		t.Fatal(err)
	}
	if f.git.fetches.Load() != 1 || gitIn(t, "", "--git-dir", path, "rev-parse", "refs/heads/main") != newer {
		t.Fatalf("after 6 minutes: %d fetches, want 1 bringing %s", f.git.fetches.Load(), newer)
	}
}

func TestEnsureConcurrentCallersShareOneClone(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh}, gh)
	f.git.slow = 100 * time.Millisecond
	var wg sync.WaitGroup
	paths := make([]string, 2)
	errs := make([]error, 2)
	for i := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			paths[i], errs[i] = f.svc.Ensure(context.Background(), "acme/widget")
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || paths[0] != paths[1] {
		t.Fatalf("paths %v, errors %v", paths, errs)
	}
	if n := f.git.clones.Load(); n != 1 {
		t.Fatalf("%d clones ran, want 1", n)
	}
}

func TestEnsureMissingRepositoryLeavesNothing(t *testing.T) {
	gh := newRepoUpstream(t)
	f := newRepoFixture(t, fakeTokens{gh: gh}, gh)
	_, err := f.svc.Ensure(context.Background(), "acme/missing")
	if err == nil || !strings.Contains(err.Error(), "acme/missing") {
		t.Fatalf("error = %v, want one naming acme/missing", err)
	}
	entries, _ := os.ReadDir(filepath.Join(f.data, "repos", "acme"))
	if len(entries) != 0 {
		t.Fatalf("the failed clone left %v", entries)
	}
}

func TestEnsureWithoutACredential(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	refusal := errors.New("acme/widget is not one of the repositories of github (settings.repositories in config.yaml)")
	f := newRepoFixture(t, fakeTokens{gh: gh, refuse: refusal}, gh)
	if _, err := f.svc.Ensure(context.Background(), "acme/widget"); err == nil || !strings.Contains(err.Error(), "settings.repositories") {
		t.Fatalf("error = %v", err)
	}
	none := newRepoFixture(t, nil, gh)
	if _, err := none.svc.Ensure(context.Background(), "acme/widget"); err == nil || !strings.Contains(err.Error(), "credentials in config.yaml") {
		t.Fatalf("with no credential at all: error = %v", err)
	}
}

func TestEnsureRefusesANameThatIsNotARepository(t *testing.T) {
	gh := newRepoUpstream(t)
	f := newRepoFixture(t, fakeTokens{gh: gh}, gh)
	for _, bad := range []string{"../etc", "acme/..", "acme", "acme/widget/more", ""} {
		if _, err := f.svc.Ensure(context.Background(), bad); err == nil {
			t.Errorf("Ensure(%q) succeeded", bad)
		}
	}
}

func TestJobCloneChecksOutTheRefBesideTheCache(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh, proxy: true}, gh)
	ctx := context.Background()
	if err := f.store.CreateTask(ctx, store.Task{ID: "task1", Title: "Job", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}

	clone, err := f.svc.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-abc", TaskID: "task1", Repo: "acme/widget", Ref: "feature/x"})
	if err != nil {
		t.Fatalf("JobClone: %v", err)
	}
	if want := filepath.Join(f.data, "jobs", "ex-abc", "widget"); clone.Path != want {
		t.Fatalf("path = %s, want %s", clone.Path, want)
	}
	if clone.Branch != "feature/x" || gitIn(t, clone.Path, "symbolic-ref", "--short", "HEAD") != "feature/x" {
		t.Fatalf("branch = %q", clone.Branch)
	}
	if got := gitIn(t, clone.Path, "remote", "get-url", "origin"); got != gh.url("acme/widget") {
		t.Fatalf("origin = %s, want the repository's address, not the cache", got)
	}
	if out, _ := exec.Command("git", "-C", clone.Path, "config", "--get", "remote.origin.pushurl").Output(); len(out) != 0 {
		t.Fatalf("pushes go to %s; with Legato's proxy they must follow the session's settings", out)
	}

	recorded, err := f.store.JobCloneByPath(ctx, clone.Path)
	if err != nil || recorded.ExecutionID != "ex-abc" || recorded.TaskID != "task1" || recorded.Repo != "acme/widget" {
		t.Fatalf("recorded %+v, %v", recorded, err)
	}
	task, err := f.store.GetTask(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	cc, _ := store.ParseCodeContext(task.CodeContext)
	if cc == nil || cc.Repo != "acme/widget" || cc.Branch != "feature/x" || cc.WorkingDir != clone.Path {
		t.Fatalf("code context = %+v", cc)
	}
}

func TestJobCloneWithoutTheProxyCannotPush(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh, proxy: false}, gh)
	clone, err := f.svc.JobClone(context.Background(), JobCloneRequest{ExecutionID: "ex-abc", TaskID: "task1", Repo: "acme/widget"})
	if err != nil {
		t.Fatalf("JobClone: %v", err)
	}
	if clone.Branch != "main" {
		t.Fatalf("branch = %q, want the default branch", clone.Branch)
	}
	if got := gitIn(t, clone.Path, "config", "--get", "remote.origin.pushurl"); got != JobClonePushPlaceholder {
		t.Fatalf("push URL = %q, want the placeholder", got)
	}
}

func TestJobCloneRefusesAnExecutionIDThatIsAPath(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh}, gh)
	if _, err := f.svc.JobClone(context.Background(), JobCloneRequest{ExecutionID: "../x", TaskID: "task1", Repo: "acme/widget"}); err == nil {
		t.Fatal("JobClone took ../x as an execution")
	}
}

// jobFixture is a job clone of acme/widget for task1, its execution
// recorded as running.
func jobFixture(t *testing.T) (*repoFixture, store.JobClone) {
	t.Helper()
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh, proxy: true}, gh)
	ctx := context.Background()
	if err := f.store.CreateTask(ctx, store.Task{ID: "task1", Title: "Job", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	clone, err := f.svc.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-abc", TaskID: "task1", Repo: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.CreateExecution(ctx, store.Execution{ID: "ex-abc", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning,
		TaskID: "task1", ExecutionTaskID: "task1", WorkingDir: clone.Path, StartedAt: *f.now}); err != nil {
		t.Fatal(err)
	}
	return f, clone
}

func (f *repoFixture) end(t *testing.T, executionID string) {
	t.Helper()
	if err := f.store.TerminalizeExecution(context.Background(), executionID, store.ExecutionSucceeded); err != nil {
		t.Fatal(err)
	}
}

// idle moves time past the grace a clone just used is given.
func (f *repoFixture) idle() { *f.now = f.now.Add(jobCloneGrace) }

func (f *repoFixture) sweep(t *testing.T) {
	t.Helper()
	if err := f.svc.SweepJobClones(context.Background()); err != nil {
		t.Fatalf("SweepJobClones: %v", err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestSweepKeepsTheCloneOfARunningAgent(t *testing.T) {
	f, clone := jobFixture(t)
	*f.now = f.now.Add(time.Hour)
	f.sweep(t)
	if !exists(clone.Path) {
		t.Fatal("the clone of a running agent was removed")
	}
}

func TestSweepRemovesACloneWithNothingUnpushed(t *testing.T) {
	f, clone := jobFixture(t)
	f.end(t, "ex-abc")
	f.idle()
	f.sweep(t)
	if exists(clone.Path) || exists(filepath.Dir(clone.Path)) {
		t.Fatal("the clone of an ended agent with nothing unpushed is still there")
	}
	if live, _ := f.store.ListLiveJobClones(context.Background()); len(live) != 0 {
		t.Fatalf("the removed clone is still recorded as live: %+v", live)
	}
}

func TestSweepKeepsUnpushedWorkForSevenDaysWithANote(t *testing.T) {
	f, clone := jobFixture(t)
	gitIn(t, clone.Path, "commit", "-q", "--allow-empty", "-m", "unpushed")
	f.end(t, "ex-abc")
	f.idle()
	ctx := context.Background()

	f.sweep(t)
	if !exists(clone.Path) {
		t.Fatal("a clone with unpushed commits was removed")
	}
	notes, err := f.store.ListTaskNotes(ctx, "task1")
	if err != nil || len(notes) != 1 || !strings.Contains(notes[0].Body, clone.Path) || !strings.Contains(notes[0].Body, "2026-11-09") {
		t.Fatalf("notes = %+v, %v; want one naming the path and the day it goes", notes, err)
	}

	*f.now = f.now.Add(6 * 24 * time.Hour)
	f.sweep(t)
	if !exists(clone.Path) {
		t.Fatal("removed before its seven days")
	}
	if notes, _ := f.store.ListTaskNotes(ctx, "task1"); len(notes) != 1 {
		t.Fatalf("a second sweep added a note: %d notes", len(notes))
	}

	*f.now = f.now.Add(25 * time.Hour)
	f.sweep(t)
	if exists(clone.Path) {
		t.Fatal("still there after seven days")
	}
}

func TestSweepRemovesACloneWhoseWorkWasPushed(t *testing.T) {
	f, clone := jobFixture(t)
	gitIn(t, clone.Path, "switch", "-q", "-c", "legato/task1-fix")
	gitIn(t, clone.Path, "commit", "-q", "--allow-empty", "-m", "pushed")
	// The push reaches GitHub through Legato's proxy; here, straight to the fake.
	gitIn(t, clone.Path, "push", "-q", f.gh.url("acme/widget"), "legato/task1-fix")
	f.end(t, "ex-abc")
	f.idle()
	f.sweep(t)
	if exists(clone.Path) {
		t.Fatal("a clone whose commits were pushed was kept")
	}
}

func TestSweepLeavesACloneWhoseExecutionIsNotRecordedYet(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh, proxy: true}, gh)
	clone, err := f.svc.JobClone(context.Background(), JobCloneRequest{ExecutionID: "ex-new", TaskID: "task1", Repo: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	f.sweep(t)
	if !exists(clone.Path) {
		t.Fatal("a clone made a moment ago, before its execution, was removed")
	}
	*f.now = f.now.Add(time.Hour)
	f.sweep(t)
	if exists(clone.Path) {
		t.Fatal("an hour on, a clone whose spawn never recorded an execution is still there")
	}
}

func TestTheCacheTakesItsCredentialWhenTheChannelsAreBuilt(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, nil, gh)
	if _, err := f.svc.Ensure(context.Background(), "acme/widget"); err == nil {
		t.Fatal("cloned with no credential")
	}
	f.svc.SetTokens(fakeTokens{gh: gh})
	if _, err := f.svc.Ensure(context.Background(), "acme/widget"); err != nil {
		t.Fatalf("with the credential: %v", err)
	}
}

func TestJobCloneOnANewBranchRecordsItAndItsBase(t *testing.T) {
	gh := newRepoUpstream(t, "acme/widget")
	f := newRepoFixture(t, fakeTokens{gh: gh, proxy: true}, gh)
	ctx := context.Background()
	if err := f.store.CreateTask(ctx, store.Task{ID: "task1", Title: "Job", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	clone, err := f.svc.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-abc", TaskID: "task1", Repo: "acme/widget", Branch: "legato/task1"})
	if err != nil {
		t.Fatalf("JobClone: %v", err)
	}
	if clone.Branch != "legato/task1" || gitIn(t, clone.Path, "symbolic-ref", "--short", "HEAD") != "legato/task1" {
		t.Fatalf("branch = %q", clone.Branch)
	}
	task, _ := f.store.GetTask(ctx, "task1")
	cc, _ := store.ParseCodeContext(task.CodeContext)
	if cc == nil || cc.Branch != "legato/task1" || cc.BaseBranch != "main" {
		t.Fatalf("code context = %+v, want the new branch on main", cc)
	}
}

func TestTheCacheSaysWhyARepositoryCannotBeCloned(t *testing.T) {
	gh := newRepoUpstream(t)
	none := newRepoFixture(t, nil, gh)
	if problem := none.svc.CloneProblem("acme/widget"); !strings.Contains(problem, "github credential") {
		t.Fatalf("with no credential: %q", problem)
	}
	f := newRepoFixture(t, fakeTokens{gh: gh, refuse: errors.New("acme/widget is not one of the repositories of github (settings.repositories in config.yaml)")}, gh)
	if problem := f.svc.CloneProblem("acme/widget"); !strings.Contains(problem, "settings.repositories") {
		t.Fatalf("an unlisted repository: %q", problem)
	}
	ok := newRepoFixture(t, fakeTokens{gh: gh}, gh)
	if problem := ok.svc.CloneProblem("acme/widget"); problem != "" {
		t.Fatalf("a listed repository: %q", problem)
	}
}
