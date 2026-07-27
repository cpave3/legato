package server

import (
	"encoding/json"
	"net/http"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func (s *Server) workflowsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.writeJSON(w, http.StatusOK, workflowengine.Discover(s.workflowGlobalDir, s.workflowProjectDir))
	}
}
func (s *Server) workflowStartHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := r.PathValue("name")
		result := workflowengine.Discover(s.workflowGlobalDir, s.workflowProjectDir)
		var definition *workflowengine.Definition
		for _, item := range result.Definitions {
			if item.Name == name {
				definition = item
				break
			}
		}
		if definition == nil {
			http.Error(w, "workflow not found", http.StatusNotFound)
			return
		}
		var body struct {
			Inputs map[string]string `json:"inputs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		view, err := s.workflows.Start(r.Context(), definition, body.Inputs, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.NotifyWorkflowChanged(view.Run.ID, view.Run.CurrentStepID)
		s.writeJSON(w, http.StatusCreated, view)
	}
}
func (s *Server) workflowRunsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		runs, err := s.workflows.ListRuns(r.Context(), r.URL.Query().Get("status"), 100)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		s.writeJSON(w, 200, runs)
	}
}
func (s *Server) workflowRunHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		view, err := s.workflows.Show(r.Context(), r.PathValue("run_id"))
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		s.writeJSON(w, 200, view)
	}
}
func (s *Server) workflowCancelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", 503)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		view, err := s.workflows.Cancel(r.Context(), r.PathValue("run_id"), "cancelled from web")
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		s.NotifyWorkflowChanged(view.Run.ID, view.Run.CurrentStepID)
		s.writeJSON(w, 200, view)
	}
}
func (s *Server) workflowRetryHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", 503)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		view, err := s.workflows.Retry(r.Context(), r.PathValue("run_id"), r.PathValue("step_id"))
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		s.NotifyWorkflowChanged(view.Run.ID, view.Run.CurrentStepID)
		s.writeJSON(w, 200, view)
	}
}
func (s *Server) workflowArtifactHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", 503)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Resolve through run views to keep managed paths private from the API.
		runs, err := s.workflows.ListRuns(r.Context(), "", 1000)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		for _, run := range runs {
			view, _ := s.workflows.Show(r.Context(), run.ID)
			if view == nil {
				continue
			}
			for _, artifact := range view.Artifacts {
				if artifact.ID == r.PathValue("artifact_id") {
					if artifact.MediaType == "text/markdown" || artifact.MediaType == "text/plain" {
						w.Header().Set("Content-Type", artifact.MediaType)
					} else {
						w.Header().Set("Content-Type", "application/octet-stream")
						w.Header().Set("Content-Disposition", "attachment")
					}
					http.ServeFile(w, r, artifact.ManagedPath)
					return
				}
			}
		}
		http.Error(w, "artifact not found", 404)
	}
}

func (s *Server) NotifyWorkflowChanged(runID, stepID string) {
	s.hub.Broadcast(WSMessage{Type: MsgWorkflowChanged, WorkflowRunID: runID, WorkflowStepID: stepID})
}
