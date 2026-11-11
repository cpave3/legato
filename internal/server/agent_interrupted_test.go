package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type interruptedFake struct {
	service.AgentService
	list      []service.InterruptedAgent
	resumed   string
	fresh     bool
	dismissed string
	err       error
}

func (f *interruptedFake) SweepInterruptedAgents(context.Context) (int, error) { return 0, nil }

func (f *interruptedFake) ListInterruptedAgents(context.Context) ([]service.InterruptedAgent, error) {
	return f.list, nil
}

func (f *interruptedFake) ResumeInterruptedAgent(_ context.Context, taskID string, fresh bool) error {
	if f.err != nil {
		return f.err
	}
	f.resumed, f.fresh = taskID, fresh
	return nil
}

func (f *interruptedFake) ResumeAllInterruptedAgents(context.Context) []service.InterruptedResult {
	return []service.InterruptedResult{{TaskID: "T-1"}, {TaskID: "T-2", Err: errors.New("its folder is gone")}}
}

func (f *interruptedFake) DismissInterruptedAgent(_ context.Context, taskID string) error {
	if f.err != nil {
		return f.err
	}
	f.dismissed = taskID
	return nil
}

func TestInterruptedAgentsAreListedWithWhetherTheyCanBeResumed(t *testing.T) {
	at := time.Date(2026, 11, 11, 3, 4, 5, 0, time.UTC)
	agents := &interruptedFake{list: []service.InterruptedAgent{
		{TaskID: "T-1", Title: "Fix sync", AgentKind: "claude-code", WorkingDir: "/src/app", Resources: []string{"Vault"}, Resumable: true, InterruptedAt: at},
		{TaskID: "T-2", Title: "Docs", AgentKind: "codex", CanStartNew: true, Reason: "it never reported its conversation"},
	}}
	var answer struct {
		Agents []struct {
			TaskID        string   `json:"task_id"`
			Title         string   `json:"title"`
			AgentKind     string   `json:"agent_kind"`
			WorkingDir    string   `json:"working_dir"`
			Resources     []string `json:"resources"`
			Resumable     bool     `json:"resumable"`
			CanStartNew   bool     `json:"can_start_new"`
			Reason        string   `json:"reason"`
			InterruptedAt string   `json:"interrupted_at"`
		} `json:"agents"`
	}

	status := serveJSON(t, New(nil, agents, nil, ":0"), http.MethodGet, "/api/agents/interrupted", "", &answer)

	if status != http.StatusOK || len(answer.Agents) != 2 {
		t.Fatalf("status = %d, answer %+v", status, answer)
	}
	first, second := answer.Agents[0], answer.Agents[1]
	if first.TaskID != "T-1" || first.Title != "Fix sync" || first.AgentKind != "claude-code" || first.WorkingDir != "/src/app" ||
		!first.Resumable || len(first.Resources) != 1 || first.InterruptedAt != "2026-11-11T03:04:05Z" {
		t.Fatalf("first = %+v", first)
	}
	if second.Resumable || !second.CanStartNew || second.Reason == "" {
		t.Fatalf("second = %+v", second)
	}
}

func TestResumingAnInterruptedAgent(t *testing.T) {
	agents := &interruptedFake{}
	srv := New(nil, agents, nil, ":0")

	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/T-1/resume", "", nil); status != http.StatusOK || agents.resumed != "T-1" || agents.fresh {
		t.Fatalf("status = %d, resumed %q fresh %v", status, agents.resumed, agents.fresh)
	}
	if status := serveJSON(t, srv, http.MethodPost, "/api/agents/T-2/resume", `{"fresh":true}`, nil); status != http.StatusOK || agents.resumed != "T-2" || !agents.fresh {
		t.Fatalf("status = %d, resumed %q fresh %v; want a new agent", status, agents.resumed, agents.fresh)
	}
}

func TestAnInterruptedAgentThatCannotBeResumedIsAConflict(t *testing.T) {
	agents := &interruptedFake{err: fmt.Errorf("%w: its folder /src/app is gone", service.ErrAgentNotResumable)}
	var failure struct{ Error string }

	status := serveJSON(t, New(nil, agents, nil, ":0"), http.MethodPost, "/api/agents/T-1/resume", "", &failure)

	if status != http.StatusConflict || failure.Error == "" {
		t.Fatalf("status = %d, body %+v; want 409 with the reason", status, failure)
	}
}

func TestResumingOrDismissingAnAgentThatIsNotInterruptedIsNotFound(t *testing.T) {
	agents := &interruptedFake{err: fmt.Errorf("interrupted agent of task T-9: %w", store.ErrNotFound)}
	srv := New(nil, agents, nil, ":0")

	for _, path := range []string{"/api/agents/T-9/resume", "/api/agents/T-9/dismiss"} {
		if status := serveJSON(t, srv, http.MethodPost, path, "", nil); status != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", path, status)
		}
	}
}

func TestDismissingAnInterruptedAgent(t *testing.T) {
	agents := &interruptedFake{}

	status := serveJSON(t, New(nil, agents, nil, ":0"), http.MethodPost, "/api/agents/T-1/dismiss", "", nil)

	if status != http.StatusOK || agents.dismissed != "T-1" {
		t.Fatalf("status = %d, dismissed %q", status, agents.dismissed)
	}
}

func TestResumeAllSaysHowEachWent(t *testing.T) {
	var answer struct {
		Results []struct {
			TaskID string `json:"task_id"`
			Error  string `json:"error"`
		} `json:"results"`
	}

	status := serveJSON(t, New(nil, &interruptedFake{}, nil, ":0"), http.MethodPost, "/api/agents/interrupted/resume-all", "", &answer)

	if status != http.StatusOK || len(answer.Results) != 2 || answer.Results[0].Error != "" || answer.Results[1].Error == "" {
		t.Fatalf("status = %d, answer %+v", status, answer)
	}
}
