package handofftest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cpave3/legato/internal/engine/sandbox"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// FakeRepos stands in for a worker's repository cache: it says which
// repositories the worker can clone, which branches GitHub has (those
// pushed with Push; every repository's default branch is main), and
// "clones" one by making an empty folder. It never runs git or reaches
// GitHub.
type FakeRepos struct {
	mu        sync.Mutex
	dir       string
	refused   map[string]string
	failing   map[string]error
	pushed    map[string]bool
	refreshes []string
	clones    []service.JobCloneRequest
	nextID    int64
}

// NewFakeRepos makes clones under dir; every repository can be cloned.
func NewFakeRepos(dir string) *FakeRepos {
	return &FakeRepos{dir: dir, refused: map[string]string{}, failing: map[string]error{}, pushed: map[string]bool{}}
}

// Push puts a branch of repo on the fake GitHub, as a person pushing work
// they began.
func (f *FakeRepos) Push(repo, branch string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushed[repo+"@"+branch] = true
}

// Refreshes lists the repositories fetched before a handoff's clone, oldest
// first.
func (f *FakeRepos) Refreshes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.refreshes...)
}

// PushedBranch "fetches" repo and says whether the branch was pushed, as
// service.RepositoryService.PushedBranch does; the default branch is main.
func (f *FakeRepos) PushedBranch(_ context.Context, repo, branch string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes = append(f.refreshes, repo)
	if err, ok := f.failing[repo]; ok {
		return "", err
	}
	if !f.pushed[repo+"@"+branch] {
		return "", fmt.Errorf("%w: %s has no branch %s", service.ErrBranchNotOnGitHub, repo, branch)
	}
	return "main", nil
}

// Refresh "fetches" repo, as service.RepositoryService.Refresh does.
func (f *FakeRepos) Refresh(_ context.Context, repo string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes = append(f.refreshes, repo)
	if err, ok := f.failing[repo]; ok {
		return "", err
	}
	return filepath.Join(f.dir, "cache", repo), nil
}

// Refuse makes the worker unable to clone repo, saying why.
func (f *FakeRepos) Refuse(repo, why string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refused[repo] = why
}

// Fail makes cloning repo fail with err once the worker tries.
func (f *FakeRepos) Fail(repo string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failing[repo] = err
}

// Clones lists the clones asked for, oldest first.
func (f *FakeRepos) Clones() []service.JobCloneRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]service.JobCloneRequest(nil), f.clones...)
}

// CloneProblem says why repo cannot be cloned, or "".
func (f *FakeRepos) CloneProblem(repo string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refused[repo]
}

// JobClone makes an empty folder for the clone.
func (f *FakeRepos) JobClone(_ context.Context, req service.JobCloneRequest) (store.JobClone, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.failing[req.Repo]; ok {
		return store.JobClone{}, err
	}
	if why, ok := f.refused[req.Repo]; ok {
		return store.JobClone{}, errors.New(why)
	}
	if req.PushedBranch && !f.pushed[req.Repo+"@"+req.Branch] {
		return store.JobClone{}, fmt.Errorf("%w: %s has no branch %s", service.ErrBranchNotOnGitHub, req.Repo, req.Branch)
	}
	f.clones = append(f.clones, req)
	f.nextID++
	_, name, _ := strings.Cut(req.Repo, "/")
	path := filepath.Join(f.dir, req.ExecutionID, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return store.JobClone{}, err
	}
	branch := req.Branch
	if branch == "" {
		branch = req.FreshBranch
	}
	return store.JobClone{ID: f.nextID, ExecutionID: req.ExecutionID, TaskID: req.TaskID, Repo: req.Repo, Ref: req.Ref, Branch: branch, Path: path}, nil
}

// fakeSandbox is a sandbox backend that is always there and wraps a launch
// command as "SANDBOX ... --".
type fakeSandbox struct{}

func (fakeSandbox) Name() string     { return "fake" }
func (fakeSandbox) Available() error { return nil }
func (fakeSandbox) Wrap(_ sandbox.Plan, command []string) ([]string, error) {
	return append([]string{"SANDBOX", "--"}, command...), nil
}

// FakeAgentTool is an agent tool a worker knows: its launch command is its
// name, and it can be launched with its permission prompts off.
type FakeAgentTool struct{ name string }

func (a FakeAgentTool) Name() string                { return a.name }
func (a FakeAgentTool) InstallHooks(string) error   { return nil }
func (a FakeAgentTool) UninstallHooks(string) error { return nil }
func (a FakeAgentTool) EnvVars(taskID, _ string) map[string]string {
	return map[string]string{"LEGATO_TASK_ID": taskID}
}
func (a FakeAgentTool) LaunchCommand(_ map[string]string, _, _, _ string) string { return a.name }
func (a FakeAgentTool) PromptsOffArgs() []string {
	return []string{fmt.Sprintf("--%s-prompts-off", a.name)}
}

// workerAgentTools are the agent tools a worker knows: claude-code (the
// default) and codex.
func workerAgentTools() map[string]service.AIToolAdapter {
	return map[string]service.AIToolAdapter{
		"claude-code": FakeAgentTool{name: "claude-code"},
		"codex":       FakeAgentTool{name: "codex"},
	}
}
