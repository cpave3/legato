package server

import (
	"context"
	"encoding/json"
	"errors"
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
	store  *store.Store
	svc    *service.ReviewService
	tourID string
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
	tour, err := svc.EnsureReviewTour(ctx, "task-1", "implementation")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Ready(ctx, tour.ID, "ready for review"); err != nil {
		t.Fatal(err)
	}
	server := New(nil, nil, nil, "")
	server.SetReviewService(svc)
	return &reviewServerFixture{server: server, store: s, svc: svc, tourID: tour.ID}
}

func TestReviewTourAndStepDiff(t *testing.T) {
	f := newReviewServerFixture(t)
	tourView, err := f.svc.Tour(context.Background(), f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.InsertReviewHunkNote(context.Background(), store.ReviewHunkNote{
		ID: "note-1", TaskID: "task-1", TourID: f.tourID, StepID: tourView.Steps[0].ID,
		FilePath: "api.go", HunkAnchor: "saved-anchor", Body: "Explain this hunk.",
	}); err != nil {
		t.Fatal(err)
	}

	tourReq := httptest.NewRequest(http.MethodGet, "/api/review/tours/"+f.tourID, nil)
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
	if len(tour.HunkNotes) != 1 || tour.HunkNotes[0].Body != "Explain this hunk." || tour.HunkNotes[0].HunkAnchor != "saved-anchor" {
		t.Fatalf("unexpected hunk_notes: %+v", tour.HunkNotes)
	}

	diffReq := httptest.NewRequest(http.MethodGet, "/api/review/tours/"+f.tourID+"/steps/"+tour.Steps[0].ID+"/diff", nil)
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
	hunks, ok := files[0]["hunks"].([]any)
	if !ok || len(hunks) != 1 {
		t.Fatalf("unexpected hunks: %+v", files[0]["hunks"])
	}
	hunk, ok := hunks[0].(map[string]any)
	if !ok || strings.TrimSpace(fmt.Sprint(hunk["anchor"])) == "" {
		t.Fatalf("diff hunk missing anchor: %+v", hunks[0])
	}
}

func TestDeleteReviewRemovesTourAndRetainsTask(t *testing.T) {
	f := newReviewServerFixture(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/review/tours/"+f.tourID, nil)
	res := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want %d, body = %s", res.Code, http.StatusNoContent, res.Body.String())
	}
	if res.Body.Len() != 0 {
		t.Fatalf("DELETE body = %q, want empty", res.Body.String())
	}

	if _, err := f.store.GetReviewTour(context.Background(), f.tourID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted tour err = %v, want %v", err, store.ErrNotFound)
	}

	task, err := f.store.GetTask(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("task removed with review: %v", err)
	}
	if task.ID != "task-1" {
		t.Fatalf("retained task ID = %q, want task-1", task.ID)
	}
}

func TestReviewMutationsUpdateTour(t *testing.T) {
	f := newReviewServerFixture(t)
	tour, err := f.svc.Tour(context.Background(), f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	stepID := tour.Steps[0].ID

	requests := []struct {
		path   string
		body   string
		status int
	}{
		{"/api/review/tours/" + f.tourID + "/steps/" + stepID + "/reviewed", `{"reviewed":true}`, http.StatusOK},
		{"/api/review/tours/" + f.tourID + "/steps/" + stepID + "/question", `{"text":"Why this shape?"}`, http.StatusAccepted},
		{"/api/review/tours/" + f.tourID + "/complete", ``, http.StatusOK},
	}
	for _, tc := range requests {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		res := httptest.NewRecorder()
		f.server.Handler().ServeHTTP(res, req)
		if res.Code != tc.status {
			t.Fatalf("POST %s status = %d, want %d, body = %s", tc.path, res.Code, tc.status, res.Body.String())
		}
	}

	updated, err := f.svc.Tour(context.Background(), f.tourID)
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
	tour, err := f.svc.Tour(context.Background(), f.tourID)
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
		{"wrong queue method", http.MethodPost, "/api/review/queue", "", http.StatusMethodNotAllowed},
		{"wrong review method", http.MethodPost, "/api/review/tours/" + f.tourID, "", http.StatusMethodNotAllowed},
		{"malformed JSON", http.MethodPost, "/api/review/tours/" + f.tourID + "/steps/" + stepID + "/question", `{`, http.StatusBadRequest},
		{"missing text", http.MethodPost, "/api/review/tours/" + f.tourID + "/steps/" + stepID + "/question", `{}`, http.StatusBadRequest},
		{"missing reviewed", http.MethodPost, "/api/review/tours/" + f.tourID + "/steps/" + stepID + "/reviewed", `{}`, http.StatusBadRequest},
		{"missing tour", http.MethodGet, "/api/review/tours/missing", "", http.StatusNotFound},
		{"delete missing tour", http.MethodDelete, "/api/review/tours/missing", "", http.StatusNotFound},
		{"missing step", http.MethodGet, "/api/review/tours/" + f.tourID + "/steps/missing/diff", "", http.StatusNotFound},
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
		TaskID: "task-1", TourID: "rt-123", StepID: "rs-123", Kind: "question",
	}})

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != MsgReviewChanged || msg.TaskID != "task-1" || msg.TourID != "rt-123" || msg.StepID != "rs-123" || msg.Kind != "question" {
		t.Fatalf("unexpected message: %+v", msg)
	}
}

func TestBoardIncludesAggregatedReviewBadgeTarget(t *testing.T) {
	f := newReviewServerFixture(t)
	f.server.board = &mockBoardService{
		columns: []service.Column{{Name: "Doing"}},
		cards: map[string][]service.Card{
			"Doing": {{ID: "task-1", Title: "Review API", Status: "Doing"}},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/board", nil)
	res := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	var board BoardResponse
	if err := json.NewDecoder(res.Body).Decode(&board); err != nil {
		t.Fatal(err)
	}
	card := board.Columns[0].Cards[0]
	if card.ReviewTourID != f.tourID || card.ReviewName != "implementation" || !card.ReviewReady || card.ReviewUnreviewed != 1 {
		t.Fatalf("unexpected review badge: %+v", card)
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
	if len(items) != 1 || items[0].TourID != f.tourID || items[0].TaskID != "task-1" || items[0].Name != "implementation" || items[0].Unreviewed != 1 {
		t.Fatalf("unexpected queue: %+v", items)
	}
}
