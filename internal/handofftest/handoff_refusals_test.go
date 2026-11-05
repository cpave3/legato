package handofftest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// handOff hands a local task from main to its paired worker.
func handOff(t *testing.T, main *Main, workerID, taskID string, edit ...func(*service.TaskHandoffRequest)) (service.AwayTask, error) {
	t.Helper()
	req := service.TaskHandoffRequest{TaskID: taskID, Worker: workerID, Purpose: service.HandoffMakeChange, Repo: "acme/widget", AgentKind: "claude-code"}
	for _, f := range edit {
		f(&req)
	}
	return main.Handoffs.HandOff(context.Background(), req)
}

// workerCards are every card on the worker's board.
func workerCards(t *testing.T, worker *Worker) []service.Card {
	t.Helper()
	var out []service.Card
	for _, column := range []string{"Backlog", "Ready", "Doing", "Review", "Done"} {
		cards, err := worker.Board.ListCards(context.Background(), column)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, cards...)
	}
	return out
}

// stillHome checks a refused handoff left the task on the main, not away,
// and made nothing on the worker.
func stillHome(t *testing.T, main *Main, worker *Worker, taskID string) {
	t.Helper()
	if away, err := main.Handoffs.Away(context.Background(), taskID); err != nil || away != nil {
		t.Fatalf("the task is away: %+v, %v", away, err)
	}
	if err := main.Board.UpdateTaskTitle(context.Background(), taskID, "Still editable"); err != nil {
		t.Fatalf("the task cannot be edited: %v", err)
	}
	if worker.Store != nil {
		if cards := workerCards(t, worker); len(cards) != 0 {
			t.Fatalf("the worker made %+v", cards)
		}
	}
}

func TestTheWorkerRefusesWhatItCannotRunBeforeMakingAnything(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	worker.Repos.Refuse("acme/secret", "the GitHub App cannot read acme/secret")

	for _, tc := range []struct {
		name string
		edit func(*service.TaskHandoffRequest)
		says string
	}{
		{"an unknown agent tool", func(r *service.TaskHandoffRequest) { r.AgentKind = "gemini" }, `"gemini"`},
		{"a repository it cannot clone", func(r *service.TaskHandoffRequest) { r.Repo = "acme/secret" }, "the GitHub App cannot read acme/secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskID, _ := localTask(t, main, "Fix it", "", "")
			_, err := handOff(t, main, paired.InstanceID, taskID, tc.edit)
			if !errors.Is(err, service.ErrWorkerRefused) || !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("err %v", err)
			}
			stillHome(t, main, worker, taskID)
		})
	}
}

func TestAWorkerWithoutTheSandboxRefusesEveryHandoff(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{NoSandbox: true})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix it", "", "")

	// The main sees it in the worker's meta and sends nothing.
	if _, err := handOff(t, main, paired.InstanceID, taskID); !errors.Is(err, service.ErrWorkerRefused) || !strings.Contains(err.Error(), "sandbox") {
		t.Fatalf("err %v", err)
	}
	stillHome(t, main, worker, taskID)

	// The worker refuses the call itself too.
	body, _ := json.Marshal(service.HandoffCall{HandoffID: "ho-1", MainTaskID: taskID, Title: "Fix it", Notes: []service.HandoffCallNote{},
		Repo: "acme/widget", Purpose: service.HandoffMakeChange, Kind: service.HandoffNewBranch, Branch: "legato/x"})
	resp := worker.Do(http.MethodPost, service.HandoffCallPath, mainToken(t, main, paired.InstanceID), string(body))
	var refused struct{ Error, Message string }
	_ = json.NewDecoder(resp.Body).Decode(&refused)
	if resp.StatusCode != http.StatusUnprocessableEntity || refused.Error != "no_sandbox" {
		t.Fatalf("%d %+v", resp.StatusCode, refused)
	}
	stillHome(t, main, worker, taskID)
}

