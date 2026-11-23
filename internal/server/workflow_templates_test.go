package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
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

func TestADraftedWorkflowIsSavedPausedAndNeverOverwrites(t *testing.T) {
	s, global, _ := templateServer(t)
	draft := "schema_version: 1\nname: standup\ntitle: Standup\non:\n  schedule: {every: daily, at: \"09:00\", enabled: true}\nsteps:\n  - {id: n, title: N, type: task.note, with: {task: \"${{ workflow.task }}\", body: hi}}\n"
	body, _ := json.Marshal(map[string]string{"yaml": draft})
	post := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workflow-drafts", strings.NewReader(string(body))))
		return rec
	}
	if rec := post(); rec.Code != http.StatusCreated {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	data, _ := os.ReadFile(filepath.Join(global, "standup.workflow.yaml"))
	if !strings.Contains(string(data), "enabled: false") || strings.Contains(string(data), "enabled: true") {
		t.Fatalf("saved:\n%s", data)
	}
	if rec := post(); rec.Code != http.StatusConflict {
		t.Fatalf("again: %d", rec.Code)
	}
}

// The start page (LEG-514) opens a template in the editor as a draft, so
// the list carries each template's text.
func TestTheTemplateListCarriesEachTemplatesText(t *testing.T) {
	s, _, _ := templateServer(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workflow-templates", nil))
	var list struct {
		Templates []struct{ Name, YAML string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	for _, tpl := range list.Templates {
		example, ok := workflowengine.ExampleNamed(tpl.Name)
		if !ok || tpl.YAML != string(example.YAML) {
			t.Errorf("template %s: yaml = %q", tpl.Name, tpl.YAML)
		}
	}
	if len(list.Templates) == 0 {
		t.Fatal("no templates")
	}
}
