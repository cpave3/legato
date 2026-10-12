package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

var testPNG = []byte("\x89PNG\r\n\x1a\nthe board")

// newDocumentImageServer is a server with a plan service and a review
// service that keep image files, over one store with one task.
func newDocumentImageServer(t *testing.T) (*Server, *store.Store, *service.PlanService, *service.ReviewService) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "documents.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.CreateTask(context.Background(), store.Task{ID: "task1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	planImages := filepath.Join(t.TempDir(), "plan-images")
	plans := service.NewPlanService(db, nil, nil)
	plans.SetImageDir(planImages)
	reviews := service.NewReviewService(db, nil, nil)
	reviews.SetImageDirs(filepath.Join(t.TempDir(), "review-images"), planImages)
	srv := NewWithSwarm(nil, nil, nil, "", nil, nil, "")
	srv.SetPlanService(plans)
	srv.SetReviewService(reviews)
	return srv, db, plans, reviews
}

// submitTestPlan submits a plan whose Markdown shows one image of its
// bundle, and returns the plan as the API reads it back.
func submitTestPlan(t *testing.T, srv *Server, plans *service.PlanService) service.PlanView {
	t.Helper()
	bundle := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bundle, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{
		"assets/board.png": testPNG,
		"plan.md":          []byte("# Plan\n\n![The board](assets/board.png)\n"),
		"plan.json":        []byte(`{"schema_version":1,"title":"Plan"}`),
	} {
		if err := os.WriteFile(filepath.Join(bundle, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	submitted, err := plans.Submit(context.Background(), "task1", "shots", bundle)
	if err != nil {
		t.Fatal(err)
	}
	read := agentUIRequest(srv, http.MethodGet, "/api/plans/"+submitted.Plan.ID)
	if read.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", read.Code, read.Body.String())
	}
	var view service.PlanView
	if err := json.Unmarshal(read.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func TestPlanImageIsServedFromTheAddressThePlanNames(t *testing.T) {
	srv, _, plans, _ := newDocumentImageServer(t)
	view := submitTestPlan(t, srv, plans)
	if len(view.Images) != 1 || view.Images[0].Name != "assets/board.png" || !strings.HasPrefix(view.Images[0].URL, "/plan-images/") {
		t.Fatalf("the plan's images = %#v", view.Images)
	}
	// An <img> sends no token, and the exhibits that come later are drawn
	// in a frame that must hold none: the image is served without one.
	srv.SetAuthToken("secret")

	got := agentUIRequest(srv, http.MethodGet, view.Images[0].URL)
	if got.Code != http.StatusOK || got.Body.String() != string(testPNG) {
		t.Fatalf("image status=%d body=%q", got.Code, got.Body.String())
	}
	header := got.Header()
	if header.Get("Content-Type") != "image/png" || header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("type headers = %v", header)
	}
	if cache := header.Get("Cache-Control"); !strings.Contains(cache, "immutable") || !strings.Contains(cache, "private") {
		t.Fatalf("Cache-Control = %q", cache)
	}
	if policy := header.Get("Content-Security-Policy"); !strings.Contains(policy, "default-src 'none'") || !strings.Contains(policy, "sandbox") {
		t.Fatalf("Content-Security-Policy = %q", policy)
	}
}

func TestPlanImageRouteGivesOutNothingElse(t *testing.T) {
	srv, _, plans, _ := newDocumentImageServer(t)
	view := submitTestPlan(t, srv, plans)
	address := view.Images[0].URL
	folder, file := filepath.Base(filepath.Dir(address)), filepath.Base(address)
	for name, path := range map[string]string{
		"another plan":             "/plan-images/pl-other/" + file,
		"a name that is no hash":   "/plan-images/" + folder + "/board.png",
		"an unknown hash":          "/plan-images/" + folder + "/" + strings.Repeat("0", 64) + ".png",
		"a path out of the folder": "/plan-images/" + folder + "/..%2F..%2Fdocuments.db",
		"the folder itself":        "/plan-images/" + folder + "/",
		// A plan's image is not a review's.
		"the review route": "/review-images/" + folder + "/" + file,
	} {
		if got := agentUIRequest(srv, http.MethodGet, path); got.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, got.Code)
		}
	}
	if got := agentUIRequest(srv, http.MethodPost, address); got.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", got.Code)
	}
	// The API beside it still wants the token.
	srv.SetAuthToken("secret")
	if got := agentUIRequest(srv, http.MethodGet, "/api/plans/"+view.Plan.ID); got.Code != http.StatusUnauthorized {
		t.Fatalf("the plan without a token = %d", got.Code)
	}
}

func TestReviewImageIsServedFromTheAddressTheTourNames(t *testing.T) {
	srv, db, _, reviews := newDocumentImageServer(t)
	ctx := context.Background()
	repo := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "initial"}, {"checkout", "-b", "feature"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "add a"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := db.SetTaskWorktree(ctx, "task1", &store.TaskWorktree{PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	tour, err := db.EnsureReviewTour(ctx, "task1", "")
	if err != nil {
		t.Fatal(err)
	}
	shot := filepath.Join(t.TempDir(), "after.png")
	if err := os.WriteFile(shot, testPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	chapterID, err := reviews.CreateChapter(ctx, tour.ID, service.ChapterArgs{
		Title: "Header", Narration: "![After](" + shot + ")",
		Includes: []service.ChapterInclude{{FilePath: "a.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}

	read := agentUIRequest(srv, http.MethodGet, "/api/review/tours/"+tour.ID)
	if read.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", read.Code, read.Body.String())
	}
	var view service.ReviewTourView
	if err := json.Unmarshal(read.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	images := view.StepImages[chapterID]
	if len(images) != 1 || images[0].Name != shot || !strings.HasPrefix(images[0].URL, "/review-images/") {
		t.Fatalf("the chapter's images = %#v", images)
	}
	srv.SetAuthToken("secret")
	got := agentUIRequest(srv, http.MethodGet, images[0].URL)
	if got.Code != http.StatusOK || got.Body.String() != string(testPNG) || got.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("image status=%d type=%q body=%q", got.Code, got.Header().Get("Content-Type"), got.Body.String())
	}
	// A review's image is not a plan's.
	if got := agentUIRequest(srv, http.MethodGet, strings.Replace(images[0].URL, "/review-images/", "/plan-images/", 1)); got.Code != http.StatusNotFound {
		t.Fatalf("the plan route = %d, want 404", got.Code)
	}
}
