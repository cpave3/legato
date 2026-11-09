package server

import (
	"net/http"
	"testing"
	"time"
)

func TestRestartAsksLegatoToRestart(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	asked := make(chan struct{}, 1)
	srv.SetRestarter(func() { asked <- struct{}{} })

	rec := callUpdates(srv, http.MethodPost, "/api/restart", "")

	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /api/restart: %d %s", rec.Code, rec.Body)
	}
	select {
	case <-asked:
	case <-time.After(time.Second):
		t.Fatal("no restart was asked for")
	}
}

func TestRestartIsUnavailableWithoutARestarter(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	if rec := callUpdates(srv, http.MethodPost, "/api/restart", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /api/restart: %d", rec.Code)
	}
}

func TestRestartTakesOnlyPost(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	srv.SetRestarter(func() {})
	if rec := callUpdates(srv, http.MethodGet, "/api/restart", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/restart: %d", rec.Code)
	}
}
