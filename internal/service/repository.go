package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/gitrepo"
	"github.com/cpave3/legato/internal/engine/store"
)

// RepoTokenSource gives the host read tokens for GitHub repositories, from
// the GitHub credential that lists them.
type RepoTokenSource interface {
	// GitReadToken returns a read token for one repository and the address
	// to clone it from. With no GitHub credential for the repository, the
	// error names the setting to change.
	GitReadToken(ctx context.Context, repo string) (token []byte, cloneURL string, err error)
	// PushesThroughProxy reports whether agent sessions' pushes to the
	// repository go to Legato's credential proxy.
	PushesThroughProxy(repo string) bool
	// CanRead says, without making a token, whether a GitHub credential
	// lists the repository; the error names the setting to change.
	CanRead(repo string) error
}

// RepoGit is the git the repository cache runs (gitrepo.Runner).
type RepoGit interface {
	CloneBare(ctx context.Context, url, dest string, token []byte) error
	Fetch(ctx context.Context, dir string, token []byte) error
	CloneJob(ctx context.Context, j gitrepo.JobClone) (string, error)
	StartBranch(ctx context.Context, clone, branch string) error
	UnpushedTips(ctx context.Context, clone, cache string) ([]string, error)
	OriginURL(ctx context.Context, dir string) (string, error)
	DefaultBranch(ctx context.Context, dir string) (string, error)
}

// RepositoryServiceOptions configures the repository cache.
type RepositoryServiceOptions struct {
	// DataDir holds the cache (repos/) and job clones (jobs/).
	DataDir string
	// MaxAge is how long a fetch is trusted; 0 fetches every time.
	MaxAge time.Duration
	// Tokens gives read tokens; nil when no GitHub credential is configured.
	Tokens RepoTokenSource
	Git    RepoGit
	Store  *store.Store
	// KeepFor is how long a clone with unpushed work is kept: 7 days unless set.
	KeepFor time.Duration
	// KeepIdle is how long a clone no agent works in is kept for the next
	// (a stack of branches in one folder): 0 removes it at once,
	// KeepIdleForever never.
	KeepIdle time.Duration
	Now      func() time.Time
}

// RepositoryService keeps Legato's own clones of GitHub repositories, and
// the clones made from them for jobs.
type RepositoryService struct {
	opts   RepositoryServiceOptions
	mu     sync.Mutex
	locks  map[string]*sync.Mutex
	tokens RepoTokenSource
}

// NewRepositoryService builds the repository cache.
func NewRepositoryService(opts RepositoryServiceOptions) *RepositoryService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.KeepFor == 0 {
		opts.KeepFor = 7 * 24 * time.Hour
	}
	return &RepositoryService{opts: opts, locks: map[string]*sync.Mutex{}, tokens: opts.Tokens}
}

// SetTokens gives the cache its GitHub credential once the credential
// channels are built.
func (s *RepositoryService) SetTokens(tokens RepoTokenSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = tokens
}

func (s *RepositoryService) tokenSource() RepoTokenSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// validRepo checks owner/name, which becomes a path under the cache.
func validRepo(repo string) error {
	if !repoName.MatchString(repo) {
		return fmt.Errorf("%q is not a GitHub repository (owner/name)", repo)
	}
	for _, part := range strings.Split(repo, "/") {
		if part == "." || part == ".." || part == ".git" {
			return fmt.Errorf("%q is not a GitHub repository (owner/name)", repo)
		}
	}
	return nil
}

// CachePath is where the cache keeps a repository.
func (s *RepositoryService) CachePath(repo string) string {
	owner, name, _ := strings.Cut(repo, "/")
	return filepath.Join(s.opts.DataDir, "repos", owner, strings.TrimSuffix(name, ".git")+".git")
}

// fetchedStamp is the file in a cached repository whose time is its last
// clone or fetch.
const fetchedStamp = "legato-fetched"

func (s *RepositoryService) lock(repo string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.locks[repo]
	if !ok {
		l = &sync.Mutex{}
		s.locks[repo] = l
	}
	return l
}

// Ensure makes the repository's cache when there is none, and fetches it
// when its last fetch is older than MaxAge. It returns the cache's path.
func (s *RepositoryService) Ensure(ctx context.Context, repo string) (string, error) {
	return s.ensure(ctx, repo, false)
}

// Refresh fetches the repository's cache now, making it when there is none.
func (s *RepositoryService) Refresh(ctx context.Context, repo string) (string, error) {
	return s.ensure(ctx, repo, true)
}

