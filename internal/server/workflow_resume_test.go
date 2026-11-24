package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// resumingWorkflows is a workflow service whose runs can resume their
// card's agent with a macro (LEG-614).
type resumingWorkflows struct {
	forceWorkflowService
	runID, macro string
	err          error
}

func (r *resumingWorkflows) ResumeAgent(_ context.Context, runID, macro string) (service.MacroStartResult, error) {
	r.runID, r.macro = runID, macro
	if r.err != nil {
		return service.MacroStartResult{}, r.err
	}
	return service.MacroStartResult{TaskID: "card0001", Started: true, AgentKind: "claude-code"}, nil
}

func resumeRequest(t *testing.T, workflows WorkflowService, body string) *httptest.ResponseRecorder {
	t.Helper()
	s := New(nil, nil, nil, "")
	s.SetWorkflowService(workflows, "", "")
	req := httptest.NewRequest(http.MethodPost, "/api/workflow-runs/wr-1/resume-agent", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(rec, req)
	return rec
}

func TestResumeAgentStartsTheRunsCardAgentWithAMacro(t *testing.T) {
	workflows := &resumingWorkflows{}
	rec := resumeRequest(t, workflows, `{"macro":"Rebase and repush"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var result service.MacroStartResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || !result.Started || result.TaskID != "card0001" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if workflows.runID != "wr-1" || workflows.macro != "Rebase and repush" {
		t.Fatalf("resumed %q with %q", workflows.runID, workflows.macro)
	}
}

func TestResumeAgentSaysWhyItCannot(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		body string
		want int
	}{
		"no macro":           {body: `{}`, want: http.StatusBadRequest},
		"a finished run":     {err: store.ErrWorkflowStateConflict, body: `{"macro":"x"}`, want: http.StatusConflict},
		"the agent waits":    {err: service.ErrResumeAgentWaiting, body: `{"macro":"x"}`, want: http.StatusConflict},
		"an unknown macro":   {err: service.ErrMacroNotFound, body: `{"macro":"x"}`, want: http.StatusNotFound},
		"an unknown run":     {err: store.ErrNotFound, body: `{"macro":"x"}`, want: http.StatusNotFound},
		"a folder that went": {err: service.ErrMacroFolderGone, body: `{"macro":"x"}`, want: http.StatusConflict},
	} {
		t.Run(name, func(t *testing.T) {
			if rec := resumeRequest(t, &resumingWorkflows{err: tc.err}, tc.body); rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestResumeAgentNeedsAServiceThatCanResume(t *testing.T) {
	if rec := resumeRequest(t, &forceWorkflowService{}, `{"macro":"x"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}
