package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// workflowSchemaHandler serves the workflow JSON Schema, for the web
// editor and builder and for editors of the operator's own: GET only,
// with its schema version and an ETag of its content.
func (s *Server) workflowSchemaHandler() http.HandlerFunc {
	schema := workflowengine.JSONSchema()
	sum := sha256.Sum256(schema)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, max-age=300")
		w.Header().Set("X-Legato-Workflow-Schema-Version", strconv.Itoa(workflowengine.SchemaVersion))
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/schema+json")
		_, _ = w.Write(schema)
	}
}
