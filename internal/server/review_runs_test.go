package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

type fakeReviewRuns struct {
	runID, stepID string
	err           error
}

func (f *fakeReviewRuns) AnotherPass(_ context.Context, runID, stepID string) error {
	f.runID, f.stepID = runID, stepID
	return f.err
}

type showingWorkflows struct{ forceWorkflowService }

func (showingWorkflows) Show(_ context.Context, runID string) (*store.WorkflowRunView, error) {
	return &store.WorkflowRunView{Run: store.WorkflowRun{ID: runID, CurrentStepID: "review"}}, nil
}

func TestRunAnotherPassReachesTheRunsReviewStep(t *testing.T) {
	runs := &fakeReviewRuns{}
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(&showingWorkflows{}, "", "")
	s.SetReviewRuns(runs)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflow-runs/wr-1/steps/review/another-pass", nil))
	if rec.Code != http.StatusOK || runs.runID != "wr-1" || runs.stepID != "review" {
		t.Fatalf("status=%d body=%s runs=%+v", rec.Code, rec.Body.String(), runs)
	}

	runs.err = errors.New("the review loop is not waiting for its author")
	rec = httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflow-runs/wr-1/steps/review/another-pass", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("refused: status=%d", rec.Code)
	}
}

func TestRunAnotherPassWithoutReviewLoopsIsUnavailable(t *testing.T) {
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(&showingWorkflows{}, "", "")
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflow-runs/wr-1/steps/review/another-pass", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
}
