// Package gitrepo runs git for Legato's repository cache: bare clones and
// fetches that authenticate with a token, and clones for jobs that borrow
// the cache's objects.
//
// A token reaches git through a pipe the git process inherits, read by an
// askpass helper (the legato binary itself, see AskPass). It is never in an
// argument, an environment variable, a git configuration or a file.
//
// Every git process runs without the system and global configuration, with
// no credential helper, hooks or fsmonitor, so neither the person's own
// git settings nor anything planted in a repository runs with it.
package gitrepo

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// AskPassEnv, set in git's environment, makes the program GIT_ASKPASS names
// answer as the askpass helper. Its value is the file descriptor the token
// is read from.
const AskPassEnv = "LEGATO_GIT_ASKPASS_FD"

// tokenFD is where git and its children find the token's pipe: the first of
// exec.Cmd's ExtraFiles.
const tokenFD = "3"

// tokenUser is the user name GitHub takes with an installation token.
const tokenUser = "x-access-token"

// IsAskPass says whether this process was started by git as Runner's
// askpass helper.
func IsAskPass(getenv func(string) string) bool { return getenv(AskPassEnv) != "" }

// AskPass answers git's prompt (its one argument): the user name for a
// "Username" prompt, and the token read from the inherited pipe for a
// "Password" prompt. It returns the process's exit code.
func AskPass(args []string, stdout, stderr io.Writer) int {
	prompt := strings.Join(args, " ")
	if strings.HasPrefix(prompt, "Username") {
		fmt.Fprintln(stdout, tokenUser)
		return 0
	}
	if !strings.HasPrefix(prompt, "Password") {
		fmt.Fprintln(stderr, "legato: git asked something other than a user name or password")
		return 1
	}
	pipe := os.NewFile(3, "token")
	if pipe == nil || os.Getenv(AskPassEnv) != tokenFD {
		fmt.Fprintln(stderr, "legato: no token was given to git")
		return 1
	}
	defer pipe.Close()
	token, err := bufio.NewReader(io.LimitReader(pipe, 1<<16)).ReadString('\n')
	token = strings.TrimSpace(token)
	if (err != nil && !errors.Is(err, io.EOF)) || token == "" {
		fmt.Fprintln(stderr, "legato: no token was given to git")
		return 1
	}
	fmt.Fprintln(stdout, token)
	return 0
}

// Runner runs git.
type Runner struct {
	// Git is the git binary; "git" when empty.
	Git string
	// AskPass is the program git runs for a password: the legato binary,
	// which answers as AskPass when AskPassEnv is set.
	AskPass string
}

func (r Runner) git() string {
	if r.Git == "" {
		return "git"
	}
	return r.Git
}

// hardened are the options every git process gets: no credential helper,
// no hooks, no fsmonitor.
var hardened = []string{"-c", "credential.helper=", "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false",
	"-c", "credential.username=" + tokenUser}

// keptEnv are the variables of Legato's own environment git keeps: what it
// needs to find programs and reach the network. No credential is one.
var keptEnv = []string{"PATH", "HOME", "LANG", "LC_ALL", "TZ", "TMPDIR",
	"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "NO_PROXY", "no_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "GIT_SSL_CAINFO", "GIT_SSL_CAPATH"}

