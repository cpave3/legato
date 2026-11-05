package handofftest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// prMain is a main with PR tracking against gh and its worker connections
// started, paired with a worker, and a task away on it. It returns the
// main, the worker, the main's task ID and the worker's clone.
func prMain(t *testing.T, gh *FakeGitHub) (*Main, *Worker, string, service.AwayTask, string) {
	t.Helper()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Clock: worker.Clock, GitHub: gh})
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, away := awayOn(t, main, worker, paired.InstanceID, "A big change")
	eventually(t, "the connection", func() bool { return main.Connections.Connected(paired.InstanceID) })
	clone := filepath.Join(worker.Dir, "jobs", worker.Repos.Clones()[0].ExecutionID, "widget")
	return main, worker, taskID, away, clone
}

// stackOn writes a Staccato stack of branches into a worker's clone, each
// on the one before, the first on main.
func stackOn(t *testing.T, clone string, branches ...string) {
	t.Helper()
	sha := strings.Repeat("b", 40)
	var entries []string
	parent := "main"
	for _, b := range branches {
		entries = append(entries, fmt.Sprintf(`%q:{"parent":%q,"base_sha":%q}`, b, parent, sha))
		parent = b
	}
	graph := `{"version":7,"root":"main","branches":{` + strings.Join(entries, ",") + `}}`
	if err := os.MkdirAll(filepath.Join(clone, ".git", "stack"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, ".git", "stack", "graph.json"), []byte(graph), 0o644); err != nil {
		t.Fatal(err)
	}
}

// trackedPRs is a main task's tracked branches as "branch #n flags", in
// stack order.
func trackedPRs(t *testing.T, main *Main, taskID string) []string {
	t.Helper()
	branches, err := main.PRs.(service.PRBranchActions).TrackedBranches(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(branches))
	for _, b := range branches {
		item := b.Branch + " (" + b.Source + ")"
		if b.PR != nil && b.PR.PRNumber > 0 {
			item += fmt.Sprintf(" #%d", b.PR.PRNumber)
			if b.PR.IsDraft {
				item += " draft"
			}
			if b.PR.CheckStatus != "" {
				item += " " + b.PR.CheckStatus
			}
			if b.PR.ReviewDecision != "" {
				item += " " + b.PR.ReviewDecision
			}
		}
		out = append(out, item)
	}
	return out
}