func (s *RepositoryService) ensure(ctx context.Context, repo string, force bool) (string, error) {
	if err := validRepo(repo); err != nil {
		return "", err
	}
	l := s.lock(repo)
	l.Lock()
	defer l.Unlock()
	path := s.CachePath(repo)
	stamp := filepath.Join(path, fetchedStamp)
	exists := false
	if _, err := os.Stat(filepath.Join(path, "HEAD")); err == nil {
		exists = true
		if info, err := os.Stat(stamp); err == nil && !force && s.opts.Now().Sub(info.ModTime()) < s.opts.MaxAge {
			return path, nil
		}
	}
	tokens := s.tokenSource()
	if tokens == nil {
		return "", fmt.Errorf("%s: Legato needs a GitHub credential to clone repositories (credentials in config.yaml)", repo)
	}
	token, url, err := tokens.GitReadToken(ctx, repo)
	if err != nil {
		return "", fmt.Errorf("%s: %w", repo, err)
	}
	if exists {
		if err := s.opts.Git.Fetch(ctx, path, token); err != nil {
			return "", fmt.Errorf("fetching %s: %w", repo, err)
		}
	} else if err := s.clone(ctx, repo, url, path, token); err != nil {
		return "", err
	}
	now := s.opts.Now()
	if err := os.WriteFile(stamp, nil, 0o600); err != nil {
		return "", err
	}
	if err := os.Chtimes(stamp, now, now); err != nil {
		return "", err
	}
	return path, nil
}

// clone makes the cache in a folder beside path and moves it into place,
// so a clone that fails leaves nothing and another Legato's finished clone
// is kept.
func (s *RepositoryService) clone(ctx context.Context, repo, url, path string, token []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	dest := filepath.Join(tmp, "repo.git")
	if err := s.opts.Git.CloneBare(ctx, url, dest, token); err != nil {
		return fmt.Errorf("cloning %s: %w", repo, err)
	}
	if err := os.Rename(dest, path); err != nil {
		if _, statErr := os.Stat(filepath.Join(path, "HEAD")); statErr == nil {
			return nil
		}
		return err
	}
	return nil
}

// KeepIdleForever keeps idle job clones until they are removed by hand.
const KeepIdleForever time.Duration = -1

// ErrCloneBusy is a change to a job clone that another agent is working in.
var ErrCloneBusy = errors.New("another agent is working in the clone")

// JobClonePushPlaceholder is where a job clone's pushes go when no
// GitHub credential sends them to Legato's proxy: a path that is no
// repository, so git push fails without contacting GitHub.
const JobClonePushPlaceholder = "/nonexistent/legato-pushes-need-a-github-credential"

// JobCloneRequest asks for a clone of a repository for one job.
type JobCloneRequest struct {
	ExecutionID string
	TaskID      string
	Repo        string // owner/name
	Ref         string // branch, tag or commit; the default branch when empty
	// Branch, when set, is the branch the clone ends on. In a new clone:
	// GitHub's branch of that name when there is one, else a new one made at
	// Ref. In a clone joined: a new branch at the commit it has checked out.
	Branch string
	// FreshBranch is the branch a new clone ends on when Branch is empty.
	FreshBranch string
	// CloneID, when set, is a job clone to work in instead of a new one.
	// Without it, a task with a clone of Repo works in that one again.
	CloneID int64
}

var executionName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// JobDir is the folder that holds an execution's job clones.
func (s *RepositoryService) JobDir(executionID string) string {
	return filepath.Join(s.opts.DataDir, "jobs", executionID)
}