func (r Runner) env() []string {
	env := []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}
	for _, name := range keptEnv {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// run runs git with args in dir. With a token, git can ask for it once
// through the askpass helper.
func (r Runner) run(ctx context.Context, dir string, token []byte, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, r.git(), append(append([]string{}, hardened...), args...)...)
	cmd.Dir = dir
	cmd.Env = r.env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if token != nil {
		if r.AskPass == "" {
			return "", errors.New("no askpass helper to give git the token")
		}
		read, write, err := os.Pipe()
		if err != nil {
			return "", err
		}
		defer read.Close()
		// A token is far smaller than a pipe's buffer: written whole before
		// git starts, and the write end closed so a second read ends.
		_, err = write.Write(append(append([]byte{}, token...), '\n'))
		write.Close()
		if err != nil {
			return "", err
		}
		cmd.ExtraFiles = []*os.File{read}
		cmd.Env = append(cmd.Env, "GIT_ASKPASS="+r.AskPass, AskPassEnv+"="+tokenFD)
	}
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, lastLine(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// lastLine is the last line git wrote, which says what went wrong.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// CloneBare makes a bare clone of url at dest holding its branches and
// tags. dest's remote keeps url, and fetches update its branches.
func (r Runner) CloneBare(ctx context.Context, url, dest string, token []byte) error {
	if _, err := r.run(ctx, "", token, "clone", "--quiet", "--bare", url, dest); err != nil {
		return err
	}
	// A bare clone sets no fetch refspec: a later fetch would update nothing.
	_, err := r.run(ctx, "", nil, "--git-dir", dest, "config", "remote.origin.fetch", "+refs/heads/*:refs/heads/*")
	return err
}

// Fetch updates a bare clone's branches and tags from its remote, removing
// branches the remote no longer has.
func (r Runner) Fetch(ctx context.Context, dir string, token []byte) error {
	_, err := r.run(ctx, "", token, "--git-dir", dir, "fetch", "--quiet", "--prune", "--tags", "origin")
	return err
}

// JobClone describes a clone made for one job.
type JobClone struct {
	// Cache is the bare clone whose objects the job's clone borrows.
	Cache string
	// Origin is the address the clone's origin names: the repository on
	// GitHub, not the cache.
	Origin string
	// Dest is where the clone is made; it must not exist.
	Dest string
	// Ref is the branch, tag or commit checked out; the cache's default
	// branch when empty.
	Ref string
	// PushURL, when set, is where origin's pushes go instead of Origin.
	PushURL string
	// NewBranch, when set, is the branch the clone ends on: origin's branch
	// of that name when there is one, else a new branch made at Ref that
	// tracks nothing (so a bare git push does not go to the base).
	NewBranch string
}

// CloneJob clones the cache into j.Dest, borrowing its objects, and checks
// out j.Ref: a branch as a local branch that tracks origin's, anything else
// detached; then j.NewBranch when set. It returns the branch checked out,
// "" when detached. A clone that fails is removed.
func (r Runner) CloneJob(ctx context.Context, j JobClone) (branch string, err error) {
	cache, err := filepath.Abs(j.Cache)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(j.Dest)
		}
	}()
	if j.NewBranch != "" {
		if _, err := r.run(ctx, "", nil, "check-ref-format", "--branch", j.NewBranch); err != nil {
			return "", fmt.Errorf("%q is not a branch name git takes", j.NewBranch)
		}
	}
	if _, err := r.run(ctx, "", nil, "clone", "--quiet", "--no-checkout", "--reference", cache, "file://"+cache, j.Dest); err != nil {
		return "", err
	}
	in := func(args ...string) (string, error) { return r.run(ctx, j.Dest, nil, args...) }
	if _, err := in("remote", "set-url", "origin", j.Origin); err != nil {
		return "", err
	}
	if j.PushURL != "" {
		if _, err := in("config", "remote.origin.pushurl", j.PushURL); err != nil {
			return "", err
		}
	}
	ref := j.Ref
	if ref == "" {
		if ref, err = in("symbolic-ref", "--short", "HEAD"); err != nil {
			return "", err
		}
	}
	onOrigin := func(branch string) bool {
		_, err := in("rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
		return err == nil
	}
	if j.NewBranch != "" && onOrigin(j.NewBranch) {
		ref = j.NewBranch
	}
	if onOrigin(ref) {
		if _, err := in("checkout", "--quiet", "--force", "-B", ref, "--track", "origin/"+ref); err != nil {
			return "", err
		}
		branch = ref
	} else if _, err := in("checkout", "--quiet", "--force", "--detach", ref); err != nil {
		return "", fmt.Errorf("%s is no branch, tag or commit of the repository: %w", ref, err)
	}
	if j.NewBranch == "" || j.NewBranch == branch {
		return branch, nil
	}
	if _, err := in("checkout", "--quiet", "--no-track", "-b", j.NewBranch); err != nil {
		return "", err
	}
	return j.NewBranch, nil
}

var objectName = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// UnpushedTips lists the commits at the tips of a job clone's branches, and
// at a detached HEAD, that the cache does not have. The clone's refs are
// read as files: no git process runs in the clone, whose configuration and
// hooks the agent controls. A clone whose .git is not a plain folder is an
// error, so the caller keeps it.
func (r Runner) UnpushedTips(ctx context.Context, clone, cache string) ([]string, error) {
	gitDir := filepath.Join(clone, ".git")
	if info, err := os.Lstat(gitDir); err != nil {
		return nil, err
	} else if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", gitDir)
	}
	tips := map[string]bool{}
	add := func(data []byte) {
		if sha := strings.TrimSpace(string(data)); objectName.MatchString(sha) {
			tips[sha] = true
		}
	}
	if head, err := readRegular(filepath.Join(gitDir, "HEAD")); err == nil {
		add(head)
	}
	if packed, err := readRegular(filepath.Join(gitDir, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(packed), "\n") {
			if sha, ref, ok := strings.Cut(line, " "); ok && strings.HasPrefix(ref, "refs/heads/") {
				add([]byte(sha))
			}
		}
	}
	heads := filepath.Join(gitDir, "refs", "heads")
	err := filepath.WalkDir(heads, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == heads && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.Type().IsRegular() {
			if data, err := readRegular(path); err == nil {
				add(data)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var absent []string
	for sha := range tips {
		if _, err := r.run(ctx, "", nil, "--git-dir", cache, "cat-file", "-e", sha+"^{commit}"); err != nil {
			absent = append(absent, sha)
		}
	}
	sort.Strings(absent)
	return absent, nil
}

// readRegular reads a small regular file, never through a link.
func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 1<<24))
}

// OriginURL is the address a bare clone fetches from.
func (r Runner) OriginURL(ctx context.Context, dir string) (string, error) {
	return r.run(ctx, "", nil, "--git-dir", dir, "config", "--get", "remote.origin.url")
}

