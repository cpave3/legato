package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// workflowSchemaHandler serves the workflow JSON Schema, for editors of the
// operator's own: GET only, version 2's (the version Legato writes) or with
// ?version=1 version 1's, with its schema version and an ETag of its
// content.
func (s *Server) workflowSchemaHandler() http.HandlerFunc {
	type served struct {
		schema []byte
		etag   string
	}
	schemas := map[int]served{}
	for _, version := range []int{workflowengine.SchemaVersion, workflowengine.SchemaVersion2} {
		schema := workflowengine.JSONSchemaFor(version)
		sum := sha256.Sum256(schema)
		schemas[version] = served{schema, `"` + hex.EncodeToString(sum[:8]) + `"`}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		version := workflowengine.LatestSchemaVersion
		if asked := r.URL.Query().Get("version"); asked != "" {
			version, _ = strconv.Atoi(asked)
		}
		current, ok := schemas[version]
		if !ok {
			s.writeError(w, http.StatusBadRequest, "version must be 1 or 2")
			return
		}
		schema, etag := current.schema, current.etag
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, max-age=300")
		w.Header().Set("X-Legato-Workflow-Schema-Version", strconv.Itoa(version))
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("Content-Type", "application/schema+json")
		_, _ = w.Write(schema)
	}
}
