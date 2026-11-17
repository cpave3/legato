package handofftest

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestANewBranchStartsFromTheMainsBaseBranch(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{})
	worker.Repos.Push("acme/widget", "develop")
	main := NewMain(t, MainOptions{Clock: worker.Clock, BaseBranches: map[string]string{"acme/widget": "develop"}})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")

	away, err := handOff(t, main, paired.InstanceID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()

	clones := worker.Repos.Clones()
	if len(clones) != 1 || clones[0].Ref != "develop" || clones[0].Branch != "legato/"+strings.ToLower(taskID) {
		t.Fatalf("clones %+v; want legato/<task> made at develop", clones)
	}
	brief := briefOf(t, worker.Tmux.Spawned()[0])
	if !strings.Contains(brief, "Starts from: a new branch off develop.") || !strings.Contains(brief, "open PRs against develop") {
		t.Fatalf("the brief:\n%s", brief)
	}
	if h, err := worker.Store.OpenHandoffForTask(ctx, away.WorkerTaskID); err != nil || h.Base != "develop" {
		t.Fatalf("handoff %+v, %v", h, err)
	}
}

func TestANewBranchWithoutABaseStartsFromTheDefaultBranch(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")

	if _, err := handOff(t, main, paired.InstanceID, taskID); err != nil {
		t.Fatal(err)
	}
	worker.Settle()

	if clones := worker.Repos.Clones(); len(clones) != 1 || clones[0].Ref != "" {
		t.Fatalf("clones %+v; want no ref", clones)
	}
}

func TestABaseGitHubDoesNotHaveFailsTheHandoff(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{})
	main := NewMain(t, MainOptions{Clock: worker.Clock, BaseBranches: map[string]string{"acme/widget": "develop"}})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")

	away, err := handOff(t, main, paired.InstanceID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()

	h, err := worker.Store.OpenHandoffForTask(ctx, away.WorkerTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if h.State != store.HandoffFailed || !strings.Contains(h.Failure, "origin has no branch develop") {
		t.Fatalf("handoff %s: %q", h.State, h.Failure)
	}
	if len(worker.Tmux.Spawned()) != 0 {
		t.Fatal("an agent started")
	}
}

func TestAWorkerWithoutTheBaseFeatureIsNotSentABase(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Handler: withoutFeatures(service.HandoffFeatureBase)})
	main := NewMain(t, MainOptions{Clock: worker.Clock, BaseBranches: map[string]string{"acme/widget": "develop"}})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")

	_, err := handOff(t, main, paired.InstanceID, taskID)
	if !errors.Is(err, service.ErrWorkerTooOld) || !strings.Contains(err.Error(), "too old for base") {
		t.Fatalf("handing off with a base: %v", err)
	}
	stillHome(t, main, worker, taskID)
}
