package service

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// ErrOwnerQuery is an owner lookup without a repository and one more thing
// to match it by.
var ErrOwnerQuery = errors.New("give repo with at least one of pr, branch and sha (a sha of 7 or more characters)")

// OwnerQuery names work in a repository: its pull request, branch or commit.
type OwnerQuery struct {
	Repo   string
	PR     int
	Branch string
	SHA    string
}

// OwnerMatch is a task that owns the work, with its agents.
type OwnerMatch struct {
	Task     OwnerTask      `json:"task"`
	Agents   []OwnerAgent   `json:"agents"`
	Worktree *OwnerWorktree `json:"worktree,omitempty"`
	PR       *OwnerPR       `json:"pr,omitempty"`
	// Match is the rule that matched: pr, sha or branch.
	Match string `json:"match"`
}

type OwnerTask struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	URL    string `json:"url,omitempty"`
}

// OwnerAgent is one of a task's agents. State is working, waiting, idle or
// error for a running agent, or stopped for one whose conversation can be
// resumed.
type OwnerAgent struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	State string `json:"state"`
}

type OwnerWorktree struct {
	Branch string `json:"branch"`
}

type OwnerPR struct {
	Number int    `json:"number"`
	URL    string `json:"url,omitempty"`
	State  string `json:"state,omitempty"`
}

// OwnerLookupService finds the tasks that own a repository's pull request,
// branch or commit.
type OwnerLookupService struct {
	db        *store.Store
	agents    AgentService
	publicURL string
}

// NewOwnerLookupService makes the service. publicURL is the web UI's
// address for links to tasks; empty leaves them out.
func NewOwnerLookupService(db *store.Store, agents AgentService, publicURL string) *OwnerLookupService {
	return &OwnerLookupService{db: db, agents: agents, publicURL: strings.TrimRight(publicURL, "/")}
}

const (
	matchPR     = "pr"
	matchSHA    = "sha"
	matchBranch = "branch"
)

// Lookup returns the matching tasks, best first: by PR number, then commit,
// then branch; within each, tasks with a running agent first, then the most
// recently updated.
func (s *OwnerLookupService) Lookup(ctx context.Context, q OwnerQuery) ([]OwnerMatch, error) {
	q.Repo, q.Branch, q.SHA = strings.TrimSpace(q.Repo), strings.TrimSpace(q.Branch), strings.ToLower(strings.TrimSpace(q.SHA))
	if q.SHA != "" && len(q.SHA) < 7 {
		q.SHA = ""
	}
	if q.Repo == "" || (q.PR <= 0 && q.Branch == "" && q.SHA == "") {
		return nil, ErrOwnerQuery
	}
	tasks, err := s.db.ListTasksForRepository(ctx, q.Repo)
	if err != nil {
		return nil, err
	}
	type ranked struct {
		match   OwnerMatch
		rank    int
		live    bool
		updated string
	}
	var found []ranked
	for i := range tasks {
		task := &tasks[i]
		cc, _ := store.ParseCodeContext(task.CodeContext)
		pr, _ := store.ParsePRMeta(task.PRMeta)
		match, rank := ownerRule(q, cc, pr)
		if match == "" {
			continue
		}
		presence, err := taskAgentPresence(ctx, s.db, task)
		if err != nil {
			return nil, err
		}
		m := OwnerMatch{Task: OwnerTask{ID: task.ID, Title: task.Title, Status: task.Status}, Agents: presence.agents(), Match: match}
		if s.publicURL != "" {
			m.Task.URL = s.publicURL + "/board?highlight_task=" + task.ID
		}
		if branch := ownerBranch(task, cc); branch != "" {
			m.Worktree = &OwnerWorktree{Branch: branch}
		}
		if pr != nil && pr.PRNumber > 0 {
			m.PR = &OwnerPR{Number: pr.PRNumber, URL: pr.PRURL, State: pr.State}
		}
		found = append(found, ranked{match: m, rank: rank, live: presence.running != nil, updated: task.UpdatedAt})
	}
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.live != b.live {
			return a.live
		}
		return a.updated > b.updated
	})
	matches := make([]OwnerMatch, 0, len(found))
	for _, f := range found {
		matches = append(matches, f.match)
	}
	return matches, nil
}

