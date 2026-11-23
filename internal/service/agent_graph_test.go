package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// graphFixture is a real store with an agent service over a mock tmux and
// the graph service over both, on a clock the test moves.
type graphFixture struct {
	relatedFixture
	graph *AgentGraphService
	now   time.Time
}

func newGraphFixture(t *testing.T) *graphFixture {
	t.Helper()
	f := &graphFixture{relatedFixture: newRelatedFixture(t), now: time.Now()}
	f.graph = NewAgentGraphService(AgentGraphOptions{
		Store: f.store, Agents: f.svc, Related: f.related,
		Now: func() time.Time { return f.now },
	})
	return f
}

func (f *graphFixture) read(t *testing.T) AgentGraph {
	t.Helper()
	graph, err := f.graph.Graph(context.Background())
	if err != nil {
		t.Fatalf("reading the agent graph: %v", err)
	}
	return graph
}

// linksOf are the graph's links of one kind.
func linksOf(graph AgentGraph, kind string) []AgentLink {
	var out []AgentLink
	for _, link := range graph.Links {
		if link.Kind == kind {
			out = append(out, link)
		}
	}
	return out
}

func TestPeerMessagesBetweenAgentsAreRecentLinksWithACount(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	for _, id := range []string{"task-a", "task-b"} {
		createTask(t, f.store, id)
		f.spawn(t, id, "")
	}
	for range 2 {
		if err := f.svc.SendPeerMessage(ctx, "task-a", "task-b", "I'm on the parser"); err != nil {
			t.Fatal(err)
		}
	}

	links := linksOf(f.read(t), AgentLinkPeerMessage)
	if len(links) != 1 {
		t.Fatalf("peer message links = %+v, want one from task-a to task-b", links)
	}
	if got := links[0]; got.From != "task-a" || got.To != "task-b" || got.Count != 2 || got.LastAt == "" {
		t.Fatalf("peer message link = %+v, want task-a → task-b, 2 messages, with the time of the last", got)
	}

	// Thirty minutes on, the messages are no longer recent.
	f.now = f.now.Add(31 * time.Minute)
	if links := linksOf(f.read(t), AgentLinkPeerMessage); len(links) != 0 {
		t.Fatalf("peer message links after 31 minutes = %+v, want none", links)
	}
}

func TestAMessageFromTheOperatorIsNotAPeerLink(t *testing.T) {
	f := newGraphFixture(t)
	createTask(t, f.store, "task-b")
	f.spawn(t, "task-b", "")
	// `legato agent message` run outside any agent has no sender.
	if err := f.svc.SendPeerMessage(context.Background(), "", "task-b", "hello"); err != nil {
		t.Fatal(err)
	}
	if links := linksOf(f.read(t), AgentLinkPeerMessage); len(links) != 0 {
		t.Fatalf("peer message links = %+v, want none for a message with no sender", links)
	}
}

func TestAReviewRunLinksItsReviewerToTheAuthorAgent(t *testing.T) {
	f := newGraphFixture(t)
	createTask(t, f.store, "LEG-423")
	f.spawn(t, "LEG-423", "")
	f.runLoop(t, "run-task", reviewRun{Repo: "cpave3/legato", PR: 210, LinkedTask: "LEG-423", AuthorTask: "LEG-423"}, 2)
	f.spawn(t, "run-task", SlotReviewer)

	links := linksOf(f.read(t), AgentLinkReviewLoop)
	if len(links) != 1 {
		t.Fatalf("review loop links = %+v, want one", links)
	}
	if got := links[0]; got.From != "run-task_reviewer" || got.To != "LEG-423" || got.Label != "pass 2" {
		t.Fatalf("review loop link = %+v, want run-task_reviewer → LEG-423 at pass 2", got)
	}
}

