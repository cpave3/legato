package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// A task's notes, read alone: every note's full body, source and time,
// oldest first, and nothing else of the task.
func TestATasksNotesAreReadAloneInFull(t *testing.T) {
	long := strings.Repeat("a long note. ", 300)
	svc := &mockBoardService{
		cardDetail: &service.CardDetail{
			ID: "task-1", Title: "Docker", DescriptionMD: "the description", LocalDescriptionMD: "the description",
			Notes: []store.TaskNote{
				{ID: "tn-1", TaskID: "task-1", Body: "started", Source: "agent", CreatedAt: "2026-11-12 03:00:00"},
				{ID: "tn-2", TaskID: "task-1", Body: long, Source: "workflow", CreatedAt: "2026-11-12 04:00:00"},
			},
		},
	}
	srv := New(svc, nil, nil, ":0")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/tasks/task-1/notes", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "the description") {
		t.Fatalf("the notes hold the description: %s", w.Body)
	}
	var got []store.TaskNote
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Body != "started" || got[1].Body != long || got[1].Source != "workflow" || got[1].CreatedAt != "2026-11-12 04:00:00" {
		t.Fatalf("notes = %+v", got)
	}

	svc.cardDetail = &service.CardDetail{ID: "task-2"}
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/tasks/task-2/notes", nil))
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("no notes = %d %s", w.Code, w.Body)
	}

	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/tasks/task-1/notes", strings.NewReader(`{}`)))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d", w.Code)
	}
}

// A task's plans, newest first; each one's text is read by its ID.
func TestATasksPlansAreListedAndEachReadByID(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "LEG-160", Title: "Seatbelt", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	plan := store.Plan{ID: "pl-LEG-160-macos-seatbelt-sandbox", TaskID: "LEG-160", Name: "macos-seatbelt-sandbox", Title: "Seatbelt", Status: "proposed", LatestRevision: 1}
	if err := db.InsertPlanRevision(ctx, plan, store.PlanRevision{ID: "rev-1", PlanID: plan.ID, Revision: 1, Markdown: "# Use sandbox-exec", ManifestJSON: "{}"}, nil); err != nil {
		t.Fatal(err)
	}
	srv := New(nil, nil, nil, "")
	srv.SetPlanService(service.NewPlanService(db, nil, nil))
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	w := get("/api/tasks/LEG-160/plans")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d %s", w.Code, w.Body)
	}
	var list []service.PlanQueueItem
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].PlanID != plan.ID || list[0].Status != "proposed" || list[0].Revision != 1 {
		t.Fatalf("plans = %+v", list)
	}

	w = get("/api/plans/" + list[0].PlanID)
	if w.Code != http.StatusOK {
		t.Fatalf("plan = %d %s", w.Code, w.Body)
	}
	var view service.PlanView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Revision.Markdown != "# Use sandbox-exec" || view.Plan.Status != "proposed" || view.Plan.LatestRevision != 1 {
		t.Fatalf("view = %+v", view)
	}

	if w := get("/api/tasks/LEG-404/plans"); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "task not found") {
		t.Fatalf("unknown task = %d %s", w.Code, w.Body)
	}
}

// Both reads are classed read, so the Reeve and any read-only token may
// call them.
func TestTheTaskReadsAreClassedRead(t *testing.T) {
	for _, path := range []string{"/api/tasks/LEG-160/notes", "/api/tasks/LEG-160/plans", "/api/plans/pl-1"} {
		info := ClassifyRequest(httptest.NewRequest(http.MethodGet, path, nil))
		if info.Class != ClassRead || !info.Known {
			t.Errorf("GET %s = %+v", path, info)
		}
	}
}