// JobClone gives a task a job clone to work in: the one CloneID names,
// else the task's own clone of Repo when it has one, else a new clone of
// the repository at the ref, at jobs/<execution>/<name> beside the cache,
// whose objects it borrows. A new clone's origin names the repository on
// GitHub; when the session's pushes do not go to Legato's proxy, they go
// to JobClonePushPlaceholder. The clone is recorded with the task, and the
// task's code context names the repository and branch.
func (s *RepositoryService) JobClone(ctx context.Context, req JobCloneRequest) (store.JobClone, error) {
	if !executionName.MatchString(req.ExecutionID) {
		return store.JobClone{}, fmt.Errorf("%q is not an execution", req.ExecutionID)
	}
	if req.CloneID != 0 {
		clone, err := s.opts.Store.JobCloneByID(ctx, req.CloneID)
		if err != nil {
			return store.JobClone{}, fmt.Errorf("job clone %d: %w", req.CloneID, err)
		}
		return s.joinJobClone(ctx, req, *clone)
	}
	if own, err := s.opts.Store.LiveJobCloneForTask(ctx, req.TaskID, req.Repo); err == nil {
		if _, statErr := os.Lstat(own.Path); statErr == nil {
			return s.joinJobClone(ctx, req, *own)
		}
		_ = s.opts.Store.MarkJobCloneRemoved(ctx, own.ID, s.opts.Now())
	}
	if req.Branch == "" {
		req.Branch = req.FreshBranch
	}
	cache, err := s.Ensure(ctx, req.Repo)
	if err != nil {
		return store.JobClone{}, err
	}
	origin, err := s.opts.Git.OriginURL(ctx, cache)
	if err != nil {
		return store.JobClone{}, fmt.Errorf("reading the cache of %s: %w", req.Repo, err)
	}
	_, name, _ := strings.Cut(req.Repo, "/")
	dest := filepath.Join(s.JobDir(req.ExecutionID), strings.TrimSuffix(name, ".git"))
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return store.JobClone{}, err
	}
	push := ""
	if tokens := s.tokenSource(); tokens == nil || !tokens.PushesThroughProxy(req.Repo) {
		push = JobClonePushPlaceholder
	}
	base := req.Ref
	if base == "" {
		if base, err = s.opts.Git.DefaultBranch(ctx, cache); err != nil {
			return store.JobClone{}, fmt.Errorf("reading the default branch of %s: %w", req.Repo, err)
		}
	}
	branch, err := s.opts.Git.CloneJob(ctx, gitrepo.JobClone{Cache: cache, Origin: origin, Dest: dest, Ref: base, PushURL: push, NewBranch: req.Branch})
	if err != nil {
		return store.JobClone{}, fmt.Errorf("checking out %s: %w", req.Repo, err)
	}
	clone := store.JobClone{ExecutionID: req.ExecutionID, TaskID: req.TaskID, Repo: req.Repo, Ref: req.Ref, Branch: branch,
		Path: dest, CreatedAt: s.opts.Now(), TaskIDs: []string{req.TaskID}}
	clone.LastUsedAt = clone.CreatedAt
	if clone.ID, err = s.opts.Store.CreateJobClone(ctx, clone); err != nil {
		_ = os.RemoveAll(dest)
		return store.JobClone{}, err
	}
	s.recordCodeContext(ctx, clone, base)
	return clone, nil
}

// joinJobClone gives a task a clone that exists: on its current branch, or
// on a new branch stacked there. Moving the clone to a new branch while
// another agent works in it is ErrCloneBusy, since that agent would move too.
func (s *RepositoryService) joinJobClone(ctx context.Context, req JobCloneRequest, clone store.JobClone) (store.JobClone, error) {
	if req.Repo != "" && req.Repo != clone.Repo {
		return store.JobClone{}, fmt.Errorf("job clone %d is of %s, not %s", clone.ID, clone.Repo, req.Repo)
	}
	if _, err := os.Lstat(clone.Path); err != nil {
		return store.JobClone{}, fmt.Errorf("the job clone at %s is gone: %w", clone.Path, err)
	}
	current, _, err := gitrepo.Head(clone.Path)
	if err != nil {
		return store.JobClone{}, fmt.Errorf("reading the branch of %s: %w", clone.Path, err)
	}
	base := ""
	if req.Branch != "" && req.Branch != current {
		busy, err := s.cloneBusy(ctx, clone)
		if err != nil {
			return store.JobClone{}, err
		}
		if busy {
			return store.JobClone{}, fmt.Errorf("%w: it is on %s; start this agent there, or on %s once that agent has finished",
				ErrCloneBusy, displayBranch(current), req.Branch)
		}
		if err := s.opts.Git.StartBranch(ctx, clone.Path, req.Branch); err != nil {
			return store.JobClone{}, fmt.Errorf("starting %s in %s: %w", req.Branch, clone.Path, err)
		}
		base, current = current, req.Branch
	}
	if err := s.opts.Store.AttachJobCloneTask(ctx, clone.ID, req.TaskID, s.opts.Now()); err != nil {
		return store.JobClone{}, err
	}
	clone.TaskID, clone.Branch = req.TaskID, current
	s.recordCodeContext(ctx, clone, base)
	return clone, nil
}

func displayBranch(branch string) string {
	if branch == "" {
		return "a detached HEAD"
	}
	return branch
}

func (s *RepositoryService) cloneBusy(ctx context.Context, clone store.JobClone) (bool, error) {
	running, err := s.opts.Store.ListRunningExecutions(ctx)
	if err != nil {
		return false, err
	}
	return inUse(clone, running), nil
}

