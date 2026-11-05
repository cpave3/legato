package handofftest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// mainWeb is the main's web API, as its web UI calls it.
type mainWeb struct {
	t   *testing.T
	srv *server.Server
}

const mainWebToken = "main-web-token"

func newMainWeb(t *testing.T, main *Main) *mainWeb {
	t.Helper()
	agents := service.NewAgentService(main.Store, NewFakeTmux(), main.Dir)
	srv := server.New(main.Board, agents, NewFakeTmux(), "")
	srv.SetAuthToken(mainWebToken)
	srv.SetWorkers(main.Workers)
	srv.SetMainHandoffs(main.Handoffs)
	return &mainWeb{t: t, srv: srv}
}

// do calls the main's web API with its full web token and decodes the JSON
// answer into into (nil to drop it).
func (m *mainWeb) do(method, path, body string, into any) int {
	m.t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+mainWebToken)
	rec := httptest.NewRecorder()
	m.srv.Handler().ServeHTTP(rec, req)
	if into != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
			m.t.Fatalf("%s %s: %d %s: %v", method, path, rec.Code, rec.Body.String(), err)
		}
	}
	return rec.Code
}

type boardJSON struct {
	Columns []struct {
		Name  string `json:"name"`
		Cards []struct {
			ID   string           `json:"id"`
			Away *json.RawMessage `json:"away"`
		} `json:"cards"`
	} `json:"columns"`
	Workers []struct {
		InstanceID string `json:"instance_id"`
		Name       string `json:"name"`
		Address    string `json:"address"`
	} `json:"workers"`
}

type awayJSON struct {
	WorkerInstanceID string `json:"worker_instance_id"`
	Worker           string `json:"worker"`
	HandoffID        string `json:"handoff_id"`
	WorkerTaskID     string `json:"worker_task_id"`
	Purpose          string `json:"purpose"`
	Kind             string `json:"kind"`
	Repo             string `json:"repo"`
	Branch           string `json:"branch"`
	AgentKind        string `json:"agent_kind"`
	LeftColumn       string `json:"left_column"`
	HandedOffAt      string `json:"handed_off_at"`
	State            string `json:"state"`
	URL              string `json:"url"`
}

func (b boardJSON) away(t *testing.T, column, taskID string) *awayJSON {
	t.Helper()
	for _, c := range b.Columns {
		if c.Name != column {
			continue
		}
		for _, card := range c.Cards {
			if card.ID != taskID {
				continue
			}
			if card.Away == nil {
				return nil
			}
			var a awayJSON
			if err := json.Unmarshal(*card.Away, &a); err != nil {
				t.Fatal(err)
			}
			return &a
		}
	}
	t.Fatalf("no card %s in %s: %+v", taskID, column, b)
	return nil
}

func TestAMainHandsOffFromItsWebBoardAndTheBoardShowsTheTaskAway(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix the flaky login test", "It fails one run in ten.", "")
	web := newMainWeb(t, main)

	// Before: the board lists the paired worker, so the Away lane shows,
	// and the task is at home.
	var before boardJSON
	if code := web.do(http.MethodGet, "/api/board", "", &before); code != http.StatusOK {
		t.Fatalf("board: %d", code)
	}
	if len(before.Workers) != 1 || before.Workers[0].Name != "vm-1" || before.Workers[0].InstanceID != paired.InstanceID || before.Workers[0].Address != worker.URL {
		t.Fatalf("workers %+v", before.Workers)
	}
	if before.away(t, "Ready", taskID) != nil {
		t.Fatal("the task is away before it was handed off")
	}

	// A refreshed board follows the handoff, as for any other card change.
	refreshed := main.Bus.Subscribe(events.EventCardsRefreshed)
	defer main.Bus.Unsubscribe(refreshed)

	var answer struct {
		Away awayJSON `json:"away"`
	}
	body := `{"worker":"` + paired.InstanceID + `","purpose":"make-change","kind":"new-branch","repo":"acme/widget","agent_kind":"claude-code"}`
	if code := web.do(http.MethodPost, "/api/tasks/"+taskID+"/handoff", body, &answer); code != http.StatusCreated {
		t.Fatalf("handoff: %d %+v", code, answer)
	}
	worker.Settle()
	if answer.Away.Worker != "vm-1" || answer.Away.WorkerTaskID == "" {
		t.Fatalf("answer %+v", answer)
	}
	select {
	case <-refreshed:
	case <-time.After(2 * time.Second):
		t.Fatal("the board was not told to refresh")
	}

	var after boardJSON
	web.do(http.MethodGet, "/api/board", "", &after)
	away := after.away(t, "Ready", taskID)
	if away == nil {
		t.Fatal("the board does not show the task away")
	}
	want := awayJSON{
		WorkerInstanceID: paired.InstanceID, Worker: "vm-1", HandoffID: answer.Away.HandoffID, WorkerTaskID: answer.Away.WorkerTaskID,
		Purpose: service.HandoffMakeChange, Kind: service.HandoffNewBranch, Repo: "acme/widget", Branch: "legato/" + strings.ToLower(taskID),
		AgentKind: "claude-code", LeftColumn: "Ready", HandedOffAt: "2026-11-05T09:00:00Z",
		// The state the worker accepted it in, until summary rows bring news.
		State: store.HandoffPending,
		URL:   worker.URL + "/board?highlight_task=" + answer.Away.WorkerTaskID,
	}
	if *away != want {
		t.Fatalf("away\n got %+v\nwant %+v", *away, want)
	}
}