func TestALinkToAnAgentThatIsNotRunningIsLeftOut(t *testing.T) {
	f := newGraphFixture(t)
	// The author's task has no agent running.
	createTask(t, f.store, "LEG-423")
	f.runLoop(t, "run-task", reviewRun{Repo: "cpave3/legato", PR: 210, AuthorTask: "LEG-423"}, 1)
	f.spawn(t, "run-task", SlotReviewer)

	if links := f.read(t).Links; len(links) != 0 {
		t.Fatalf("links = %+v, want none while the author has no agent", links)
	}
}

func TestTheReeveIsLinkedToTheAgentsItWatches(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	for _, id := range []string{"reeve-1", "task-a"} {
		createTask(t, f.store, id)
		f.spawn(t, id, "")
	}
	if err := f.store.WatchForReeve(ctx, store.ReeveWatch{TaskID: "task-a", ReeveTaskID: "reeve-1"}); err != nil {
		t.Fatal(err)
	}

	links := linksOf(f.read(t), AgentLinkReeveWatch)
	if len(links) != 1 || links[0].From != "reeve-1" || links[0].To != "task-a" {
		t.Fatalf("Reeve watch links = %+v, want reeve-1 → task-a", links)
	}
}

func (f *graphFixture) spawnIn(t *testing.T, taskID, dir string) {
	t.Helper()
	createTask(t, f.store, taskID)
	if err := f.svc.SpawnAgent(context.Background(), taskID, 0, 0, AgentSpawnOptions{AgentKind: "claude-code", WorkingDir: dir}); err != nil {
		t.Fatalf("starting %s in %s: %v", taskID, dir, err)
	}
}

// gitRepo makes a Git repository and returns its root.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAgentsAreBoxedByTheRepositoryTheirWorktreeBelongsTo(t *testing.T) {
	f := newGraphFixture(t)
	repo := gitRepo(t)
	sub := filepath.Join(repo, "web")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	f.graph = NewAgentGraphService(AgentGraphOptions{
		Store: f.store, Agents: f.svc, Now: func() time.Time { return f.now },
		Inspector: CheckoutInspectFunc(func(dir string) (*store.CodeContext, error) {
			if dir != repo {
				t.Errorf("inspected %s, want the worktree root %s", dir, repo)
			}
			return &store.CodeContext{Repo: "acme/widget"}, nil
		}),
	})
	f.spawnIn(t, "task-a", repo)
	f.spawnIn(t, "task-b", sub)
	f.spawnIn(t, "task-c", t.TempDir())

	places := f.read(t).Agents
	for _, id := range []string{"task-a", "task-b"} {
		if got := places[id]; got.Repo != "acme/widget" || got.Worktree != repo {
			t.Errorf("%s's place = %+v, want acme/widget in worktree %s", id, got, repo)
		}
	}
	if got := places["task-c"]; got.Repo != "" || got.Worktree != "" {
		t.Errorf("task-c's place = %+v, want none outside Git", got)
	}
}

func TestAHandedOffAgentAndADelegatedOneAreBadged(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	f.handoffLoop(t, "task-h", 0, "")
	f.spawn(t, "task-h", "")
	createTask(t, f.store, "task-d")
	f.spawn(t, "task-d", "")
	if err := f.store.InsertDelegation(ctx, store.Delegation{ID: "dl-1", Integration: "argus", IdempotencyKey: "k1", CaseID: "CASE-9",
		Mode: "change", TaskID: "task-d", State: store.DelegationDelivered}); err != nil {
		t.Fatal(err)
	}

	places := f.read(t).Agents
	if got := places["task-h"].Badges; len(got) != 1 || got[0].Kind != AgentBadgeHandoff || got[0].Label != "from erebus" {
		t.Errorf("task-h's badges = %+v, want one: from erebus", got)
	}
	if got := places["task-d"].Badges; len(got) != 1 || got[0].Kind != AgentBadgeDelegation || got[0].Label != "Argus case" || got[0].Detail != "CASE-9" {
		t.Errorf("task-d's badges = %+v, want one: Argus case CASE-9", got)
	}
}