// JobCloneInfo is a live job clone as the web UI lists it.
type JobCloneInfo struct {
	ID   int64  `json:"id"`
	Repo string `json:"repo"`
	Path string `json:"path"`
	// Branch is the branch checked out now, "" when detached.
	Branch     string    `json:"branch"`
	TaskIDs    []string  `json:"task_ids"`
	Busy       bool      `json:"busy"`
	LastUsedAt time.Time `json:"last_used_at"`
	// Unpushed counts branch tips Legato's copy of GitHub lacks.
	Unpushed    int        `json:"unpushed"`
	RemoveAfter *time.Time `json:"remove_after,omitempty"`
	Problem     string     `json:"problem,omitempty"`
}

// ListJobClones lists the live job clones, the most recently used first.
func (s *RepositoryService) ListJobClones(ctx context.Context) ([]JobCloneInfo, error) {
	clones, err := s.opts.Store.ListLiveJobClones(ctx)
	if err != nil {
		return nil, err
	}
	running, err := s.opts.Store.ListRunningExecutions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]JobCloneInfo, 0, len(clones))
	for _, c := range clones {
		info := JobCloneInfo{ID: c.ID, Repo: c.Repo, Path: c.Path, TaskIDs: c.TaskIDs, Busy: inUse(c, running),
			LastUsedAt: c.LastUsedAt, RemoveAfter: c.RemoveAfter}
		if info.TaskIDs == nil {
			info.TaskIDs = []string{}
		}
		if branch, _, err := gitrepo.Head(c.Path); err != nil {
			info.Problem = err.Error()
		} else {
			info.Branch = branch
			tips, err := s.opts.Git.UnpushedTips(ctx, c.Path, s.CachePath(c.Repo))
			if err != nil {
				info.Problem = err.Error()
			}
			info.Unpushed = len(tips)
		}
		out = append(out, info)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastUsedAt.After(out[j].LastUsedAt) })
	return out, nil
}

// RemoveJobClone removes a job clone now, with whatever is unpushed in it.
// One an agent is working in is ErrCloneBusy.
func (s *RepositoryService) RemoveJobClone(ctx context.Context, id int64) error {
	clone, err := s.opts.Store.JobCloneByID(ctx, id)
	if err != nil {
		return err
	}
	busy, err := s.cloneBusy(ctx, *clone)
	if err != nil {
		return err
	}
	if busy {
		return ErrCloneBusy
	}
	return s.removeJobClone(ctx, *clone)
}

// recordCodeContext names the clone's repository and branch on its task,
// unless the person linked the task to a repository themselves. With no
// base, one already recorded for the same branch is kept.
func (s *RepositoryService) recordCodeContext(ctx context.Context, clone store.JobClone, base string) {
	task, err := s.opts.Store.GetTask(ctx, clone.TaskID)
	if err != nil {
		return
	}
	existing, _ := store.ParseCodeContext(task.CodeContext)
	if existing != nil && (existing.RepoSource == "manual" || existing.BranchSource == "manual") {
		return
	}
	if base == "" && existing != nil && existing.Branch == clone.Branch {
		base = existing.BaseBranch
	}
	now := s.opts.Now().UTC().Format(time.RFC3339)
	cc := &store.CodeContext{Repo: clone.Repo, Branch: clone.Branch, BaseBranch: base, WorkingDir: clone.Path,
		RepoSource: "job_clone", BranchSource: "job_clone", LinkedAt: now, UpdatedAt: now}
	if err := s.opts.Store.SetCodeContext(ctx, clone.TaskID, cc); err != nil {
		log.Printf("repositories: the code context of %s: %v", clone.TaskID, err)
	}
}

// jobCloneGrace is how long a clone is left after it was last used, at the
// least: the spawn that made or joined it records its execution a moment
// later.
const jobCloneGrace = 10 * time.Minute

