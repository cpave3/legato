package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

func TestTheWorkflowsPageSwitchesATriggerHere(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "triage.yaml"), []byte(`schema_version: 1
name: triage
title: Triage
inputs:
  task: {type: task, required: true}
on:
  task.entered_column:
    column: Triage
    enabled: false
    inputs: {task: "${{ event.task }}"}
steps:
  - {id: stop, title: Stop, type: agent.stop, with: {task: "${{ workflow.inputs.task }}"}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(nil, nil, nil, ":0")
	s.workflowGlobalDir = dir
	workflows := service.NewWorkflowService(db, nil)
	s.SetWorkflowTriggerCoordinator(service.NewWorkflowTriggerCoordinator(db, workflows, func() workflowengine.DiscoveryResult { return workflowengine.DiscoverPaths(dir) }))
	states := func() []map[string]any {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workflows", nil))
		var body struct {
			TriggerStates map[string][]map[string]any `json:"trigger_states"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%v: %s", err, rec.Body)
		}
		return body.TriggerStates["triage"]
	}
	if got := states(); len(got) != 1 || got[0]["enabled"] != false || got[0]["file_enabled"] != false || got[0]["trigger"] != "task.entered_column" {
		t.Fatalf("states = %v", got)
	}
	put := func(path, body string) int {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, path, strings.NewReader(body)))
		return rec.Code
	}
	if code := put("/api/workflows/triage/triggers/task.entered_column", `{"enabled": true}`); code != http.StatusOK {
		t.Fatalf("switch on: %d", code)
	}
	if got := states(); got[0]["enabled"] != true || got[0]["overridden"] != true {
		t.Fatalf("after switching on: %v", got)
	}
	if code := put("/api/workflows/triage/triggers/schedule", `{"enabled": true}`); code != http.StatusNotFound {
		t.Fatalf("an undeclared trigger: %d", code)
	}
	if code := put("/api/workflows/triage/triggers/task.entered_column", `{}`); code != http.StatusBadRequest {
		t.Fatalf("no enabled: %d", code)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/workflows/triage/triggers/task.entered_column", nil))
	if rec.Code != http.StatusOK || states()[0]["overridden"] != false {
		t.Fatalf("reset: %d %v", rec.Code, states())
	}
	// The file is never rewritten.
	data, _ := os.ReadFile(filepath.Join(dir, "triage.yaml"))
	if !strings.Contains(string(data), "enabled: false") {
		t.Fatal("the definition was rewritten")
	}
}
