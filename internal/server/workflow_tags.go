package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// workflowTagsHandler serves PUT /api/workflows/{name}/tags {tags}: the
// workflow's tags written into its file, which keeps every other line
// (LEG-657). A file Legato may not edit, such as a built-in's, is refused.
func (s *Server) workflowTagsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body struct {
			Tags []string `json:"tags"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			s.writeError(w, http.StatusBadRequest, `want {"tags": ["…"]}`)
			return
		}
		ctx := r.Context()
		definition := s.findWorkflow(ctx, r.PathValue("name"))
		if definition == nil {
			s.writeWorkflowError(w, http.StatusNotFound, "not_found", "no workflow "+r.PathValue("name"), nil)
			return
		}
		info := workflowengine.FileFor(definition, s.workflowFolderList(ctx))
		if !info.Editable {
			s.writeWorkflowError(w, http.StatusForbidden, "not_editable", info.Reason, nil)
			return
		}
		data, err := os.ReadFile(info.Path)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		text, err := workflowengine.SetTags(string(data), body.Tags)
		if err != nil {
			s.writeWorkflowError(w, http.StatusUnprocessableEntity, "invalid", err.Error(), nil)
			return
		}
		if after, diagnostics := s.checked(ctx, text); after == nil {
			s.writeWorkflowError(w, http.StatusUnprocessableEntity, "invalid", diagnostics[0].Message, map[string]any{"diagnostics": diagnostics})
			return
		}
		if err := workflowengine.ReplaceFile(info.Path, []byte(text), workflowengine.FileDigest(data)); errors.Is(err, workflowengine.ErrStaleFile) {
			s.writeWorkflowError(w, http.StatusConflict, "stale", "the file changed while its tags were written: try again", nil)
			return
		} else if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.notifyWorkflowDefinitionsChanged()
		tags := body.Tags
		if tags == nil {
			tags = []string{}
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"name": definition.Name, "tags": tags, "digest": workflowengine.FileDigest([]byte(text))})
	}
}

// workflowStatsDays is how many days GET /api/workflow-stats counts.
const workflowStatsDays = 7

// workflowStatsHandler serves GET /api/workflow-stats?tz=<zone>: each
// workflow's newest run and its runs and failures on each of the last 7
// days, by the dates of the zone the page is viewed in (this machine's
// without tz).
func (s *Server) workflowStatsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.workflows == nil {
			s.writeError(w, http.StatusServiceUnavailable, "workflow service unavailable")
			return
		}
		loc := time.Local
		if zone := r.URL.Query().Get("tz"); zone != "" {
			var err error
			if loc, err = time.LoadLocation(zone); err != nil {
				s.writeError(w, http.StatusBadRequest, "unknown time zone "+zone)
				return
			}
		}
		stats, err := s.workflows.Stats(r.Context(), time.Now(), loc, workflowStatsDays)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, stats)
	}
}