func TestTheWebHandoffRouteSaysWhyAHandoffWasRefused(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	bare := StartWorker(t, WorkerOptions{Name: "vm-2", NoSandbox: true})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	unsandboxed := Pair(t, main, bare)
	web := newMainWeb(t, main)

	running, _ := localTask(t, main, "Being worked on here", "", "")
	if err := main.Store.InsertAgentSession(ctx, store.AgentSession{TaskID: running, TmuxSession: "legato-" + running, Command: "claude", AgentKind: "claude-code", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	away, _ := localTask(t, main, "Already away", "", "")
	if _, err := handOff(t, main, paired.InstanceID, away); err != nil {
		t.Fatal(err)
	}
	worker.Settle()

	handOffBody := func(workerID, purpose, agent string) string {
		return `{"worker":"` + workerID + `","purpose":"` + purpose + `","repo":"acme/widget","agent_kind":"` + agent + `"}`
	}
	for _, tc := range []struct {
		name   string
		task   func() string
		body   string
		status int
		says   string
	}{
		{"an agent running on the main", func() string { return running }, handOffBody(paired.InstanceID, "make-change", "claude-code"), http.StatusConflict, "stop task " + running + "'s agent"},
		{"a task already away", func() string { return away }, handOffBody(paired.InstanceID, "make-change", "claude-code"), http.StatusConflict, "away on vm-1"},
		{"an agent tool the worker does not know", nil, handOffBody(paired.InstanceID, "make-change", "gemini"), http.StatusUnprocessableEntity, `does not know the agent tool "gemini"`},
		{"a worker without the sandbox", nil, handOffBody(unsandboxed.InstanceID, "make-change", "claude-code"), http.StatusUnprocessableEntity, "vm-2 cannot run agents in Legato's sandbox"},
		{"no purpose", nil, handOffBody(paired.InstanceID, "", "claude-code"), http.StatusBadRequest, "the purpose is make-change or find-out"},
		{"a worker never paired", nil, handOffBody("srv-0000000000000000", "find-out", "claude-code"), http.StatusBadRequest, "not paired"},
		{"no such task", func() string { return "nope1234" }, handOffBody(paired.InstanceID, "find-out", "claude-code"), http.StatusNotFound, "no task nope1234"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			taskID := ""
			if tc.task != nil {
				taskID = tc.task()
			} else {
				taskID, _ = localTask(t, main, "Fix it", "", "")
			}
			var refused struct {
				Error string `json:"error"`
			}
			if code := web.do(http.MethodPost, "/api/tasks/"+taskID+"/handoff", tc.body, &refused); code != tc.status || !strings.Contains(refused.Error, tc.says) {
				t.Fatalf("%d %q, want %d saying %q", code, refused.Error, tc.status, tc.says)
			}
		})
	}

	// An unreachable worker: nothing is sent, and the form says so.
	worker.Stop()
	taskID, _ := localTask(t, main, "Fix it", "", "")
	var refused struct {
		Error string `json:"error"`
	}
	if code := web.do(http.MethodPost, "/api/tasks/"+taskID+"/handoff", handOffBody(paired.InstanceID, "find-out", "claude-code"), &refused); code != http.StatusBadGateway || !strings.Contains(refused.Error, "vm-1 did not answer") {
		t.Fatalf("%d %q", code, refused.Error)
	}
}

func TestABoardWithoutWorkersListsNone(t *testing.T) {
	main := NewMain(t, MainOptions{})
	web := newMainWeb(t, main)
	var raw map[string]json.RawMessage
	web.do(http.MethodGet, "/api/board", "", &raw)
	if string(raw["workers"]) != "[]" {
		t.Fatalf("workers %s", raw["workers"])
	}
}
