package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestPlanHTTPFlowAnswersRequiredQuestionAndApproves(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Plan API", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	bundle := t.TempDir()
	if err := os.WriteFile(filepath.Join(bundle, "plan.md"), []byte("# Plan\n\nUse SQLite.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "plan.json"), []byte(`{"schema_version":1,"title":"Plan API","questions":[{"id":"db","kind":"single_choice","prompt":"Database?","required":true,"options":[{"id":"sqlite","label":"SQLite"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := service.NewPlanService(s, nil, nil)
	view, err := svc.Submit(ctx, "task-1", "", bundle)
	if err != nil {
		t.Fatal(err)
	}
	server := New(nil, nil, nil, "")
	server.SetPlanService(svc)

	request := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, req)
		return recorder
	}
	if got := request(http.MethodPost, "/api/plans/"+view.Plan.ID+"/approve", ""); got.Code != http.StatusConflict {
		t.Fatalf("approve before response = %d: %s", got.Code, got.Body.String())
	}
	if got := request(http.MethodPut, "/api/plans/"+view.Plan.ID+"/responses/db", `{"values":["sqlite"]}`); got.Code != http.StatusOK {
		t.Fatalf("response = %d: %s", got.Code, got.Body.String())
	}
	created := request(http.MethodPost, "/api/plans/"+view.Plan.ID+"/comments", `{"body":"Clarify migration","selection_start":8,"selection_end":18,"selected_text":"Use SQLite"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("comment = %d: %s", created.Code, created.Body.String())
	}
	var comment store.PlanComment
	if err := json.Unmarshal(created.Body.Bytes(), &comment); err != nil {
		t.Fatal(err)
	}
	updated := request(http.MethodPatch, "/api/plans/"+view.Plan.ID+"/comments/"+comment.ID, `{"body":"Clarify the migration path"}`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update comment = %d: %s", updated.Code, updated.Body.String())
	}
	if err := json.Unmarshal(updated.Body.Bytes(), &comment); err != nil {
		t.Fatal(err)
	}
	if comment.Body != "Clarify the migration path" || comment.SelectionStart == nil || *comment.SelectionStart != 8 {
		t.Fatalf("updated comment = %+v", comment)
	}
	if got := request(http.MethodPost, "/api/plans/"+view.Plan.ID+"/approve", ""); got.Code != http.StatusOK {
		t.Fatalf("approve = %d: %s", got.Code, got.Body.String())
	}
	got := request(http.MethodGet, "/api/plans/"+view.Plan.ID, "")
	if got.Code != http.StatusOK {
		t.Fatalf("get = %d: %s", got.Code, got.Body.String())
	}
	var result service.PlanView
	if err := json.Unmarshal(got.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Plan.Status != "approved" || len(result.Responses) != 1 || len(result.Comments) != 1 {
		t.Fatalf("result = %+v", result)
	}
}