// SweepJobClones removes the job clones no running agent has worked in for
// KeepIdle and whose branches hold nothing unpushed. A clone with unpushed commits is
// kept for KeepFor, with a note on its task, and then removed. The cache
// is fetched first, so commits pushed since are known to be pushed.
func (s *RepositoryService) SweepJobClones(ctx context.Context) error {
	clones, err := s.opts.Store.ListLiveJobClones(ctx)
	if err != nil {
		return err
	}
	running, err := s.opts.Store.ListRunningExecutions(ctx)
	if err != nil {
		return err
	}
	fetched := map[string]error{}
	now := s.opts.Now()
	for _, clone := range clones {
		if inUse(clone, running) {
			if err := s.opts.Store.TouchJobClone(ctx, clone.ID, now); err != nil {
				log.Printf("repositories: recording the use of %s: %v", clone.Path, err)
			}
			continue
		}
		if clone.RemoveAfter != nil {
			if !now.Before(*clone.RemoveAfter) {
				if err := s.removeJobClone(ctx, clone); err != nil {
					log.Printf("repositories: %v", err)
				}
			}
			continue
		}
		idle := now.Sub(clone.LastUsedAt)
		if idle < jobCloneGrace || s.opts.KeepIdle < 0 || idle < s.opts.KeepIdle {
			continue
		}
		if _, err := os.Lstat(clone.Path); errors.Is(err, fs.ErrNotExist) {
			_ = s.opts.Store.MarkJobCloneRemoved(ctx, clone.ID, now)
			continue
		}
		fetchErr, done := fetched[clone.Repo]
		if !done {
			_, fetchErr = s.Refresh(ctx, clone.Repo)
			fetched[clone.Repo] = fetchErr
		}
		if fetchErr != nil {
			log.Printf("repositories: keeping the job clone %s until %s can be fetched: %v", clone.Path, clone.Repo, fetchErr)
			continue
		}
		tips, err := s.opts.Git.UnpushedTips(ctx, clone.Path, s.CachePath(clone.Repo))
		if err == nil && len(tips) == 0 {
			if err := s.removeJobClone(ctx, clone); err != nil {
				log.Printf("repositories: %v", err)
			}
			continue
		}
		s.keepJobClone(ctx, clone, tips, err)
	}
	return nil
}

func inUse(clone store.JobClone, running []store.Execution) bool {
	for _, e := range running {
		if e.ID == clone.ExecutionID || within(e.WorkingDir, clone.Path) {
			return true
		}
	}
	return false
}

// keepJobClone keeps a clone with unpushed work, or one Legato could not
// check, for KeepFor, and says so on its task.
func (s *RepositoryService) keepJobClone(ctx context.Context, clone store.JobClone, tips []string, checkErr error) {
	until := s.opts.Now().Add(s.opts.KeepFor)
	if err := s.opts.Store.KeepJobCloneUntil(ctx, clone.ID, until); err != nil {
		log.Printf("repositories: keeping the job clone %s: %v", clone.Path, err)
		return
	}
	what := "has commits that were never pushed (" + shortList(tips) + ")"
	if checkErr != nil {
		what = "could not be checked for unpushed commits (" + checkErr.Error() + ")"
	}
	body := fmt.Sprintf("The job clone of %s at %s %s. Legato keeps it until %s and then removes it.",
		clone.Repo, clone.Path, what, until.UTC().Format("2006-01-02 15:04 UTC"))
	if _, err := AddTaskNote(ctx, s.opts.Store, clone.TaskID, body, "legato"); err != nil {
		log.Printf("repositories: a note on %s about its job clone: %v", clone.TaskID, err)
	}
}

func shortList(shas []string) string {
	short := make([]string, len(shas))
	for i, sha := range shas {
		short[i] = sha[:min(len(sha), 12)]
	}
	return strings.Join(short, ", ")
}

// removeJobClone removes a clone's folder, and its execution's folder when
// nothing else is in it.
func (s *RepositoryService) removeJobClone(ctx context.Context, clone store.JobClone) error {
	jobs := filepath.Join(s.opts.DataDir, "jobs")
	if !within(clone.Path, jobs) || clone.Path == jobs {
		return fmt.Errorf("%s is not in %s; not removing it", clone.Path, jobs)
	}
	if err := os.RemoveAll(clone.Path); err != nil {
		return fmt.Errorf("removing the job clone %s: %w", clone.Path, err)
	}
	_ = os.Remove(filepath.Dir(clone.Path)) // only when empty
	if err := s.opts.Store.MarkJobCloneRemoved(ctx, clone.ID, s.opts.Now()); err != nil {
		return fmt.Errorf("recording the removal of %s: %w", clone.Path, err)
	}
	return nil
}

// CloneProblem says why the cache cannot clone a repository, "" when it can:
// no GitHub credential, or none that lists it.
func (s *RepositoryService) CloneProblem(repo string) string {
	tokens := s.tokenSource()
	if tokens == nil {
		return "no github credential is configured (credentials in config.yaml)"
	}
	if err := tokens.CanRead(repo); err != nil {
		return err.Error()
	}
	return ""
}