// ownerRule is the best rule a task matches, with its rank (lower is better).
func ownerRule(q OwnerQuery, cc *store.CodeContext, pr *store.PRMeta) (string, int) {
	if q.PR > 0 && pr != nil && pr.PRNumber == q.PR && strings.EqualFold(pr.Repo, q.Repo) {
		return matchPR, 0
	}
	if q.SHA != "" {
		for _, sha := range []string{codeContextSHA(cc), prSHA(pr)} {
			if sha != "" && strings.HasPrefix(strings.ToLower(sha), q.SHA) {
				return matchSHA, 1
			}
		}
	}
	if q.Branch != "" && cc != nil && strings.EqualFold(cc.Repo, q.Repo) && cc.Branch == q.Branch {
		return matchBranch, 2
	}
	return "", 0
}

func codeContextSHA(cc *store.CodeContext) string {
	if cc == nil {
		return ""
	}
	return cc.HeadSHA
}

func prSHA(pr *store.PRMeta) string {
	if pr == nil {
		return ""
	}
	return pr.HeadSHA
}

func ownerBranch(task *store.Task, cc *store.CodeContext) string {
	if task.WorktreeBranch != nil && *task.WorktreeBranch != "" {
		return *task.WorktreeBranch
	}
	if cc != nil {
		return cc.Branch
	}
	return ""
}

// agentPresence is what a task has of an agent: one running, or a saved
// conversation that can be resumed, or neither.
type agentPresence struct {
	running *store.AgentSession
	// saved is the kind and execution of a conversation that can be resumed.
	savedKind, savedExecution string
	// notResumable says why a stopped agent of this task cannot be resumed
	// by an integration: a swarm or a workflow runs it.
	notResumable string
}

func taskAgentPresence(ctx context.Context, db *store.Store, task *store.Task) (agentPresence, error) {
	var p agentPresence
	running, err := db.GetAgentSessionByTaskID(ctx, task.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return p, err
	}
	p.running = running
	if task.SwarmWorkingDir != nil && *task.SwarmWorkingDir != "" {
		p.notResumable = "a swarm runs this task's agent"
	}
	if latest, err := db.LatestAgentSessionForTask(ctx, task.ID); err != nil {
		return p, err
	} else if latest != nil {
		if owner := restartOwner(latest); owner != "" {
			p.notResumable = owner
		}
	}
	saved, err := db.ListTaskNativeSessions(ctx, task.ID)
	if err != nil {
		return p, err
	}
	var latestSaved *store.TaskNativeSession
	for i := range saved {
		if latestSaved == nil || saved[i].UpdatedAt.After(latestSaved.UpdatedAt) {
			latestSaved = &saved[i]
		}
	}
	if latestSaved != nil {
		p.savedKind, p.savedExecution = latestSaved.AgentKind, latestSaved.ExecutionID
	}
	if p.savedKind == "" && task.ChimeraSessionID != nil && *task.ChimeraSessionID != "" {
		p.savedKind = "chimera"
	}
	return p, nil
}

// agents lists the task's agents for an owner match.
func (p agentPresence) agents() []OwnerAgent {
	if p.running != nil {
		return []OwnerAgent{{ID: p.running.ExecutionID, Kind: p.running.AgentKind, State: agentActivityState(p.running.Activity)}}
	}
	if p.savedKind != "" {
		return []OwnerAgent{{ID: p.savedExecution, Kind: p.savedKind, State: "stopped"}}
	}
	return []OwnerAgent{}
}

func agentActivityState(activity string) string {
	switch activity {
	case "working", "background":
		return "working"
	case "waiting":
		return "waiting"
	case "error":
		return "error"
	}
	return "idle"
}
