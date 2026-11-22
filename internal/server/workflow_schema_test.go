package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestTheWorkflowSchemaIsServedWithItsVersion(t *testing.T) {
	s := New(nil, nil, nil, ":0")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workflow-schema", nil))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), workflowengine.JSONSchema()) {
		t.Fatalf("status = %d, body = %.80s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/schema+json" || rec.Header().Get("X-Legato-Workflow-Schema-Version") != "1" {
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
