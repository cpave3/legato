package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNavigationSummaryRequiresGETAndConfiguredServices(t *testing.T) {
	s := New(nil, nil, nil, ":0")

	post := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/navigation-summary", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d body=%s", post.Code, post.Body.String())
	}

	get := httptest.NewRecorder()
	s.server.Handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/navigation-summary", nil))
	if get.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET status=%d body=%s", get.Code, get.Body.String())
	}
}
