package handofftest

import (
	"errors"
	"strings"
	"testing"
)

// A handoff can start long after the person last pushed, so the worker
// fetches its copy of the repository first whatever the cache's max age says:
// the new branch starts from GitHub's default branch as it is now.
func TestANewBranchHandoffFetchesTheRepositoryBeforeItsClone(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1", GitHubRepositories: []string{"acme/*"}})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")

	away, err := handOff(t, main, paired.InstanceID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()

	if got := worker.Repos.Refreshes(); len(got) != 1 || got[0] != "acme/widget" {
		t.Fatalf("refreshes %v, want acme/widget fetched once", got)
	}
	if clones := worker.Repos.Clones(); len(clones) != 1 {
		t.Fatalf("clones %+v", clones)
	}
	_, got := handoffState(t, worker, mainToken(t, main, paired.InstanceID), away.HandoffID)
	if got.State != "in_progress" {
		t.Fatalf("%+v", got)
	}
}

// A fetch that fails fails the handoff with its reason, before any clone: the
// agent never starts from a copy that may be stale.
func TestAHandoffWhoseFetchFailsFailsWithTheReason(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1", GitHubRepositories: []string{"acme/*"}})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")
	worker.Repos.Fail("acme/widget", errors.New("fetching acme/widget: connection reset"))

	away, err := handOff(t, main, paired.InstanceID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()

	_, got := handoffState(t, worker, mainToken(t, main, paired.InstanceID), away.HandoffID)
	if got.State != "failed" || !strings.Contains(got.Failure, "connection reset") {
		t.Fatalf("%+v, want failed with the fetch's reason", got)
	}
	if clones := worker.Repos.Clones(); len(clones) != 0 {
		t.Fatalf("cloned %+v after a failed fetch", clones)
	}
}
