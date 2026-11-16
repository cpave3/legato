package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// SkillService is Legato's skills, for Settings › Skills.
type SkillService interface {
	Dir() string
	List(ctx context.Context) ([]service.Skill, error)
	SetEnabled(ctx context.Context, name string, enabled bool) error
}

// SetSkillService serves /api/settings/skills.
func (s *Server) SetSkillService(svc SkillService) {
	s.skills = svc
}

// skillsHandler serves GET /api/settings/skills: the skills folder and every
// skill in it, found again at each ask, with its state and any error.
func (s *Server) skillsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.skills == nil {
			s.writeError(w, http.StatusServiceUnavailable, "skills are not available here")
			return
		}
		list, err := s.skills.List(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if list == nil {
			list = []service.Skill{}
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"dir": s.skills.Dir(), "skills": list})
	}
}

// skillHandler serves PUT /api/settings/skills/{name} {enabled}: turns a
// skill on or off for agents started from now on, and answers the skill.
func (s *Server) skillHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.skills == nil {
			s.writeError(w, http.StatusServiceUnavailable, "skills are not available here")
			return
		}
		var req struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.Enabled == nil {
			s.writeError(w, http.StatusBadRequest, "say whether the skill is on: {\"enabled\": true} or false")
			return
		}
		name := r.PathValue("name")
		if err := s.skills.SetEnabled(r.Context(), name, *req.Enabled); err != nil {
			if errors.Is(err, service.ErrSkillNotFound) {
				s.writeError(w, http.StatusNotFound, "no skill named "+name+" in the skills folder")
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		list, err := s.skills.List(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, skill := range list {
			if skill.Name == name {
				s.writeJSON(w, http.StatusOK, skill)
				return
			}
		}
		s.writeError(w, http.StatusNotFound, "no skill named "+name+" in the skills folder")
	}
}
