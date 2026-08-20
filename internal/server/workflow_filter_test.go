package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

type filteringWorkflowService struct {
	forceWorkflowService
	filter store.WorkflowRunFilter
	runs   []store.WorkflowRun
}

func (f *filteringWorkflowService) ListRuns(_ context.Context, filter store.WorkflowRunFilter) ([]store.WorkflowRun, error) {
	f.filter = filter
	return f.runs, nil
}

func TestWorkflowRunsEndpointForwardsGenericFilters(t *testing.T) {
	workflows := &filteringWorkflowService{runs: []store.WorkflowRun{{ID: "wr-1"}}}
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(workflows, "", "")
	req := httptest.NewRequest(http.MethodGet, "/api/workflow-runs?status=running&workflow_name=review-pr&input.repo=cpave3%2Flegato&input.pr_number=42&limit=20", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got := workflows.filter
	if got.Status != "running" || got.WorkflowName != "review-pr" || got.Limit != 20 || got.Inputs["repo"] != "cpave3/legato" || got.Inputs["pr_number"] != "42" {
		t.Fatalf("filter=%+v", got)
	}
}

func TestWorkflowRunsEndpointReturnsAnEmptyCollectionWhenNoRunsMatch(t *testing.T) {
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(&filteringWorkflowService{}, "", "")
	req := httptest.NewRequest(http.MethodGet, "/api/workflow-runs?workflow_name=review-pr", nil)
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "[]\n" {
		t.Fatalf("body=%q, want empty JSON collection", rec.Body.String())
	}
}

func TestWorkflowRunsEndpointRejectsMalformedFilters(t *testing.T) {
	for _, query := range []string{
		"limit=0",
		"limit=101",
		"limit=nope",
		"input.=value",
		"input.bad.key=value",
		"input.repo=",
		"input.repo=one&input.repo=two",
		"status=running&status=failed",
		"workflow_name=one&workflow_name=two",
		"limit=1&limit=2",
	} {
		t.Run(query, func(t *testing.T) {
			s := New(nil, nil, nil, "")
			s.SetWorkflowService(&filteringWorkflowService{}, "", "")
			req := httptest.NewRequest(http.MethodGet, "/api/workflow-runs?"+query, nil)
			rec := httptest.NewRecorder()
			s.server.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
