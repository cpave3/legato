package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func templateServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	global, project := filepath.Join(t.TempDir(), "global"), filepath.Join(t.TempDir(), "project", ".legato", "workflows")
	s := New(nil, nil, nil, ":0")
	s.workflowGlobalDir, s.workflowProjectDir = global, project
	return s, global, project
}

func addTemplate(s *Server, template, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflow-templates/"+template, strings.NewReader(body)))
	return rec
}

func TestTheWorkflowsPageAddsATemplateToTheGlobalOrProjectFolder(t *testing.T) {
	s, global, project := templateServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workflow-templates", nil))
	var list struct {
		Templates []struct{ Name, Title string }
		Folders   []struct{ ID, Path string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	names := map[string]bool{}
	for _, tpl := range list.Templates {
		names[tpl.Name] = true
	}
	if !names["triage"] || !names["review-pr"] || len(list.Folders) != 2 || list.Folders[0].ID != "global" || list.Folders[1].ID != "project" {
		t.Fatalf("list = %s", rec.Body)
	}

	rec = addTemplate(s, "triage", `{}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add to global: %d %s", rec.Code, rec.Body)
	}
	var added struct{ Name, Path string }
	_ = json.Unmarshal(rec.Body.Bytes(), &added)
	if added.Path != filepath.Join(global, "triage.workflow.yaml") {
		t.Fatalf("added = %+v", added)
	}
	before, _ := os.ReadFile(added.Path)

	// The name is taken now, in any folder.
	if rec := addTemplate(s, "triage", `{"folder":"project"}`); rec.Code != http.StatusConflict {
		t.Fatalf("a taken name: %d %s", rec.Code, rec.Body)
	}
	rec = addTemplate(s, "triage", `{"folder":"project","name":"triage-here"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add to project: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(project, "triage-here.workflow.yaml")); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(global, "triage.workflow.yaml"))
	if string(before) != string(after) {
		t.Fatal("an existing workflow was overwritten")
	}
	if rec := addTemplate(s, "triage", `{"folder":"elsewhere","name":"x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown folder: %d", rec.Code)
	}
	if rec := addTemplate(s, "nope", `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown template: %d", rec.Code)
	}
}
