package handofftest

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// briefOf is the brief a spawned agent was given as its assignment.
func briefOf(t *testing.T, spawned Spawn) string {
	t.Helper()
	for _, kv := range spawned.Env {
		if file, ok := strings.CutPrefix(kv, "LEGATO_BRIEF_FILE="); ok {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			return string(raw)
		}
	}
	t.Fatal("the agent was given no brief")
	return ""
}

// fromPushedBranch hands a task off to start from branch.
func fromPushedBranch(branch string) func(*service.TaskHandoffRequest) {
	return func(req *service.TaskHandoffRequest) {
		req.Kind, req.Branch = service.HandoffPushedBranch, branch
	}
}

func TestAHandoffFromAPushedBranchStartsOnGitHubsBranch(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1", GitHubRepositories: []string{"acme/*"}})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	worker.Repos.Push("acme/widget", "fix/login-timeout")
	taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")

	away, err := handOff(t, main, paired.InstanceID, taskID, fromPushedBranch("fix/login-timeout"))
	if err != nil {
		t.Fatal(err)
	}
	if away.Kind != service.HandoffPushedBranch || away.Branch != "fix/login-timeout" {
		t.Fatalf("away %+v", away)
	}
	worker.Settle()

	// The worker fetched its copy of the repository before looking for the
	// branch, and the agent's clone is on GitHub's branch, not a new one.
	if got := worker.Repos.Refreshes(); len(got) != 1 || got[0] != "acme/widget" {
		t.Fatalf("refreshes %v, want acme/widget fetched", got)
	}
	clones := worker.Repos.Clones()
	if len(clones) != 1 || clones[0].Branch != "fix/login-timeout" || !clones[0].PushedBranch {
		t.Fatalf("clones %+v", clones)
	}
	_, got := handoffState(t, worker, mainToken(t, main, paired.InstanceID), away.HandoffID)
	if got.State != "in_progress" {
		t.Fatalf("%+v", got)
	}

	// The brief says the branch has commits, to read them first and only
	// add to it.
	spawned := worker.Tmux.Spawned()
	if len(spawned) != 1 {
		t.Fatalf("spawned %+v", spawned)
	}
	brief := briefOf(t, spawned[0])
	for _, want := range []string{"Starts from: the pushed branch fix/login-timeout.",
		"Work on fix/login-timeout. It already has commits; read them against main first",
		"Never rebase, amend or force-push it; reuse its PR."} {
		if !strings.Contains(brief, want) {
			t.Fatalf("the brief does not say %q:\n%s", want, brief)
		}
	}

	// The session may push the given branch as well as its own, and its
	// guidance says so.
	grants, err := worker.Credentials.Grants(ctx)
	if err != nil || len(grants) != 1 || !strings.HasPrefix(strings.Join(grants[0].Resources, " "), "github:acme/widget@refs/heads/fix/login-timeout ") {
		t.Fatalf("grants %+v, %v", grants, err)
	}
	base := "legato/" + strings.ToLower(taskID)
	if branches, ok := service.HandoffSessionBranches(ctx, worker.Store, away.WorkerTaskID); !ok || branches.Given != "fix/login-timeout" || branches.Base != base || !branches.Granted {
		t.Fatalf("session branches %+v, %v", branches, ok)
	}
}

func TestAHandoffFromABranchNotOnGitHubFails(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix it", "", "")

	away, err := handOff(t, main, paired.InstanceID, taskID, fromPushedBranch("fix/never-pushed"))
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()

	_, got := handoffState(t, worker, mainToken(t, main, paired.InstanceID), away.HandoffID)
	if got.State != "failed" || !strings.HasPrefix(got.Failure, "branch not on GitHub") || !strings.Contains(got.Failure, "fix/never-pushed") {
		t.Fatalf("%+v", got)
	}
	if got := worker.Repos.Refreshes(); len(got) != 1 {
		t.Fatalf("refreshes %v: the worker did not fetch before deciding", got)
	}
	if clones := worker.Repos.Clones(); len(clones) != 0 {
		t.Fatalf("a clone was made: %+v", clones)
	}
	if spawned := worker.Tmux.Spawned(); len(spawned) != 0 {
		t.Fatalf("an agent started: %+v", spawned)
	}
}

func TestAHandoffFromTheDefaultBranchFails(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	worker.Repos.Push("acme/widget", "main")
	taskID, _ := localTask(t, main, "Fix it", "", "")

	away, err := handOff(t, main, paired.InstanceID, taskID, fromPushedBranch("main"))
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()

	_, got := handoffState(t, worker, mainToken(t, main, paired.InstanceID), away.HandoffID)
	if got.State != "failed" || !strings.Contains(got.Failure, "default branch") {
		t.Fatalf("%+v", got)
	}
	if spawned := worker.Tmux.Spawned(); len(spawned) != 0 {
		t.Fatalf("an agent started: %+v", spawned)
	}
}

func TestTheMainRefusesAPushedBranchWithNoName(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix it", "", "")

	for _, branch := range []string{"", "  ", "not a..branch"} {
		if _, err := handOff(t, main, paired.InstanceID, taskID, fromPushedBranch(branch)); !errors.Is(err, service.ErrHandoffInput) {
			t.Fatalf("branch %q: %v, want ErrHandoffInput", branch, err)
		}
	}
	stillHome(t, main, worker, taskID)
}

func TestTheWebBoardHandsOffFromAPushedBranch(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	worker.Repos.Push("acme/widget", "fix/login-timeout")
	taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")
	web := newMainWeb(t, main)

	// No branch named: refused in the service's words, nothing sent.
	var refused struct {
		Error string `json:"error"`
	}
	noBranch := `{"worker":"` + paired.InstanceID + `","purpose":"make-change","kind":"pushed-branch","repo":"acme/widget","agent_kind":"claude-code"}`
	if code := web.do(http.MethodPost, "/api/tasks/"+taskID+"/handoff", noBranch, &refused); code != http.StatusBadRequest || !strings.Contains(refused.Error, "name the pushed branch") {
		t.Fatalf("no branch: %d %+v", code, refused)
	}

	var answer struct {
		Away awayJSON `json:"away"`
	}
	body := `{"worker":"` + paired.InstanceID + `","purpose":"make-change","kind":"pushed-branch","branch":"fix/login-timeout","repo":"acme/widget","agent_kind":"claude-code"}`
	if code := web.do(http.MethodPost, "/api/tasks/"+taskID+"/handoff", body, &answer); code != http.StatusCreated {
		t.Fatalf("handoff: %d %+v", code, answer)
	}
	worker.Settle()
	if answer.Away.Kind != service.HandoffPushedBranch || answer.Away.Branch != "fix/login-timeout" {
		t.Fatalf("away %+v", answer.Away)
	}
	if clones := worker.Repos.Clones(); len(clones) != 1 || clones[0].Branch != "fix/login-timeout" {
		t.Fatalf("clones %+v", clones)
	}
}