// DefaultBranch is the branch a bare clone's HEAD names: the repository's
// default branch.
func (r Runner) DefaultBranch(ctx context.Context, dir string) (string, error) {
	return r.run(ctx, "", nil, "--git-dir", dir, "symbolic-ref", "--short", "HEAD")
}

// ErrBranchExists is a branch StartBranch will not make: one of that name
// is there already, and is not the one checked out.
var ErrBranchExists = errors.New("the branch exists")

// openGitDir opens a clone's .git folder so that nothing read or written
// through it can leave that folder, whatever links are planted in it.
func openGitDir(clone string) (*os.Root, error) {
	top, err := os.OpenRoot(clone)
	if err != nil {
		return nil, err
	}
	defer top.Close()
	gitDir, err := top.OpenRoot(".git")
	if err != nil {
		return nil, fmt.Errorf("%s has no .git folder of its own: %w", clone, err)
	}
	return gitDir, nil
}

func readSmall(root *os.Root, name string) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil {
		return nil, err
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", name)
	}
	return io.ReadAll(io.LimitReader(f, 1<<24))
}

// branchCommit is the commit a branch is at, from its file or packed-refs;
// ok is false when there is no such branch.
func branchCommit(gitDir *os.Root, branch string) (commit string, ok bool, err error) {
	data, err := readSmall(gitDir, "refs/heads/"+branch)
	switch {
	case err == nil:
		sha := strings.TrimSpace(string(data))
		if !objectName.MatchString(sha) {
			return "", true, fmt.Errorf("the branch %s holds no commit", branch)
		}
		return sha, true, nil
	case !errors.Is(err, fs.ErrNotExist):
		return "", false, err
	}
	packed, err := readSmall(gitDir, "packed-refs")
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	for _, line := range strings.Split(string(packed), "\n") {
		if sha, ref, found := strings.Cut(line, " "); found && ref == "refs/heads/"+branch {
			return sha, true, nil
		}
	}
	return "", false, nil
}

func head(gitDir *os.Root) (branch, commit string, err error) {
	data, err := readSmall(gitDir, "HEAD")
	if err != nil {
		return "", "", err
	}
	text := strings.TrimSpace(string(data))
	if ref, ok := strings.CutPrefix(text, "ref: "); ok {
		name, ok := strings.CutPrefix(ref, "refs/heads/")
		if !ok {
			return "", "", fmt.Errorf("HEAD names %s, which is not a branch", ref)
		}
		commit, _, err := branchCommit(gitDir, name)
		return name, commit, err
	}
	if !objectName.MatchString(text) {
		return "", "", errors.New("HEAD names no commit")
	}
	return "", text, nil
}

// Head reads which branch a clone has checked out ("" when detached) and
// the commit there ("" on a branch with no commits). It reads git's files:
// no git runs in the clone.
func Head(clone string) (branch, commit string, err error) {
	gitDir, err := openGitDir(clone)
	if err != nil {
		return "", "", err
	}
	defer gitDir.Close()
	return head(gitDir)
}

// StartBranch makes a branch at the commit a clone has checked out and
// checks it out, tracking nothing: a branch stacked on the current one.
// The working tree and index are not touched, so uncommitted changes stay,
// as with git switch -c. On the branch checked out it does nothing; one
// that exists elsewhere is ErrBranchExists. It writes git's files itself
// (the branch only if absent, HEAD through git's HEAD.lock), so no git runs
// in the clone and nothing written can land outside its .git folder.
func (r Runner) StartBranch(ctx context.Context, clone, branch string) error {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return fmt.Errorf("%q is not a branch name", branch)
	}
	if _, err := r.run(ctx, "", nil, "check-ref-format", "--branch", branch); err != nil {
		return fmt.Errorf("%q is not a branch name: %w", branch, err)
	}
	gitDir, err := openGitDir(clone)
	if err != nil {
		return err
	}
	defer gitDir.Close()
	current, commit, err := head(gitDir)
	if err != nil {
		return err
	}
	if current == branch {
		return nil
	}
	if commit == "" {
		return errors.New("the clone has no commit to start a branch from")
	}
	if _, exists, err := branchCommit(gitDir, branch); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("%w: %s", ErrBranchExists, branch)
	}

	ref := "refs/heads/" + branch
	if err := gitDir.MkdirAll(filepath.Dir(ref), 0o755); err != nil {
		return err
	}
	if err := writeNew(gitDir, ref, commit+"\n"); err != nil {
		return err
	}
	if err := writeNew(gitDir, "HEAD.lock", "ref: "+ref+"\n"); err != nil {
		_ = gitDir.Remove(ref)
		return err
	}
	if err := gitDir.Rename("HEAD.lock", "HEAD"); err != nil {
		_ = gitDir.Remove("HEAD.lock")
		_ = gitDir.Remove(ref)
		return err
	}
	return nil
}

// writeNew writes a file that must not exist yet, so a git busy in the
// clone is never overwritten.
func writeNew(root *os.Root, name, content string) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		_ = root.Remove(name)
		return err
	}
	return f.Close()
}
