package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Without tmux there is no agent service: a spawn answers with why and the
// fix, as the TUI and the serve log say it.
func TestASpawnWithAgentsOffSaysWhy(t *testing.T) {
	const reason = "tmux not found: agents are off. Install tmux (apt install tmux, brew install tmux): agents run in tmux sessions."
	srv := New(&mockBoardService{}, nil, nil, ":0")
	srv.SetAgentsOff(reason)
	for _, path := range []string{"/api/agents/spawn", "/api/tasks/t1/agent"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"title":"x"}`)))
		if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), reason) {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
}
