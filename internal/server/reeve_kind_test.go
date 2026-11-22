package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// reeveKindAgents lists a Reeve-kind agent that is not the Reeve's current
// session (a stale one) beside an ordinary agent.
type reeveKindAgents struct{ promptAgentService }

func (reeveKindAgents) ListAgents(context.Context) ([]service.AgentSession, error) {
	return []service.AgentSession{
		{TaskID: "R-old", Status: "running", Reeve: true},
		{TaskID: "T-1", Status: "running"},
	}, nil
}

// The Reeve is its task's kind, everywhere: a Reeve-kind agent is the Reeve
// on the web whether or not it is the current session.
func TestAReeveKindAgentIsTheReeveOnTheWeb(t *testing.T) {
	srv := New(nil, &reeveKindAgents{}, nil, "")
	for _, query := range []string{"", "?view=compact"} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/agents"+query, nil))
		var agents []struct {
			TaskID string `json:"task_id"`
			Reeve  bool   `json:"reeve"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &agents); err != nil {
			t.Fatal(err)
		}
		if len(agents) != 2 || !agents[0].Reeve || agents[1].Reeve {
			t.Fatalf("GET /api/agents%s = %+v", query, agents)
		}
	}
}
