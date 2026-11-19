package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// A spawn whose agent tool cannot start answers with the check's reason
// and fix, which the spawn dialog shows.
func TestASpawnWhoseToolIsMissingSaysWhyAndHowToFixIt(t *testing.T) {
	refused := &service.AgentToolUnavailableError{Check: service.DoctorCheck{ID: "codex", Title: "Codex", Result: service.DoctorMissing,
		Detail: "codex not found on Legato's PATH", Fix: "Install Codex: npm install -g @openai/codex"}}
	task := &taskAgentFake{spawnErr: refused}
	rec := postTaskAgent(t, task, `{"agent_kind":"codex"}`)
	if rec.Code != http.StatusFailedDependency || !strings.Contains(rec.Body.String(), "npm install -g @openai/codex") {
		t.Fatalf("task agent: %d %s", rec.Code, rec.Body)
	}
}
