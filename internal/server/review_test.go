package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"nhooyr.io/websocket"
)

type reviewServerFixture struct {
	server *Server
	svc    *service.ReviewService
}

func newReviewServerFixture(t *testing.T) *reviewServerFixture {
	t.Helper()

	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-b", "main")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "initial")
	git("checkout", "-b", "feature")

	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{
		ID: "task-1", Title: "Review API", Status: "Doing",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskWorktree(ctx, "task-1", &store.TaskWorktree{
		PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "api.go"), []byte("package api\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "add API\n\nExpose the review endpoint.")

	svc := service.NewReviewService(s, nil, nil)
	if err := svc.Ready(ctx, "task-1", "ready for review"); err != nil {
		t.Fatal(err)
	}
	server := New(nil, nil, nil, "")
	server.SetReviewService(svc)
	return &reviewServerFixture{server: server, svc: svc}
}

func TestReviewTourAndStepDiff(t *testing.T) {
	f := newReviewServerFixture(t)

	tourReq := httptest.NewRequest(http.MethodGet, "/api/tasks/task-1/review", nil)
	tourRes := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(tourRes, tourReq)
	if tourRes.Code != http.StatusOK {
		t.Fatalf("tour status = %d, body = %s", tourRes.Code, tourRes.Body.String())
	}
	var tour service.ReviewTourView
	if err := json.NewDecoder(tourRes.Body).Decode(&tour); err != nil {
		t.Fatal(err)
	}
	if tour.Tour.TaskID != "task-1" || len(tour.Steps) != 1 {
		t.Fatalf("unexpected tour: %+v", tour)
	}

	diffReq := httptest.NewRequest(http.MethodGet, "/api/tasks/task-1/review/steps/"+tour.Steps[0].ID+"/diff", nil)
	diffRes := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(diffRes, diffReq)
	if diffRes.Code != http.StatusOK {
		t.Fatalf("diff status = %d, body = %s", diffRes.Code, diffRes.Body.String())
	}
	var files []map[string]any
	if err := json.NewDecoder(diffRes.Body).Decode(&files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("unexpected diff: %+v", files)
	}
}

func TestReviewMutationsUpdateTour(t *testing.T) {
	f := newReviewServerFixture(t)
	tour, err := f.svc.Tour(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	stepID := tour.Steps[0].ID

	requests := []struct {
		path   string
		body   string
		status int
	}{
		{"/api/tasks/task-1/review/steps/" + stepID + "/reviewed", `{"reviewed":true}`, http.StatusOK},
		{"/api/tasks/task-1/review/steps/" + stepID + "/question", `{"text":"Why this shape?"}`, http.StatusAccepted},
		{"/api/tasks/task-1/review/complete", ``, http.StatusOK},
	}
	for _, tc := range requests {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		res := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatalf("POST %s status = %d, want %d, body = %s", tc.path, res.Code, tc.status, res.Body.String())
		}
	}

	updated, err := f.svc.Tour(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Tour.Status != "reviewed" || updated.Steps[0].ReviewedAt == nil {
		t.Fatalf("review not completed: %+v", updated)
	}
	if len(updated.Messages) != 1 || updated.Messages[0].Body != "Why this shape?" {
		t.Fatalf("question not stored: %+v", updated.Messages)
	}
}

func TestReviewAPIRejectsInvalidRequests(t *testing.T) {
	f := newReviewServerFixture(t)
	tour, err := f.svc.Tour(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	stepID := tour.Steps[0].ID

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
	}{
		{"wrong method", http.MethodPost, "/api/review/queue", "", http.StatusMethodNotAllowed},
		{"malformed JSON", http.MethodPost, "/api/tasks/task-1/review/steps/" + stepID + "/question", `{`, http.StatusBadRequest},
		{"missing text", http.MethodPost, "/api/tasks/task-1/review/steps/" + stepID + "/question", `{}`, http.StatusBadRequest},
		{"missing reviewed", http.MethodPost, "/api/tasks/task-1/review/steps/" + stepID + "/reviewed", `{}`, http.StatusBadRequest},
		{"missing tour", http.MethodGet, "/api/tasks/missing/review", "", http.StatusNotFound},
		{"missing step", http.MethodGet, "/api/tasks/task-1/review/steps/missing/diff", "", http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			res := httptest.NewRecorder()
			f.server.Handler().ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("status = %d, want %d, body = %s", res.Code, tc.status, res.Body.String())
			}
			if res.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("content type = %q", res.Header().Get("Content-Type"))
			}
			var body map[string]string
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil || body["error"] == "" {
				t.Fatalf("expected JSON error, body = %+v, err = %v", body, err)
			}
		})
	}
}

func TestStartReviewEventsBroadcastsReviewChanged(t *testing.T) {
	bus := events.New()
	server := NewWithSwarm(nil, nil, nil, "", nil, bus, "")
	server.StartReviewEvents()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = server.Serve(ln) }()
	defer server.Stop(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://%s/ws", ln.Addr()), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	_, _, err = conn.Read(ctx) // initial agent_list
	if err != nil {
		t.Fatal(err)
	}
	bus.Publish(events.Event{Type: events.EventReviewChanged, Payload: events.ReviewChangedPayload{
		TaskID: "task-1", StepID: "rs-123", Kind: "question",
	}})

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != MsgReviewChanged || msg.TaskID != "task-1" || msg.StepID != "rs-123" || msg.Kind != "question" {
		t.Fatalf("unexpected message: %+v", msg)
	}
}

func TestReviewAPIUnavailableWithoutService(t *testing.T) {
	server := New(nil, nil, nil, "")
	req := httptest.NewRequest(http.MethodGet, "/api/review/queue", nil)
	res := httptest.NewRecorder()
	server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestReviewQueueReturnsReviewableTours(t *testing.T) {
	f := newReviewServerFixture(t)
	req := httptest.NewRequest(http.MethodGet, "/api/review/queue", nil)
	res := httptest.NewRecorder()

	f.server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var items []service.ReviewQueueItem
	if err := json.NewDecoder(res.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].TaskID != "task-1" || items[0].Unreviewed != 1 {
		t.Fatalf("unexpected queue: %+v", items)
	}
}
