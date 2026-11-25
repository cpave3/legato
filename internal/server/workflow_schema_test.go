package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// The schema of the version Legato writes, 2 (LEG-484), unless ?version=1
// asks for version 1's.
func TestTheWorkflowSchemaIsServedWithItsVersion(t *testing.T) {
	s := New(nil, nil, nil, ":0")
	for query, version := range map[string]int{"": 2, "?version=2": 2, "?version=1": 1} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workflow-schema"+query, nil))
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), workflowengine.JSONSchemaFor(version)) || rec.Header().Get("X-Legato-Workflow-Schema-Version") != strconv.Itoa(version) {
			t.Fatalf("%q: status = %d, version %s, body = %.80s", query, rec.Code, rec.Header().Get("X-Legato-Workflow-Schema-Version"), rec.Body.String())
		}
	}
	bad := httptest.NewRecorder()
	s.Handler().ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/api/workflow-schema?version=3", nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("version 3: status = %d", bad.Code)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workflow-schema", nil))
	if rec.Header().Get("Content-Type") != "application/schema+json" {
		t.Fatalf("headers = %v", rec.Header())
	}
	etag := rec.Header().Get("ETag")
	if etag == "" || rec.Header().Get("Cache-Control") == "" {
		t.Fatalf("headers = %v", rec.Header())
	}

	again := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/workflow-schema", nil)
	request.Header.Set("If-None-Match", etag)
	s.Handler().ServeHTTP(again, request)
	if again.Code != http.StatusNotModified {
		t.Fatalf("with its ETag: status = %d", again.Code)
	}

	post := httptest.NewRecorder()
	s.Handler().ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/workflow-schema", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status = %d", post.Code)
	}
}