func eventuallyTracked(t *testing.T, main *Main, taskID string, want []string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		got := trackedPRs(t, main, taskID)
		if reflect.DeepEqual(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("tracked branches\n got %q\nwant %q", got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTheMainTracksAnAwayTasksPRsFromTheBranchesItsWorkerPushes(t *testing.T) {
	ctx := context.Background()
	gh := NewFakeGitHub(t)
	main, _, taskID, _, clone := prMain(t, gh)
	base := "legato/" + strings.ToLower(taskID)
	gh.Set("acme/widget",
		FakePR{Number: 7, Head: base + "-api", Draft: true},
		FakePR{Number: 8, Head: base + "-ui", Base: base + "-api", Draft: true})

	// The first branch is pushed: the main finds its PR at once, with no
	// poll.
	stackOn(t, clone, base+"-api", base+"-ui")
	pushBranches(t, clone, base+"-api")
	eventuallyTracked(t, main, taskID, []string{base + "-api (api) #7 draft"})

	// The next one in the stack: found too, after the first.
	pushBranches(t, clone, base+"-ui")
	eventuallyTracked(t, main, taskID, []string{base + "-api (api) #7 draft", base + "-ui (api) #8 draft"})
	detail, err := main.Board.GetCard(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.PullRequests) != 2 || detail.PullRequests[0].PRNumber != 7 || detail.PullRequests[1].PRNumber != 8 {
		t.Fatalf("the away card's PRs %+v", detail.PullRequests)
	}

	// Drafts raise nothing in Attention.
	prAttention := service.NewPRAttentionAdapter(main.Store)
	if items, err := prAttention.Snapshot(ctx); err != nil || len(items) != 0 {
		t.Fatalf("attention for drafts: %+v, %v", items, err)
	}

	// From then on the main's normal polling follows them, away or not: the
	// top one is marked ready, fails CI and gets changes requested.
	gh.Set("acme/widget",
		FakePR{Number: 7, Head: base + "-api", Draft: true},
		FakePR{Number: 8, Head: base + "-ui", Base: base + "-api", Check: "failure", Review: "CHANGES_REQUESTED"})
	if err := main.PRs.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	if got, want := trackedPRs(t, main, taskID), []string{base + "-api (api) #7 draft", base + "-ui (api) #8 fail CHANGES_REQUESTED"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after a poll\n got %q\nwant %q", got, want)
	}
	items, err := prAttention.Snapshot(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("attention: %+v, %v", items, err)
	}
	if item := items[0]; item.TaskID != taskID || item.SignalKind != service.AttentionSignalPRMergeBlocked ||
		item.Navigation != (service.AttentionNavigation{Kind: "task", Target: taskID}) || !strings.Contains(item.Title, "#8") {
		t.Fatalf("merge blocker %+v", item)
	}

	// Return will hand the main the same branches again: nothing changes.
	added, err := main.PRs.(service.PRBranchActions).AddTaskBranches(ctx, taskID, "acme/widget", []string{base + "-api", base + "-ui"})
	if err != nil || len(added) != 0 {
		t.Fatalf("adding the returned branches: %v, %v", added, err)
	}
	if got, want := trackedPRs(t, main, taskID), []string{base + "-api (api) #7 draft", base + "-ui (api) #8 fail CHANGES_REQUESTED"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after Return's branches\n got %q\nwant %q", got, want)
	}
}

func TestASlowGitHubNeverHoldsUpTheWorkersRowsAndDiscoveryRunsOneAtATime(t *testing.T) {
	ctx := context.Background()
	gh := NewFakeGitHub(t)
	main, worker, taskID, away, clone := prMain(t, gh)
	base := "legato/" + strings.ToLower(taskID)
	gh.Set("acme/widget",
		FakePR{Number: 7, Head: base + "-api"},
		FakePR{Number: 8, Head: base + "-ui", Base: base + "-api"})
	release := gh.Hold()
	defer release()

	stackOn(t, clone, base+"-api", base+"-ui")
	pushBranches(t, clone, base+"-api")
	eventually(t, "discovery asking GitHub", func() bool { return gh.InFlight() == 1 })

	// GitHub has not answered; the worker's rows still reach the main.
	if err := worker.Agents.SetAgentActivity(ctx, away.WorkerTaskID, "working", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "working on the main", func() bool { return mainAway(t, main, taskID).View == service.AwayViewWorking })
	pushBranches(t, clone, base+"-ui")
	eventually(t, "the second branch tracked", func() bool { return len(trackedPRs(t, main, taskID)) == 2 })
	time.Sleep(100 * time.Millisecond)
	if n := gh.InFlight(); n != 1 {
		t.Fatalf("%d requests to GitHub at once for one task", n)
	}

	release()
	eventuallyTracked(t, main, taskID, []string{base + "-api (api) #7", base + "-ui (api) #8"})
	if most := gh.MostInFlight(); most != 1 {
		t.Fatalf("at most %d requests at once, want 1", most)
	}
	// The branch that came while the first discovery waited was looked for
	// once more after it, not in a discovery of its own beside it.
	if lists := gh.Lists(); lists != 2 {
		t.Fatalf("%d listings, want 2", lists)
	}
}

func TestAMainWithoutPRTrackingStillRecordsAnAwayTasksBranches(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, _ := awayOn(t, main, worker, paired.InstanceID, "Fix it")
	clone := filepath.Join(worker.Dir, "jobs", worker.Repos.Clones()[0].ExecutionID, "widget")
	branch := "legato/" + strings.ToLower(taskID)
	pushBranches(t, clone, branch)
	eventually(t, "the branch tracked", func() bool {
		rows, err := main.Store.ListTaskBranches(context.Background(), taskID)
		return err == nil && len(rows) == 1 && rows[0].Branch == branch && rows[0].Repo == "acme/widget" && rows[0].Source == service.BranchSourceAPI
	})
}
