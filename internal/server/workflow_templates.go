package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// workflowTemplate is a bundled template the Workflows page can add.
type workflowTemplate struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// workflowFolder is a folder a template can be added to: the global
// workflows folder, the project's, or a repository's checkout.
type workflowFolder struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Path  string `json:"path"`
}

func (s *Server) workflowFolders(r *http.Request) []workflowFolder {
	folders := []workflowFolder{}
	if s.workflowGlobalDir != "" {
		folders = append(folders, workflowFolder{ID: "global", Label: "Your workflows", Path: s.workflowGlobalDir})
	}
	if s.workflowProjectDir != "" {
		folders = append(folders, workflowFolder{ID: "project", Label: "This project", Path: s.workflowProjectDir})
	}
	for _, preset := range s.repositories(r.Context()) {
		if strings.TrimSpace(preset.PrimaryDir) == "" {
			continue
		}
		path := filepath.Join(preset.PrimaryDir, ".legato", "workflows")
		if path == s.workflowProjectDir {
			continue
		}
		folders = append(folders, workflowFolder{ID: "repo:" + preset.Name, Label: preset.Name, Path: path})
	}
	return folders
}

// workflowTemplatesHandler serves GET /api/workflow-templates: the bundled
// templates and the folders one can be added to.
func (s *Server) workflowTemplatesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		templates := []workflowTemplate{}
		for _, example := range workflowengine.Examples() {
			t := workflowTemplate{Name: example.Name}
			if definition, err := workflowengine.Parse(example.Name+".workflow.yaml", example.YAML); err == nil {
				t.Title, t.Description = definition.Title, strings.TrimSpace(definition.Description)
			}
			templates = append(templates, t)
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"templates": templates, "folders": s.workflowFolders(r)})
	}
}

// workflowTemplateAddHandler serves POST /api/workflow-templates/{template}
// {name?, folder?}: the template written to a folder (global by default)
// as name (its own by default), never over a workflow of that name.
func (s *Server) workflowTemplateAddHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		template := r.PathValue("template")
		if _, ok := workflowengine.ExampleNamed(template); !ok {
			s.writeError(w, http.StatusNotFound, "no template "+template)
			return
		}
		var body struct {
			Name   string `json:"name"`
			Folder string `json:"folder"`
		}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				s.writeError(w, http.StatusBadRequest, "want {\"name\": \"…\", \"folder\": \"global\"}")
				return
			}
		}
		if body.Folder == "" {
			body.Folder = "global"
		}
		dir := ""
		for _, folder := range s.workflowFolders(r) {
			if folder.ID == body.Folder {
				dir = folder.Path
			}
		}
		if dir == "" {
			s.writeError(w, http.StatusBadRequest, "no workflows folder "+body.Folder)
			return
		}
		discovered := s.discoverWorkflows()
		taken := func(name string) bool {
			for _, definition := range discovered.Definitions {
				if definition.Name == name {
					return true
				}
			}
			return false
		}
		path, err := workflowengine.AddTemplate(template, body.Name, dir, taken)
		switch {
		case errors.Is(err, workflowengine.ErrWorkflowNameTaken):
			s.writeError(w, http.StatusConflict, err.Error())
			return
		case err != nil:
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		name := body.Name
		if name == "" {
			name = template
		}
		s.hub.Broadcast(WSMessage{Type: MsgWorkflowChanged})
		s.writeJSON(w, http.StatusCreated, map[string]string{"name": name, "path": path})
	}
}

// workflowDraftHandler serves POST /api/workflow-drafts {yaml}: a drafted
// definition (the Reeve's draft_workflow proposes this call) checked,
// every trigger paused, and written to the global workflows folder, never
// over a workflow of that name.
func (s *Server) workflowDraftHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body struct {
			YAML string `json:"yaml"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || strings.TrimSpace(body.YAML) == "" {
			s.writeError(w, http.StatusBadRequest, "want {\"yaml\": \"<a workflow definition>\"}")
			return
		}
		if s.workflowGlobalDir == "" {
			s.writeError(w, http.StatusServiceUnavailable, "no workflows folder")
			return
		}
		discovered := s.discoverWorkflows()
		path, err := workflowengine.SaveDraft([]byte(body.YAML), s.workflowGlobalDir, func(name string) bool {
			for _, definition := range discovered.Definitions {
				if definition.Name == name {
					return true
				}
			}
			return false
		})
		switch {
		case errors.Is(err, workflowengine.ErrWorkflowNameTaken):
			s.writeError(w, http.StatusConflict, err.Error())
			return
		case err != nil:
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.hub.Broadcast(WSMessage{Type: MsgWorkflowChanged})
		s.writeJSON(w, http.StatusCreated, map[string]string{"path": path, "triggers": "paused"})
	}
}
