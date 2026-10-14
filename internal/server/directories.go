package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/cpave3/legato/internal/service"
)

const (
	directorySuggestionLimit    = 8
	directorySuggestionMaxLimit = 50
)

// DirectoryService answers where an agent can be started on this machine:
// the directory presets, and the directories that complete a typed path.
type DirectoryService interface {
	Presets(ctx context.Context) ([]service.DirectoryPreset, error)
	SavePreset(ctx context.Context, path, name string) (service.DirectoryPreset, error)
	Suggest(ctx context.Context, query string, limit int) (service.DirectorySuggestions, error)
	Expand(path string) string
	Display(path string) string
}

// SetDirectoryService gives the server the directory presets and path
// suggestions the spawn dialog offers.
func (s *Server) SetDirectoryService(svc DirectoryService) {
	s.directories = svc
}

type directoryDefault struct {
	Path    string `json:"path"`
	Display string `json:"display"`
}

type directoryPresetsResponse struct {
	Presets []service.DirectoryPreset `json:"presets"`
	// Default is where an agent starts when no directory is given.
	Default directoryDefault `json:"default"`
}

// directoriesHandler serves GET /api/directories?q=<typed path>[&limit=N]:
// the directories that complete a path as it is typed. It names directories
// only, never files or their content.
func (s *Server) directoriesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.directories == nil {
			s.writeError(w, http.StatusServiceUnavailable, "directory service not available")
			return
		}
		limit := directorySuggestionLimit
		if raw := r.URL.Query().Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 {
				s.writeError(w, http.StatusBadRequest, "limit must be a positive number")
				return
			}
			limit = min(parsed, directorySuggestionMaxLimit)
		}
		found, err := s.directories.Suggest(r.Context(), r.URL.Query().Get("q"), limit)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, found)
	}
}

// directoryPresetsHandler serves /api/directories/presets: GET lists the
// presets of the configuration and the saved ones, and POST {path, name?}
// saves a directory.
func (s *Server) directoryPresetsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.directories == nil {
			s.writeError(w, http.StatusServiceUnavailable, "directory service not available")
			return
		}
		switch r.Method {
		case http.MethodGet:
			presets, err := s.directories.Presets(r.Context())
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if presets == nil {
				presets = []service.DirectoryPreset{}
			}
			s.writeJSON(w, http.StatusOK, directoryPresetsResponse{
				Presets: presets,
				Default: directoryDefault{Path: s.workDir, Display: s.directories.Display(s.workDir)},
			})
		case http.MethodPost:
			var req struct {
				Path string `json:"path"`
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				s.writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			saved, err := s.directories.SavePreset(r.Context(), req.Path, req.Name)
			if err != nil {
				s.writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			s.writeJSON(w, http.StatusCreated, saved)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}
