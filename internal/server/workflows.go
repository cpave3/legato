package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func legacyWorkflowDirPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".legato", "workflows")
}

type WorkflowService interface {
	Start(context.Context, *workflowengine.Definition, map[string]string, *int) (*store.WorkflowRunView, error)
	ListRuns(context.Context, string, int) ([]store.WorkflowRun, error)
	Show(context.Context, string) (*store.WorkflowRunView, error)
	Cancel(context.Context, string, string) (*store.WorkflowRunView, error)
	Retry(context.Context, string, string) (*store.WorkflowRunView, error)
	PromptStep(context.Context, string, string) (*store.WorkflowRunView, error)
	Artifact(context.Context, string) (*store.WorkflowArtifact, error)
}

func (s *Server) workflowsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		result := workflowengine.DiscoverPaths(legacyWorkflowDirPath(), s.workflowGlobalDir, s.workflowProjectDir)
		result.Repositories = append([]workflowengine.RepositoryPreset(nil), s.workflowRepositories...)
		s.writeJSON(w, http.StatusOK, result)
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
		result := workflowengine.DiscoverPaths(legacyWorkflowDirPath(), s.workflowGlobalDir, s.workflowProjectDir)
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
func (s *Server) workflowPromptHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.workflows == nil {
			http.Error(w, "workflow service unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		view, err := s.workflows.PromptStep(r.Context(), r.PathValue("run_id"), r.PathValue("step_id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		s.NotifyWorkflowChanged(view.Run.ID, view.Run.CurrentStepID)
		s.writeJSON(w, http.StatusOK, view)
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
		artifact, err := s.workflows.Artifact(r.Context(), r.PathValue("artifact_id"))
		if err != nil {
			http.Error(w, "artifact not found", http.StatusNotFound)
			return
		}
		if artifact.MediaType == "text/markdown" || artifact.MediaType == "text/plain" {
			w.Header().Set("Content-Type", artifact.MediaType)
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", "attachment")
		}
		http.ServeFile(w, r, artifact.ManagedPath)
	}
}

func (s *Server) NotifyWorkflowChanged(runID, stepID string) {
	s.hub.Broadcast(WSMessage{Type: MsgWorkflowChanged, WorkflowRunID: runID, WorkflowStepID: stepID})
}
