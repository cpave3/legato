package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// actingWorkflowService takes step actions: a gate's choice, a paused
// step's retry, skip or stop.
type actingWorkflowService struct {
	forceWorkflowService
	run, step, action, text string
	err                     error
}

func (f *actingWorkflowService) StepAction(_ context.Context, runID, stepID, action, text string) (*store.WorkflowRunView, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.run, f.step, f.action, f.text = runID, stepID, action, text
	return &store.WorkflowRunView{Run: store.WorkflowRun{ID: runID, Status: "running", CurrentStepID: "next"}}, nil
}

func TestTheRunPageTakesAStepAction(t *testing.T) {
	workflows := &actingWorkflowService{}
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(workflows, "", "")

	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflow-runs/wr-1/steps/ask/action", strings.NewReader(`{"action":"choose:Ship"}`)))

	if rec.Code != http.StatusOK || workflows.run != "wr-1" || workflows.step != "ask" || workflows.action != "choose:Ship" {
		t.Fatalf("status %d body %s; took %+v", rec.Code, rec.Body.String(), workflows)
	}
}

func TestAStepActionTheStepDoesNotTakeIsAConflict(t *testing.T) {
	workflows := &actingWorkflowService{err: store.ErrWorkflowStateConflict}
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(workflows, "", "")

	for _, body := range []string{`{"action":"skip"}`, `{}`} {
		rec := httptest.NewRecorder()
		s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflow-runs/wr-1/steps/ask/action", strings.NewReader(body)))
		if rec.Code != http.StatusConflict && rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d", body, rec.Code)
		}
	}
}
