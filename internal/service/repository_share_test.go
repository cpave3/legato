package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

func (f *repoFixture) task(t *testing.T, id string) {
	t.Helper()
	if err := f.store.CreateTask(context.Background(), store.Task{ID: id, Title: id, Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
}

func (f *repoFixture) codeContext(t *testing.T, taskID string) *store.CodeContext {
	t.Helper()
	task, err := f.store.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	cc, _ := store.ParseCodeContext(task.CodeContext)
	if cc == nil {
		t.Fatalf("%s has no code context", taskID)
	}
	return cc
}

func TestASecondTaskStacksABranchInTheFirstsClone(t *testing.T) {
	f, first := jobFixture(t)
	gitIn(t, first.Path, "switch", "-q", "-c", "legato/task1")
	gitIn(t, first.Path, "commit", "-q", "--allow-empty", "-m", "part one")
	partOne := gitIn(t, first.Path, "rev-parse", "HEAD")
	f.end(t, "ex-abc")
	f.task(t, "task2")

	joined, err := f.svc.JobClone(context.Background(), JobCloneRequest{ExecutionID: "ex-two", TaskID: "task2", CloneID: first.ID, Branch: "legato/task2"})
	if err != nil {
		t.Fatalf("joining the clone: %v", err)
	}
	if joined.Path != first.Path || joined.Branch != "legato/task2" {
		t.Fatalf("joined = %+v, want %s on legato/task2", joined, first.Path)
	}
	if got := gitIn(t, first.Path, "rev-parse", "HEAD"); got != partOne {
		t.Fatalf("legato/task2 starts at %s, want part one's %s", got, partOne)
	}
	cc := f.codeContext(t, "task2")
	if cc.Repo != "acme/widget" || cc.Branch != "legato/task2" || cc.BaseBranch != "legato/task1" || cc.WorkingDir != first.Path {
		t.Fatalf("task2's code context = %+v, want legato/task2 on legato/task1 in the clone", cc)
	}
	clone, _ := f.store.JobCloneByID(context.Background(), first.ID)
	if len(clone.TaskIDs) != 2 || clone.TaskIDs[1] != "task2" {
		t.Fatalf("the clone's tasks = %v", clone.TaskIDs)
	}
}

func TestATaskJoiningWithNoBranchStaysOnTheClonesBranch(t *testing.T) {
	f, first := jobFixture(t)
	gitIn(t, first.Path, "switch", "-q", "-c", "legato/task1")
	f.task(t, "task2")
	joined, err := f.svc.JobClone(context.Background(), JobCloneRequest{ExecutionID: "ex-two", TaskID: "task2", CloneID: first.ID})
	if err != nil {
		t.Fatal(err)
	}
	if joined.Branch != "legato/task1" || f.codeContext(t, "task2").Branch != "legato/task1" {
		t.Fatalf("joined on %q", joined.Branch)
	}
}

func TestANewBranchWaitsForTheAgentWorkingInTheClone(t *testing.T) {
	f, first := jobFixture(t) // ex-abc is running in it
	f.task(t, "task2")
	ctx := context.Background()
	_, err := f.svc.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-two", TaskID: "task2", CloneID: first.ID, Branch: "legato/task2"})
	if !errors.Is(err, ErrCloneBusy) {
		t.Fatalf("a new branch under a working agent = %v, want ErrCloneBusy", err)
	}
	if _, err := f.svc.JobClone(ctx, JobCloneRequest{ExecutionID: "ex-two", TaskID: "task2", CloneID: first.ID}); err != nil {
		t.Fatalf("joining on the clone's branch beside a working agent: %v", err)
	}
}

func TestJoiningRefusesAnotherRepositorysClone(t *testing.T) {
	f, first := jobFixture(t)
	f.task(t, "task2")
	if _, err := f.svc.JobClone(context.Background(), JobCloneRequest{ExecutionID: "ex-two", TaskID: "task2", CloneID: first.ID, Repo: "acme/other"}); err == nil {
		t.Fatal("joined a clone of acme/widget for acme/other")
	}
	if _, err := f.svc.JobClone(context.Background(), JobCloneRequest{ExecutionID: "ex-two", TaskID: "task2", CloneID: 999}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("joining no clone = %v", err)
	}
}

func TestATasksNextAgentReusesItsClone(t *testing.T) {
	f, first := jobFixture(t)
	if err := f.store.TouchJobClone(context.Background(), first.ID, *f.now); err != nil {
		t.Fatal(err)
	}
	gitIn(t, first.Path, "switch", "-q", "-c", "legato/task1")
	f.end(t, "ex-abc")
	*f.now = f.now.Add(time.Minute)
	again, err := f.svc.JobClone(context.Background(), JobCloneRequest{ExecutionID: "ex-again", TaskID: "task1", Repo: "acme/widget", FreshBranch: "legato/other"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Path != first.Path || again.Branch != "legato/task1" {
		t.Fatalf("the task's next agent got %+v, want its clone on legato/task1", again)
	}
	if live, _ := f.store.ListLiveJobClones(context.Background()); len(live) != 1 {
		t.Fatalf("%d clones, want the one reused", len(live))
	}
}

func TestATaskWhoseCloneWasDeletedGetsAFreshOne(t *testing.T) {
	f, first := jobFixture(t)
	f.end(t, "ex-abc")
	if err := os.RemoveAll(first.Path); err != nil {
		t.Fatal(err)
	}
	again, err := f.svc.JobClone(context.Background(), JobCloneRequest{ExecutionID: "ex-again", TaskID: "task1", Repo: "acme/widget", FreshBranch: "legato/task1"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Path == first.Path || again.Branch != "legato/task1" || !exists(again.Path) {
		t.Fatalf("got %+v, want a fresh clone on legato/task1", again)
	}
}

func TestSweepKeepsAnIdleCloneForKeepIdle(t *testing.T) {
	f, clone := jobFixture(t)
	f.svc.opts.KeepIdle = 7 * 24 * time.Hour
	*f.now = f.now.Add(3 * 24 * time.Hour)
	f.sweep(t) // the agent is still working: the clone was used now
	f.end(t, "ex-abc")

	*f.now = f.now.Add(6 * 24 * time.Hour)
	f.sweep(t)
	if !exists(clone.Path) {
		t.Fatal("removed six days after its agent was last seen, with keep_idle seven")
	}
	*f.now = f.now.Add(2 * 24 * time.Hour)
	f.sweep(t)
	if exists(clone.Path) {
		t.Fatal("still there eight days after its agent was last seen")
	}
}

func TestSweepNeverRemovesAnIdleCloneKeptForever(t *testing.T) {
	f, clone := jobFixture(t)
	f.svc.opts.KeepIdle = KeepIdleForever
	f.end(t, "ex-abc")
	*f.now = f.now.Add(90 * 24 * time.Hour)
	f.sweep(t)
	if !exists(clone.Path) {
		t.Fatal("an idle clone kept forever was removed")
	}
}

func TestListJobClonesSaysWhatEachHolds(t *testing.T) {
	f, clone := jobFixture(t)
	gitIn(t, clone.Path, "switch", "-q", "-c", "legato/task1")
	gitIn(t, clone.Path, "commit", "-q", "--allow-empty", "-m", "unpushed")
	list, err := f.svc.ListJobClones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
	got := list[0]
	if got.ID != clone.ID || got.Repo != "acme/widget" || got.Path != clone.Path || got.Branch != "legato/task1" ||
		!got.Busy || got.Unpushed != 1 || len(got.TaskIDs) != 1 || got.TaskIDs[0] != "task1" {
		t.Fatalf("listed %+v", got)
	}
}

func TestRemoveJobCloneWaitsForItsAgentThenRemovesEverything(t *testing.T) {
	f, clone := jobFixture(t)
	gitIn(t, clone.Path, "commit", "-q", "--allow-empty", "-m", "unpushed")
	ctx := context.Background()
	if err := f.svc.RemoveJobClone(ctx, clone.ID); !errors.Is(err, ErrCloneBusy) {
		t.Fatalf("removing a clone an agent works in = %v, want ErrCloneBusy", err)
	}
	f.end(t, "ex-abc")
	if err := f.svc.RemoveJobClone(ctx, clone.ID); err != nil {
		t.Fatal(err)
	}
	if exists(clone.Path) {
		t.Fatal("the clone is still there")
	}
	if err := f.svc.RemoveJobClone(ctx, clone.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("removing it again = %v", err)
	}
}
