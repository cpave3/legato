package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func getReviewEngine(t *testing.T, srv *Server) service.ReviewEngineStatus {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/review-engine", nil)
	req.Header.Set("Authorization", "Bearer secret")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/review-engine: %d %s", rec.Code, rec.Body)
	}
	var status service.ReviewEngineStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	return status
}

// The card menu asks whether some Legato on the data folder runs the review
// engine, as it opens, and says why not when none does.
func TestTheServerSaysWhetherTheReviewEngineRuns(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	running := false
	srv.SetReviewEngine(func() service.ReviewEngineStatus { return service.ReviewEngineStatusOf(true, running) })
	if got := getReviewEngine(t, srv); got.Running || got.Reason != service.ReviewEngineStoppedReason {
		t.Fatalf("no holder = %+v", got)
	}
	running = true
	if got := getReviewEngine(t, srv); !got.Running || got.Reason != "" {
		t.Fatalf("held = %+v", got)
	}
}

// A server not told of the engine says none runs.
func TestAServerNotToldOfTheReviewEngineSaysNoneRuns(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	if got := getReviewEngine(t, srv); got.Running || got.Reason != service.ReviewEngineStoppedReason {
		t.Fatalf("unwired = %+v", got)
	}
}
