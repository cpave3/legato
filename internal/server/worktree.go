package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

func (s *Server) taskWorktreeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.worktrees == nil {
			s.writeError(w, http.StatusNotImplemented, "worktree creation is not enabled")
			return
		}

		taskID := r.PathValue("id")
		detail, err := s.board.GetCard(r.Context(), taskID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.writeError(w, http.StatusNotFound, "task not found")
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if detail.Worktree != nil {
			s.writeError(w, http.StatusConflict, "task already has a worktree")
			return
		}

		var request struct {
			PrimaryDir string `json:"primary_dir"`
			Branch     string `json:"branch"`
			BaseBranch string `json:"base_branch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil && err != io.EOF {
			s.writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		request.PrimaryDir = strings.TrimSpace(request.PrimaryDir)
		request.Branch = strings.TrimSpace(request.Branch)
		request.BaseBranch = strings.TrimSpace(request.BaseBranch)
		if request.PrimaryDir == "" || request.Branch == "" || request.BaseBranch == "" {
			s.writeError(w, http.StatusBadRequest, "primary_dir, branch, and base_branch are required")
			return
		}

		created, err := s.worktrees.Create(r.Context(), taskID, request.PrimaryDir, request.Branch, request.BaseBranch)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.notifyTaskChanged(taskID)
		s.writeJSON(w, http.StatusCreated, TaskWorktreeResponse{
			PrimaryDir: created.PrimaryDir,
			Path:       created.Path,
			Branch:     created.Branch,
			BaseBranch: created.BaseBranch,
		})
	}
}