func TestAMainSaysToUpdateAWorkerTooOldToTakeAHandoff(t *testing.T) {
	var old atomic.Bool
	worker := StartWorker(t, WorkerOptions{Handler: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if old.Load() && r.URL.Path == "/api/meta" {
				_, _ = w.Write([]byte(`{"version":"v0.9.0","handoff_pairing":1,"handoff":1,"sandbox":true}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	old.Store(true)
	taskID, _ := localTask(t, main, "Fix it", "", "")
	if _, err := handOff(t, main, paired.InstanceID, taskID); !errors.Is(err, service.ErrWorkerTooOld) || !strings.Contains(err.Error(), "update the worker") {
		t.Fatalf("err %v", err)
	}
	stillHome(t, main, worker, taskID)
}

func TestTheMainRefusesAnUnreachableWorkerAndARunningAgent(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)

	// A task whose agent runs on the main.
	running, _ := localTask(t, main, "Being worked on here", "", "")
	if err := main.Store.InsertAgentSession(ctx, store.AgentSession{TaskID: running, TmuxSession: "legato-" + running, Command: "claude", AgentKind: "claude-code", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if _, err := handOff(t, main, paired.InstanceID, running); !errors.Is(err, service.ErrHandoffAgentRunning) || !strings.Contains(err.Error(), "stop") {
		t.Fatalf("err %v", err)
	}
	stillHome(t, main, worker, running)

	// An unreachable worker: nothing is sent.
	worker.Stop()
	taskID, _ := localTask(t, main, "Fix it", "", "")
	if _, err := handOff(t, main, paired.InstanceID, taskID); !errors.Is(err, service.ErrWorkerUnreachable) {
		t.Fatalf("err %v", err)
	}
	stillHome(t, main, worker, taskID)
	worker.Restart()
	if cards := workerCards(t, worker); len(cards) != 0 {
		t.Fatalf("the worker made %+v", cards)
	}
}

func TestRetryingAHandoffMakesNoSecondCard(t *testing.T) {
	ctx := context.Background()
	// The worker's first answer to a handoff is lost on the way back.
	var dropped atomic.Bool
	worker := StartWorker(t, WorkerOptions{Handler: func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == service.HandoffCallPath && dropped.CompareAndSwap(false, true) {
				next.ServeHTTP(httptest.NewRecorder(), r)
				panic(http.ErrAbortHandler)
			}
			next.ServeHTTP(w, r)
		})
	}})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix it", "", "")

	if _, err := handOff(t, main, paired.InstanceID, taskID); !errors.Is(err, service.ErrWorkerUnreachable) {
		t.Fatalf("the lost answer: %v", err)
	}
	if away, _ := main.Handoffs.Away(ctx, taskID); away != nil {
		t.Fatalf("away without an answer: %+v", away)
	}
	away, err := handOff(t, main, paired.InstanceID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	cards := workerCards(t, worker)
	if len(cards) != 1 || cards[0].ID != away.WorkerTaskID {
		t.Fatalf("the worker has %+v, the main thinks %s", cards, away.WorkerTaskID)
	}

	// The same call again, as the main sends it, answers with the same card;
	// the same handoff ID for another request is refused.
	token := mainToken(t, main, paired.InstanceID)
	notes := []service.HandoffCallNote{}
	call := service.HandoffCall{HandoffID: away.HandoffID, MainTaskID: taskID, Title: "Fix it", Notes: notes, Repo: "acme/widget",
		Purpose: service.HandoffMakeChange, Kind: service.HandoffNewBranch, Branch: away.Branch, AgentKind: "claude-code"}
	body, _ := json.Marshal(call)
	var again service.HandoffAccepted
	decode(t, worker.Do(http.MethodPost, service.HandoffCallPath, token, string(body)), http.StatusOK, &again)
	if again.WorkerTaskID != away.WorkerTaskID {
		t.Fatalf("again %+v", again)
	}
	call.Title = "Something else"
	body, _ = json.Marshal(call)
	if resp := worker.Do(http.MethodPost, service.HandoffCallPath, token, string(body)); resp.StatusCode != http.StatusConflict {
		t.Fatalf("another request under the same ID: %d", resp.StatusCode)
	}
	if cards := workerCards(t, worker); len(cards) != 1 {
		t.Fatalf("the worker has %+v", cards)
	}
}

func TestAStartThatFailsMakesTheHandoffFailedWithItsReason(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	worker.Repos.Fail("acme/widget", errors.New("cloning acme/widget: repository not found"))
	taskID, _ := localTask(t, main, "Fix it", "", "")

	away, err := handOff(t, main, paired.InstanceID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()
	_, got := handoffState(t, worker, mainToken(t, main, paired.InstanceID), away.HandoffID)
	if got.State != "failed" || !strings.Contains(got.Failure, "repository not found") {
		t.Fatalf("%+v", got)
	}
	if len(worker.Tmux.Spawned()) != 0 {
		t.Fatalf("an agent started: %+v", worker.Tmux.Spawned())
	}
}

func TestAPendingHandoffStartsAfterAWorkerRestart(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix it", "", "")

	// The worker accepts the handoff and stops before the agent starts.
	worker.HoldStarts(true)
	away, err := handOff(t, main, paired.InstanceID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	token := mainToken(t, main, paired.InstanceID)
	if _, got := handoffState(t, worker, token, away.HandoffID); got.State != "pending" {
		t.Fatalf("%+v", got)
	}
	worker.HoldStarts(false)
	worker.Restart()
	worker.Settle()

	if _, got := handoffState(t, worker, token, away.HandoffID); got.State != "in_progress" {
		t.Fatalf("after the restart: %+v", got)
	}
	if spawned := worker.Tmux.Spawned(); len(spawned) != 1 || spawned[0].Name != "legato-"+away.WorkerTaskID {
		t.Fatalf("spawned %+v", spawned)
	}
}
