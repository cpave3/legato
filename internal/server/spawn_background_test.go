package server

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// slowCloneAgents spawns only when the test lets it, as an agent whose
// clone takes a while; it records what each spawn was asked and whether
// its context had ended by then.
type slowCloneAgents struct {
	service.AgentService
	release chan error
	mu      sync.Mutex
	spawned []string
	opts    []service.AgentSpawnOptions
	ctxDone []bool
	done    chan struct{}
}

func newSlowCloneAgents() *slowCloneAgents {
	return &slowCloneAgents{release: make(chan error), done: make(chan struct{}, 4)}
}

func (a *slowCloneAgents) CreateEphemeralTask(_ context.Context, title string) (string, error) {
	return "eph1", nil
}

func (a *slowCloneAgents) SpawnAgent(ctx context.Context, taskID string, _, _ int, opts ...service.AgentSpawnOptions) error {
	err := <-a.release
	a.mu.Lock()
	a.spawned = append(a.spawned, taskID)
	a.opts = append(a.opts, opts...)
	a.ctxDone = append(a.ctxDone, ctx.Err() != nil)
	a.mu.Unlock()
	a.done <- struct{}{}
	return err
}

func (a *slowCloneAgents) CaptureOutput(context.Context, string) (string, error) {
	return "", errors.New("not running")
}
func (a *slowCloneAgents) DefaultAdapter() string { return "claude-code" }
func (a *slowCloneAgents) GetTaskResumeSession(context.Context, string, string) (*service.TaskResumeSession, error) {
	return nil, nil
}

func (a *slowCloneAgents) finish(t *testing.T, err error) {
	t.Helper()
	a.release <- err
	select {
	case <-a.done:
	case <-time.After(2 * time.Second):
		t.Fatal("the spawn never finished")
	}
}

type preparingList []PreparingAgent

func preparing(t *testing.T, srv *Server) preparingList {
	t.Helper()
	var list preparingList
	if status := serveJSON(t, srv, http.MethodGet, "/api/preparing-agents", "", &list); status != http.StatusOK {
		t.Fatalf("GET /api/preparing-agents: %d", status)
	}
	return list
}

// waitFor polls until the list of agents being prepared is what want says.
func waitFor(t *testing.T, srv *Server, want func(preparingList) bool) preparingList {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		list := preparing(t, srv)
		if want(list) || time.Now().After(deadline) {
			return list
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// An agent in a clone is started in the background: the answer comes at
// once with its task, and it is listed as preparing until it runs.
func TestASpawnInACloneAnswersAtOnceAndRunsInTheBackground(t *testing.T) {
	agents := newSlowCloneAgents()
	srv := New(nil, agents, nil, ":0")

	var answer struct {
		TaskID    string `json:"task_id"`
		Preparing bool   `json:"preparing"`
	}
	status := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"Fix it","agent_kind":"claude-code","repo":"acme/widget","branch":"legato/x"}`, &answer)
	if status != http.StatusAccepted || answer.TaskID != "eph1" || !answer.Preparing {
		t.Fatalf("status %d, answer %+v; want 202 with the task, preparing", status, answer)
	}
	list := preparing(t, srv)
	if len(list) != 1 || list[0].TaskID != "eph1" || list[0].Title != "Fix it" || list[0].Repo != "acme/widget" || list[0].AgentKind != "claude-code" || list[0].Error != "" {
		t.Fatalf("preparing = %+v", list)
	}

	agents.finish(t, nil)
	if agents.ctxDone[0] {
		t.Fatal("the spawn's context had ended with the request: closing the page would cancel the clone")
	}
	if o := agents.opts[0]; !o.Ephemeral || o.Repo != "acme/widget" || o.Branch != "legato/x" {
		t.Fatalf("spawned with %+v", o)
	}
	if list := waitFor(t, srv, func(l preparingList) bool { return len(l) == 0 }); len(list) != 0 {
		t.Fatalf("still preparing after the agent started: %+v", list)
	}
}

func TestASpawnThatFailsInTheBackgroundStaysUntilDismissed(t *testing.T) {
	agents := newSlowCloneAgents()
	board := &mockBoardService{cardDetail: &service.CardDetail{ID: "T-1", Title: "Task one"}}
	srv := New(board, agents, nil, ":0")

	if status := serveJSON(t, srv, http.MethodPost, "/api/tasks/T-1/agent", `{"repo":"acme/widget"}`, nil); status != http.StatusAccepted {
		t.Fatalf("status %d, want 202", status)
	}
	if status := serveJSON(t, srv, http.MethodPost, "/api/tasks/T-1/agent", `{"repo":"acme/widget"}`, nil); status != http.StatusConflict {
		t.Fatalf("a second spawn while the first prepares: %d, want 409", status)
	}
	if status := serveJSON(t, srv, http.MethodDelete, "/api/preparing-agents/T-1", "", nil); status != http.StatusConflict {
		t.Fatalf("dismissing one still preparing: %d, want 409", status)
	}

	agents.finish(t, errors.New("cloning acme/widget: repository not found"))
	list := waitFor(t, srv, func(l preparingList) bool { return len(l) == 1 && l[0].Error != "" })
	if len(list) != 1 || list[0].Error != "cloning acme/widget: repository not found" || list[0].Title != "Task one" {
		t.Fatalf("after the failure: %+v", list)
	}

	if status := serveJSON(t, srv, http.MethodDelete, "/api/preparing-agents/T-1", "", nil); status != http.StatusNoContent {
		t.Fatalf("dismissing the failure: %d", status)
	}
	if list := preparing(t, srv); len(list) != 0 {
		t.Fatalf("still listed after dismissing: %+v", list)
	}
	if status := serveJSON(t, srv, http.MethodDelete, "/api/preparing-agents/T-1", "", nil); status != http.StatusNotFound {
		t.Fatalf("dismissing nothing: %d", status)
	}
}

func TestASpawnInAFolderStillWaitsForTheAgent(t *testing.T) {
	agents := &spawnRecordingAgents{}
	srv, _ := directoryServer(t, agents)
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"t","working_dir":"~/Projects/legato"}`, nil); status != http.StatusCreated {
		t.Fatalf("status = %d, want 201", status)
	}
}

func TestTheRepositoryCacheIsBroadcast(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	ch := make(chan WSMessage, 1)
	srv.hub.add(&wsClient{sendFn: func(msg WSMessage) { ch <- msg }})
	srv.BroadcastRepositoryCache(service.RepositoryCacheStatus{Repo: "acme/widget", State: service.CacheCopying, Percent: 38})
	select {
	case msg := <-ch:
		if msg.Type != MsgRepositoryCache || msg.Cache == nil || msg.Cache.Repo != "acme/widget" || msg.Cache.Percent != 38 {
			t.Fatalf("message = %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("nothing was broadcast")
	}
}
