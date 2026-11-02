package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The test binary is also the askpass helper git runs, as legato is.
func TestMain(m *testing.M) {
	if IsAskPass(os.Getenv) {
		os.Exit(AskPass(os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

const testToken = "ghs_testtoken0123456789"

func requireGit(t *testing.T) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	return git
}

func run(t *testing.T, dir string, args ...string) string {
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

// upstream makes a bare repository with one commit on main and a branch
// feature/x, under root/<owner>/<name>.git.
func upstream(t *testing.T, root, repo string) string {
	t.Helper()
	work := t.TempDir()
	run(t, work, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "README"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, work, "add", "README")
	run(t, work, "commit", "-q", "-m", "first")
	run(t, work, "branch", "feature/x")
	bare := filepath.Join(root, repo+".git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, work, "clone", "-q", "--bare", work, bare)
	return bare
}

// server serves root over git's smart HTTP protocol, and only to a client
// that gives token as its password.
func server(t *testing.T, root, token string) *httptest.Server {
	t.Helper()
	execPath := run(t, "", "--exec-path")
	backend := &cgi.Handler{Path: filepath.Join(execPath, "git-http-backend"), Root: "/",
		Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "x-access-token" || pass != token {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// recordingGit is a git that writes its arguments and environment to log
// before it runs the real one.
func recordingGit(t *testing.T, realGit string) (path, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "calls.log")
	path = filepath.Join(dir, "git")
	script := "#!/bin/sh\n{ echo \"ARGS: $*\"; env; } >> " + log + "\nexec " + realGit + " \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, log
}

func self(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestCloneBareWithToken(t *testing.T) {
	realGit := requireGit(t)
	root := t.TempDir()
	upstream(t, root, "acme/widget")
	srv := server(t, root, testToken)
	git, log := recordingGit(t, realGit)

	r := Runner{Git: git, AskPass: self(t)}
	dest := filepath.Join(t.TempDir(), "widget.git")
	if err := r.CloneBare(context.Background(), srv.URL+"/acme/widget.git", dest, []byte(testToken), nil); err != nil {
		t.Fatalf("CloneBare: %v", err)
	}

	if got := run(t, "", "--git-dir", dest, "rev-parse", "--verify", "refs/heads/feature/x"); got == "" {
		t.Fatal("the clone has no feature/x")
	}
	if got := run(t, "", "--git-dir", dest, "config", "remote.origin.url"); got != srv.URL+"/acme/widget.git" {
		t.Fatalf("origin = %q, want the server address with no credential", got)
	}

	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(calls, []byte(testToken)) {
		t.Fatalf("the token reached git's arguments or environment:\n%s", calls)
	}
	assertNoFileHolds(t, dest, testToken)
}

func assertNoFileHolds(t *testing.T, root, secret string) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("%s holds the token", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFetchUpdatesBranches(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	bare := upstream(t, root, "acme/widget")
	srv := server(t, root, testToken)
	r := Runner{AskPass: self(t)}
	dest := filepath.Join(t.TempDir(), "widget.git")
	if err := r.CloneBare(context.Background(), srv.URL+"/acme/widget.git", dest, []byte(testToken), nil); err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	run(t, work, "clone", "-q", bare, ".")
	run(t, work, "commit", "-q", "--allow-empty", "-m", "second")
	run(t, work, "push", "-q", "origin", "main")
	want := run(t, work, "rev-parse", "HEAD")

	if err := r.Fetch(context.Background(), dest, []byte(testToken), nil); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := run(t, "", "--git-dir", dest, "rev-parse", "refs/heads/main"); got != want {
		t.Fatalf("main = %s after the fetch, want %s", got, want)
	}

	if err := r.Fetch(context.Background(), dest, []byte("wrong"), nil); err == nil {
		t.Fatal("Fetch with a wrong token succeeded")
	}
}

// cache makes a bare clone of a fresh upstream, as Legato's cache holds one.
func cache(t *testing.T) string {
	t.Helper()
	requireGit(t)
	root := t.TempDir()
	upstream(t, root, "acme/widget")
	srv := server(t, root, testToken)
	dest := filepath.Join(t.TempDir(), "widget.git")
	if err := (Runner{AskPass: self(t)}).CloneBare(context.Background(), srv.URL+"/acme/widget.git", dest, []byte(testToken), nil); err != nil {
		t.Fatal(err)
	}
	return dest
}

func TestCloneJobAtRef(t *testing.T) {
	c := cache(t)
	dest := filepath.Join(t.TempDir(), "widget")
	branch, err := Runner{}.CloneJob(context.Background(), JobClone{Cache: c, Origin: "https://github.com/acme/widget.git", Dest: dest, Ref: "feature/x"})
	if err != nil {
		t.Fatalf("CloneJob: %v", err)
	}
	if branch != "feature/x" {
		t.Fatalf("branch = %q, want feature/x", branch)
	}
	if got := run(t, dest, "symbolic-ref", "--short", "HEAD"); got != "feature/x" {
		t.Fatalf("HEAD = %q, want feature/x", got)
	}
	if _, err := os.Stat(filepath.Join(dest, "README")); err != nil {
		t.Fatalf("the clone is not checked out: %v", err)
	}
	if got := run(t, dest, "remote", "get-url", "origin"); got != "https://github.com/acme/widget.git" {
		t.Fatalf("origin = %q", got)
	}
	alternates, err := os.ReadFile(filepath.Join(dest, ".git/objects/info/alternates"))
	if err != nil || !strings.Contains(string(alternates), filepath.Join(c, "objects")) {
		t.Fatalf("the clone does not borrow the cache's objects: %q, %v", alternates, err)
	}
	if got := run(t, dest, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/feature/x" {
		t.Fatalf("upstream = %q, want origin/feature/x", got)
	}
}

func TestCloneJobDefaultBranch(t *testing.T) {
	c := cache(t)
	dest := filepath.Join(t.TempDir(), "widget")
	branch, err := Runner{}.CloneJob(context.Background(), JobClone{Cache: c, Origin: "https://github.com/acme/widget.git", Dest: dest})
	if err != nil {
		t.Fatalf("CloneJob: %v", err)
	}
	if branch != "main" {
		t.Fatalf("branch = %q, want main", branch)
	}
	if _, err := os.Stat(filepath.Join(dest, "README")); err != nil {
		t.Fatalf("the clone is not checked out: %v", err)
	}
}

func TestCloneJobPushPlaceholder(t *testing.T) {
	c := cache(t)
	dest := filepath.Join(t.TempDir(), "widget")
	const placeholder = "/nonexistent/legato-cannot-push"
	if _, err := (Runner{}).CloneJob(context.Background(), JobClone{Cache: c, Origin: "https://github.com/acme/widget.git", Dest: dest, PushURL: placeholder}); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "push", "origin", "main")
	cmd.Dir = dest
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("the push succeeded")
	}
	if !strings.Contains(string(out), placeholder) {
		t.Fatalf("the push did not go to the placeholder:\n%s", out)
	}
}

func TestCloneJobCommitIsDetached(t *testing.T) {
	c := cache(t)
	sha := run(t, "", "--git-dir", c, "rev-parse", "refs/heads/main")
	dest := filepath.Join(t.TempDir(), "widget")
	branch, err := Runner{}.CloneJob(context.Background(), JobClone{Cache: c, Origin: "https://github.com/acme/widget.git", Dest: dest, Ref: sha})
	if err != nil {
		t.Fatalf("CloneJob: %v", err)
	}
	if branch != "" || run(t, dest, "rev-parse", "HEAD") != sha {
		t.Fatalf("branch %q, want a detached HEAD at %s", branch, sha)
	}
}

func TestCloneJobUnknownRefLeavesNothing(t *testing.T) {
	c := cache(t)
	dest := filepath.Join(t.TempDir(), "widget")
	if _, err := (Runner{}).CloneJob(context.Background(), JobClone{Cache: c, Origin: "https://github.com/acme/widget.git", Dest: dest, Ref: "no-such-branch"}); err == nil {
		t.Fatal("CloneJob of an unknown ref succeeded")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("the failed clone is still at %s", dest)
	}
}

func TestUnpushedTips(t *testing.T) {
	c := cache(t)
	dest := filepath.Join(t.TempDir(), "widget")
	if _, err := (Runner{}).CloneJob(context.Background(), JobClone{Cache: c, Origin: "https://github.com/acme/widget.git", Dest: dest}); err != nil {
		t.Fatal(err)
	}
	r := Runner{}
	if tips, err := r.UnpushedTips(context.Background(), dest, c); err != nil || len(tips) != 0 {
		t.Fatalf("a fresh clone: tips %v, err %v; want none", tips, err)
	}

	run(t, dest, "switch", "-q", "-c", "work")
	run(t, dest, "commit", "-q", "--allow-empty", "-m", "local")
	local := run(t, dest, "rev-parse", "HEAD")
	run(t, dest, "pack-refs", "--all")
	run(t, dest, "switch", "-q", "--detach", "main")
	run(t, dest, "commit", "-q", "--allow-empty", "-m", "detached")
	detached := run(t, dest, "rev-parse", "HEAD")

	tips, err := r.UnpushedTips(context.Background(), dest, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(tips) != 2 || !contains(tips, local) || !contains(tips, detached) {
		t.Fatalf("tips = %v, want %s (packed branch) and %s (detached HEAD)", tips, local, detached)
	}
}

func TestUnpushedTipsRunsNothingFromTheClone(t *testing.T) {
	c := cache(t)
	dest := filepath.Join(t.TempDir(), "widget")
	if _, err := (Runner{}).CloneJob(context.Background(), JobClone{Cache: c, Origin: "https://github.com/acme/widget.git", Dest: dest}); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "ran")
	for _, kv := range [][2]string{{"core.fsmonitor", "touch " + marker}, {"core.hooksPath", filepath.Dir(marker)}, {"core.sshCommand", "touch " + marker}} {
		run(t, dest, "config", kv[0], kv[1])
	}
	if _, err := (Runner{}).UnpushedTips(context.Background(), dest, c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the clone's configuration ran a command")
	}
}

func TestUnpushedTipsRefusesALinkedGitDir(t *testing.T) {
	c := cache(t)
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Runner{}).UnpushedTips(context.Background(), dest, c); err == nil {
		t.Fatal("a .git file was taken for a clone")
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestCloneJobMakesANewBranchFromTheBase(t *testing.T) {
	c := cache(t)
	dest := filepath.Join(t.TempDir(), "widget")
	branch, err := Runner{}.CloneJob(context.Background(), JobClone{Cache: c, Origin: "https://github.com/acme/widget.git", Dest: dest, Ref: "feature/x", NewBranch: "legato/t1"})
	if err != nil {
		t.Fatalf("CloneJob: %v", err)
	}
	if branch != "legato/t1" || run(t, dest, "symbolic-ref", "--short", "HEAD") != "legato/t1" {
		t.Fatalf("branch = %q", branch)
	}
	if run(t, dest, "rev-parse", "HEAD") != run(t, "", "--git-dir", c, "rev-parse", "refs/heads/feature/x") {
		t.Fatal("the new branch does not start at its base")
	}
	// A new branch tracks nothing: a bare git push must not go to the base.
	cmd := exec.Command("git", "-C", dest, "config", "--get", "branch.legato/t1.merge")
	if out, _ := cmd.Output(); len(out) != 0 {
		t.Fatalf("the new branch tracks %s", out)
	}
}

func TestCloneJobFollowsABranchThatExists(t *testing.T) {
	c := cache(t)
	dest := filepath.Join(t.TempDir(), "widget")
	branch, err := Runner{}.CloneJob(context.Background(), JobClone{Cache: c, Origin: "https://github.com/acme/widget.git", Dest: dest, Ref: "main", NewBranch: "feature/x"})
	if err != nil {
		t.Fatalf("CloneJob: %v", err)
	}
	if branch != "feature/x" || run(t, dest, "rev-parse", "--abbrev-ref", "@{upstream}") != "origin/feature/x" {
		t.Fatalf("branch = %q, want feature/x tracking origin's", branch)
	}
}

func TestCloneJobRefusesABadBranchName(t *testing.T) {
	c := cache(t)
	dest := filepath.Join(t.TempDir(), "widget")
	if _, err := (Runner{}).CloneJob(context.Background(), JobClone{Cache: c, Origin: "https://github.com/acme/widget.git", Dest: dest, NewBranch: "bad..name"}); err == nil {
		t.Fatal("a branch named bad..name was made")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("the failed clone is still there")
	}
}

func jobClone(t *testing.T) string {
	t.Helper()
	dest := filepath.Join(t.TempDir(), "widget")
	if _, err := (Runner{}).CloneJob(context.Background(), JobClone{Cache: cache(t), Origin: "https://github.com/acme/widget.git", Dest: dest, NewBranch: "stack/one"}); err != nil {
		t.Fatal(err)
	}
	return dest
}

func TestHeadReadsTheBranchAndCommit(t *testing.T) {
	dest := jobClone(t)
	run(t, dest, "commit", "-q", "--allow-empty", "-m", "one")
	want := run(t, dest, "rev-parse", "HEAD")
	branch, commit, err := Head(dest)
	if err != nil || branch != "stack/one" || commit != want {
		t.Fatalf("Head = %q %q %v, want stack/one %s", branch, commit, err, want)
	}

	run(t, dest, "pack-refs", "--all")
	if branch, commit, err := Head(dest); err != nil || branch != "stack/one" || commit != want {
		t.Fatalf("Head with packed refs = %q %q %v", branch, commit, err)
	}

	run(t, dest, "switch", "-q", "--detach")
	if branch, commit, err := Head(dest); err != nil || branch != "" || commit != want {
		t.Fatalf("Head detached = %q %q %v", branch, commit, err)
	}
}

func TestStartBranchStacksOnTheCurrentCommit(t *testing.T) {
	dest := jobClone(t)
	run(t, dest, "commit", "-q", "--allow-empty", "-m", "part one")
	tip := run(t, dest, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dest, "wip.txt"), []byte("not committed"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := (Runner{}).StartBranch(context.Background(), dest, "stack/two"); err != nil {
		t.Fatalf("StartBranch: %v", err)
	}
	if got := run(t, dest, "symbolic-ref", "--short", "HEAD"); got != "stack/two" {
		t.Fatalf("checked out %q, want stack/two", got)
	}
	if got := run(t, dest, "rev-parse", "stack/two"); got != tip {
		t.Fatalf("stack/two is at %s, want %s", got, tip)
	}
	if got := run(t, dest, "rev-parse", "stack/one"); got != tip {
		t.Fatalf("stack/one moved to %s", got)
	}
	if got := run(t, dest, "status", "--porcelain"); got != "?? wip.txt" {
		t.Fatalf("status = %q, want only the uncommitted file", got)
	}
	if config, _ := os.ReadFile(filepath.Join(dest, ".git", "config")); strings.Contains(string(config), "stack/two") {
		t.Fatalf("stack/two tracks something:\n%s", config)
	}
}

func TestStartBranchOnTheBranchCheckedOutChangesNothing(t *testing.T) {
	dest := jobClone(t)
	if err := (Runner{}).StartBranch(context.Background(), dest, "stack/one"); err != nil {
		t.Fatalf("StartBranch on the current branch: %v", err)
	}
	if got := run(t, dest, "symbolic-ref", "--short", "HEAD"); got != "stack/one" {
		t.Fatalf("checked out %q", got)
	}
}

func TestStartBranchRefusesABranchThatExists(t *testing.T) {
	dest := jobClone(t)
	run(t, dest, "branch", "loose")
	run(t, dest, "branch", "packed")
	run(t, dest, "pack-refs", "--all")
	run(t, dest, "branch", "-D", "loose")
	run(t, dest, "branch", "loose")
	for _, name := range []string{"loose", "packed", "main"} {
		if err := (Runner{}).StartBranch(context.Background(), dest, name); !errors.Is(err, ErrBranchExists) {
			t.Errorf("StartBranch(%s) = %v, want ErrBranchExists", name, err)
		}
	}
	if got := run(t, dest, "symbolic-ref", "--short", "HEAD"); got != "stack/one" {
		t.Fatalf("checked out %q after refusals", got)
	}
}

func TestStartBranchRefusesABadName(t *testing.T) {
	dest := jobClone(t)
	for _, name := range []string{"", "a..b", "-x", "a b", "../../escape"} {
		if err := (Runner{}).StartBranch(context.Background(), dest, name); err == nil {
			t.Errorf("StartBranch(%q) was accepted", name)
		}
	}
}

func TestStartBranchStaysInsideTheClone(t *testing.T) {
	dest := jobClone(t)
	outside := t.TempDir()
	heads := filepath.Join(dest, ".git", "refs", "heads")
	if err := os.Symlink(outside, filepath.Join(heads, "stack", "escape")); err != nil {
		t.Fatal(err)
	}
	if err := (Runner{}).StartBranch(context.Background(), dest, "stack/escape/two"); err == nil {
		t.Fatal("a branch was made through a link out of the clone")
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("StartBranch wrote outside the clone: %v", entries)
	}
}

func TestStartBranchRunsNothingFromTheClone(t *testing.T) {
	dest := jobClone(t)
	marker := filepath.Join(t.TempDir(), "ran")
	for _, kv := range [][2]string{{"core.fsmonitor", "touch " + marker}, {"core.hooksPath", filepath.Dir(marker)}, {"filter.x.smudge", "touch " + marker}} {
		run(t, dest, "config", kv[0], kv[1])
	}
	if err := os.WriteFile(filepath.Join(dest, ".gitattributes"), []byte("* filter=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (Runner{}).StartBranch(context.Background(), dest, "stack/two"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the clone's configuration ran a command")
	}
}
