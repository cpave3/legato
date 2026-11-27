package server

import (
	"context"
	"github.com/cpave3/legato/internal/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpawnEndpointsCarryExplicitClaudeAccount(t *testing.T) {
	taskAgents := &reportingAgents{}
	srv := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}, taskAgents, nil, ":0")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/agent", strings.NewReader(`{"agent_kind":"claude-code","account":"work","session_policy":"new"}`)))
	if rec.Code != http.StatusCreated || taskAgents.opts.Account != "work" {
		t.Fatalf("task status %d account %q: %s", rec.Code, taskAgents.opts.Account, rec.Body.String())
	}
	ephemeral := &spawnRecordingAgents{}
	srv, _ = directoryServer(t, ephemeral)
	if code := serveJSON(t, srv, http.MethodPost, "/api/agents/spawn", `{"title":"fixture","account":"work"}`, nil); code != http.StatusCreated {
		t.Fatalf("ephemeral status %d", code)
	}
	if len(ephemeral.spawned) != 1 || ephemeral.spawned[0].Account != "work" {
		t.Fatalf("ephemeral account lost: %+v", ephemeral.spawned)
	}
}

func TestAgentListExposesAccountLabelWithoutLocalAuthPaths(t *testing.T) {
	srv := agentsServer()
	agents := srv.agents.(*manyAgents).list
	agents[0].AgentKind = "claude-code"
	agents[0].Account = "work"
	agents[0].AccountLabel = "Work subscription"
	got := getAgents(t, srv, "?task=T-1,T-2")
	if got[0]["account"] != "work" || got[0]["account_label"] != "Work subscription" || got[0]["agent_kind"] != "claude-code" {
		t.Fatalf("selected account lost: %+v", got[0])
	}
	for _, key := range []string{"account_identity", "config_dir", "resolved_dir"} {
		if _, ok := got[0][key]; ok {
			t.Fatalf("agent list exposed local auth identity field %s", key)
		}
	}
	if _, ok := got[1]["account_label"]; ok {
		t.Fatal("unnamed default acquired a named account label")
	}
}

type accountHandoffHTTP struct{ got service.TaskHandoffRequest }

func (h *accountHandoffHTTP) HandOff(_ context.Context, req service.TaskHandoffRequest) (service.AwayTask, error) {
	h.got = req
	return service.AwayTask{}, nil
}
func TestAccountSelectionReachesMacroAndHandoffServices(t *testing.T) {
	srv := New(&mockBoardService{cardDetail: &service.CardDetail{ID: "T-1"}}, nil, nil, ":0")
	macro := &fakeMacroStarter{result: service.MacroStartResult{TaskID: "T-1", Started: true, AgentKind: "claude-code"}}
	srv.SetMacroService(macro)
	if rec := postMacro(t, srv, "T-1", `{"name":"Build","account":"work"}`); rec.Code != http.StatusCreated || macro.got.Account != "work" {
		t.Fatalf("macro account lost: %d %s %+v", rec.Code, rec.Body, macro.got)
	}
	handoff := &accountHandoffHTTP{}
	srv.SetMainHandoffs(handoff)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/T-1/handoff", strings.NewReader(`{"worker":"fixture-worker","purpose":"make-change","repo":"acme/widget","account":"work"}`)))
	if rec.Code != http.StatusCreated || handoff.got.Account != "work" {
		t.Fatalf("handoff account lost: %d %s %+v", rec.Code, rec.Body, handoff.got)
	}
}
