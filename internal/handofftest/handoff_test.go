package handofftest

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// localTask makes a local task on the main in Ready, with a note.
func localTask(t *testing.T, main *Main, title, description, note string) (string, store.TaskNote) {
	t.Helper()
	ctx := context.Background()
	card, err := main.Board.CreateTask(ctx, title, description, "Ready", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var added store.TaskNote
	if note != "" {
		if added, err = service.AddTaskNote(ctx, main.Store, card.ID, note, "agent"); err != nil {
			t.Fatal(err)
		}
	}
	return card.ID, added
}

func TestAMainHandsALocalTaskToAWorker(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, note := localTask(t, main, "Fix the flaky login test", "It fails one run in ten.", "Seen on CI since Tuesday.")

	away, err := main.Handoffs.HandOff(ctx, service.TaskHandoffRequest{
		TaskID: taskID, Worker: paired.InstanceID, Purpose: service.HandoffMakeChange, Repo: "acme/widget", AgentKind: "claude-code",
	})
	if err != nil {
		t.Fatal(err)
	}

	// The main records the task as away on the worker.
	branch := "legato/" + strings.ToLower(taskID)
	if away.WorkerInstanceID != paired.InstanceID || away.WorkerName != "vm-1" || away.HandoffID == "" || away.WorkerTaskID == "" ||
		away.Purpose != service.HandoffMakeChange || away.Kind != service.HandoffNewBranch || away.Branch != branch ||
		away.Repo != "acme/widget" || away.AgentKind != "claude-code" || away.LeftColumn != "Ready" {
		t.Fatalf("away %+v", away)
	}
	if got, err := main.Handoffs.Away(ctx, taskID); err != nil || got == nil || *got != away {
		t.Fatalf("Away = %+v, %v", got, err)
	}

	// The worker made an ordinary card in the main's workspace, in Doing,
	// with the main's note marked as from the main.
	detail, err := worker.Board.GetCard(ctx, away.WorkerTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Title != "Fix the flaky login test" || detail.DescriptionMD != "It fails one run in ten." || detail.Status != "Doing" {
		t.Fatalf("worker card %+v", detail)
	}
	if len(detail.Notes) != 1 || detail.Notes[0].Body != "Seen on CI since Tuesday." || detail.Notes[0].Source != "main" || detail.Notes[0].RemoteID != note.ID {
		t.Fatalf("worker notes %+v", detail.Notes)
	}
	cards, err := worker.Board.ListCards(ctx, "Doing")
	if err != nil || len(cards) != 1 {
		t.Fatalf("Doing on the worker: %+v, %v", cards, err)
	}
	if cards[0].WorkspaceName != "workstation" {
		t.Fatalf("the card is in workspace %q", cards[0].WorkspaceName)
	}
	if from := cards[0].HandedOffFrom; from == nil || from.MainName != "workstation" || from.MainTaskID != taskID {
		t.Fatalf("the card says it is from %+v", from)
	}
	// The worker's board API says where the card is from.
	var board struct {
		Columns []struct {
			Name  string `json:"name"`
			Cards []struct {
				ID          string `json:"id"`
				HandoffFrom *struct {
					Main       string `json:"main"`
					MainTaskID string `json:"main_task_id"`
				} `json:"handoff_from"`
			} `json:"cards"`
		} `json:"columns"`
	}
	decode(t, worker.Do(http.MethodGet, "/api/board", worker.WebToken(), ""), http.StatusOK, &board)
	found := false
	for _, column := range board.Columns {
		for _, card := range column.Cards {
			if card.ID == away.WorkerTaskID {
				found = card.HandoffFrom != nil && card.HandoffFrom.Main == "workstation" && card.HandoffFrom.MainTaskID == taskID
			}
		}
	}
	if !found {
		t.Fatalf("the board API does not say where %s is from: %+v", away.WorkerTaskID, board)
	}
}

// handoffState is a handoff as its main sees it on the worker.
func handoffState(t *testing.T, worker *Worker, token, handoffID string) (int, service.HandoffAccepted) {
	t.Helper()
	resp := worker.Do(http.MethodGet, service.HandoffCallPath+"/"+handoffID, token, "")
	var got service.HandoffAccepted
	_ = json.NewDecoder(resp.Body).Decode(&got)
	return resp.StatusCode, got
}

// eventually waits up to two seconds for ok.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTheWorkerStartsASandboxedBriefedAgentOnTheCard(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix the flaky login test", "It fails one run in ten.", "Seen on CI since Tuesday.")

	away, err := main.Handoffs.HandOff(ctx, service.TaskHandoffRequest{TaskID: taskID, Worker: paired.InstanceID, Purpose: service.HandoffMakeChange, Repo: "acme/widget", AgentKind: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()

	// The agent works in a clone of the repository on the main's branch.
	clones := worker.Repos.Clones()
	if len(clones) != 1 || clones[0].Repo != "acme/widget" || clones[0].Branch != away.Branch || clones[0].TaskID != away.WorkerTaskID {
		t.Fatalf("clones %+v", clones)
	}
	spawned := worker.Tmux.Spawned()
	session := "legato-" + away.WorkerTaskID
	if len(spawned) != 1 || spawned[0].Name != session || !strings.HasPrefix(spawned[0].WorkDir, worker.Dir) {
		_, st := handoffState(t, worker, mainToken(t, main, paired.InstanceID), away.HandoffID)
		t.Fatalf("spawned %+v (%+v)", spawned, st)
	}
	// In the sandbox, with the tool's permission prompts off.
	typed := worker.Tmux.Typed(session)
	if len(typed) == 0 || !strings.HasPrefix(typed[0], "SANDBOX") || !strings.HasSuffix(typed[0], " codex --codex-prompts-off") {
		t.Fatalf("launch %q", typed)
	}
	if s, err := worker.Store.GetAgentSessionByTaskID(ctx, away.WorkerTaskID); err != nil || !s.Sandboxed || s.AgentKind != "codex" {
		t.Fatalf("session %+v, %v", s, err)
	}
	// With the handoff's brief as its assignment.
	brief := ""
	for _, kv := range spawned[0].Env {
		if file, ok := strings.CutPrefix(kv, "LEGATO_BRIEF_FILE="); ok {
			raw, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			brief = string(raw)
		}
	}
	for _, want := range []string{"# Handed-off task", "workstation handed this task to this Legato", "Fix the flaky login test",
		"It fails one run in ten.", "### Notes from workstation", "Seen on CI since Tuesday.", "Work on " + away.Branch + "."} {
		if !strings.Contains(brief, want) {
			t.Fatalf("the brief does not say %q:\n%s", want, brief)
		}
	}

	// The agent running makes the handoff in progress; the card in Done
	// makes it done, and out of Done in progress again.
	token := mainToken(t, main, paired.InstanceID)
	if status, got := handoffState(t, worker, token, away.HandoffID); status != http.StatusOK || got.State != "in_progress" || got.WorkerTaskID != away.WorkerTaskID {
		t.Fatalf("%d %+v", status, got)
	}
	if err := worker.Board.MoveCard(ctx, away.WorkerTaskID, "Done"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "done", func() bool { _, got := handoffState(t, worker, token, away.HandoffID); return got.State == "done" })
	if err := worker.Board.MoveCard(ctx, away.WorkerTaskID, "Review"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "in progress", func() bool {
		_, got := handoffState(t, worker, token, away.HandoffID)
		return got.State == "in_progress"
	})
}

func TestAMainSeesOnlyItsOwnHandoffs(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	laptop := NewMain(t, MainOptions{Name: "laptop", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	Pair(t, laptop, worker)
	taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")
	away, err := main.Handoffs.HandOff(ctx, service.TaskHandoffRequest{TaskID: taskID, Worker: paired.InstanceID, Purpose: service.HandoffFindOut, Repo: "acme/widget"})
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := handoffState(t, worker, mainToken(t, laptop, paired.InstanceID), away.HandoffID); status != http.StatusNotFound {
		t.Fatalf("another main's handoff: %d", status)
	}
	if status, _ := handoffState(t, worker, worker.WebToken(), away.HandoffID); status != http.StatusForbidden {
		t.Fatalf("the full web token: %d", status)
	}
	// Settings › Main instances counts each main's open handoffs.
	mains, _ := worker.Tokens.ListMains(ctx)
	counts := map[string]int{}
	for _, m := range mains {
		counts[m.Name] = m.OpenHandoffs
	}
	if counts["workstation"] != 1 || counts["laptop"] != 0 {
		t.Fatalf("open handoffs %v", counts)
	}
}
