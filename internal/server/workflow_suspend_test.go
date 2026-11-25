package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// resumingWorkflowService resumes a suspended step's agent.
type resumingWorkflowService struct {
	forceWorkflowService
	resumedRun, resumedStep string
	err                     error
}

func (f *resumingWorkflowService) ResumeStep(_ context.Context, runID, stepID string) (*store.WorkflowRunView, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.resumedRun, f.resumedStep = runID, stepID
	return &store.WorkflowRunView{Run: store.WorkflowRun{ID: runID, Status: "running", CurrentStepID: stepID}}, nil
}

func TestTheRunPageResumesASuspendedStep(t *testing.T) {
	workflows := &resumingWorkflowService{}
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(workflows, "", "")

	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflow-runs/wr-1/steps/build/resume", nil))

	if rec.Code != http.StatusOK || workflows.resumedRun != "wr-1" || workflows.resumedStep != "build" {
		t.Fatalf("status %d body %s; resumed %s/%s", rec.Code, rec.Body.String(), workflows.resumedRun, workflows.resumedStep)
	}
}

func TestResumingAStepWithNothingToResumeIsAConflict(t *testing.T) {
	workflows := &resumingWorkflowService{err: store.ErrWorkflowStateConflict}
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(workflows, "", "")

	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflow-runs/wr-1/steps/build/resume", nil))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d; want 409", rec.Code)
	}
}

func TestAnInterruptedWorkflowAgentIsListedWithItsStep(t *testing.T) {
	agents := &interruptedFake{list: []service.InterruptedAgent{
		{TaskID: "LEG-559", AgentKind: "claude-code", Resumable: true, Workflow: &service.InterruptedWorkflow{RunID: "wr-1", StepID: "build", RunTitle: "Ship v1"}},
	}}
	var answer struct {
		Agents []struct {
			Workflow *struct {
				RunID    string `json:"run_id"`
				StepID   string `json:"step_id"`
				RunTitle string `json:"run_title"`
			} `json:"workflow"`
		} `json:"agents"`
	}

	status := serveJSON(t, New(nil, agents, nil, ":0"), http.MethodGet, "/api/agents/interrupted", "", &answer)

	if status != http.StatusOK || len(answer.Agents) != 1 || answer.Agents[0].Workflow == nil {
		t.Fatalf("status %d, answer %+v", status, answer)
	}
	if got := *answer.Agents[0].Workflow; got.RunID != "wr-1" || got.StepID != "build" || got.RunTitle != "Ship v1" {
		t.Fatalf("workflow = %+v", got)
	}
}
