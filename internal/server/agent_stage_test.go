package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// stagedAgents lists agents at several stages.
type stagedAgents struct{ promptAgentService }

func (stagedAgents) ListAgents(context.Context) ([]service.AgentSession, error) {
	since := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	return []service.AgentSession{
		{TaskID: "T-1", CardTaskID: "T-1", Slot: "main", Status: "running", Stage: "reviewing", StageReason: "PR #12 ready", StageSource: "milestone", StageSince: &since},
		{TaskID: "T-2", CardTaskID: "T-2", Slot: "main", Status: "running", Stage: "finished", OpenItems: 1, OpenDoneItems: 1},
		{TaskID: "T-3", CardTaskID: "T-3", Slot: "main", Status: "running", Stage: "planning", PlanPending: true},
	}, nil
}

func getStagedAgents(t *testing.T, srv *Server, query string, out any) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/agents%s: %d %s", query, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
		t.Fatal(err)
	}
}

// Every agent carries its stage and glance, in full and compact, and
// ?stage= keeps the agents at the stages named.
func TestAgentsCarryTheirStageAndFilterByIt(t *testing.T) {
	srv := New(nil, &stagedAgents{}, nil, "")

	var full []AgentResponse
	getStagedAgents(t, srv, "", &full)
	if len(full) != 3 {
		t.Fatalf("agents = %+v", full)
	}
	first := full[0]
	if first.Stage != "reviewing" || first.StageReason != "PR #12 ready" || first.StageSource != "milestone" || first.StageSince == nil || first.Glance != "your_turn" {
		t.Fatalf("first agent's stage fields = %+v", first.AgentStageFields)
	}
	if full[1].Glance != "finished" || full[2].Glance != "needs_you" {
		t.Fatalf("glances: a done note %q, a proposed plan %q", full[1].Glance, full[2].Glance)
	}

	var compact []AgentCompact
	getStagedAgents(t, srv, "?view=compact&stage=finished,Planning", &compact)
	if len(compact) != 2 || compact[0].TaskID != "T-2" || compact[0].Glance != "finished" || compact[1].Stage != "planning" {
		t.Fatalf("compact, filtered = %+v", compact)
	}

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents?stage=done", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown stage: %d", rec.Code)
	}
}

// The operator sets an agent's stage by its address, and reads its history.
func TestTheOperatorSetsAnAgentsStage(t *testing.T) {
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "stage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.CreateTask(ctx, store.Task{ID: "T-9", Title: "T", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertAgentSession(ctx, store.AgentSession{TaskID: "T-9", Slot: "reviewer", TmuxSession: "legato-T-9_reviewer", Command: "codex", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	stages := service.NewWorkStageService(db, nil)
	srv := New(nil, &promptAgentService{}, nil, "")
	srv.SetWorkStageService(stages)

	put := func(id, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/agents/"+id+"/stage", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}
	rec := put("T-9_reviewer", `{"stage":"finished","reason":"looked fine"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	var got AgentStageResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Stage != "finished" || got.StageSource != "operator" || got.StageReason != "looked fine" {
		t.Fatalf("answered %+v", got)
	}
	if main, _ := stages.Get(ctx, service.TaskPlace("T-9", "")); main != nil {
		t.Fatalf("the reviewer's stage reached the main slot: %+v", main)
	}
	if rec := put("T-9_reviewer", `{"stage":"done"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown stage: %d", rec.Code)
	}
	if rec := put("T-404", `{"stage":"finished"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("no such agent: %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/agents/T-9_reviewer/stage-history", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var history struct {
		Events []AgentStageEvent `json:"events"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &history)
	if rec.Code != http.StatusOK || len(history.Events) != 1 || history.Events[0].Stage != "finished" || history.Events[0].Source != "operator" || history.Events[0].At == nil {
		t.Fatalf("history: %d %s", rec.Code, rec.Body.String())
	}
}

// An agent says why it needs the operator and whether it has a done note
// unread: a proposed plan (plan_pending) and its open done items.
func TestAgentsSayWhyTheyNeedTheOperatorAndWhatIsUnread(t *testing.T) {
	srv := New(nil, &stagedAgents{}, nil, "")
	var full []AgentResponse
	getStagedAgents(t, srv, "", &full)
	if full[1].OpenDoneItems != 1 || full[1].PlanPending || !full[2].PlanPending || full[0].OpenDoneItems != 0 {
		t.Fatalf("fields = %+v / %+v / %+v", full[0].AgentStageFields, full[1].AgentStageFields, full[2].AgentStageFields)
	}
	var compact []AgentCompact
	getStagedAgents(t, srv, "?view=compact", &compact)
	if compact[1].OpenDoneItems != 1 || !compact[2].PlanPending {
		t.Fatalf("compact = %+v", compact)
	}
}

// A board card carries its main agent's glance, so its strip can lead with
// it; a reviewer slot does not change it.
func TestABoardCardCarriesItsMainAgentsGlance(t *testing.T) {
	cards := []service.Card{{ID: "T-1", Title: "One"}, {ID: "T-2", Title: "Two"}, {ID: "T-3", Title: "Three"}, {ID: "T-4", Title: "No agent"}}
	board := &mockBoardService{columns: []service.Column{{Name: "Doing"}}, cards: map[string][]service.Card{"Doing": cards}}
	srv := New(board, &stagedAgents{}, nil, "")
	rec := httptest.NewRecorder()
	srv.boardHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/board", nil))
	var resp BoardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, card := range resp.Columns[0].Cards {
		got[card.ID] = card.Glance
	}
	want := map[string]string{"T-1": "your_turn", "T-2": "finished", "T-3": "needs_you", "T-4": ""}
	for id, glance := range want {
		if got[id] != glance {
			t.Fatalf("glances = %v, want %v", got, want)
		}
	}
	if strings.Contains(rec.Body.String(), `"glance":""`) {
		t.Fatalf("a card with no agent sends an empty glance: %s", rec.Body.String())
	}

	srv = New(board, &stagedSlotAgents{}, nil, "")
	rec = httptest.NewRecorder()
	srv.boardHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/board", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if glance := resp.Columns[0].Cards[0].Glance; glance != "working" {
		t.Fatalf("with a reviewer, the main agent's glance = %q", glance)
	}
}
