package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// Watching is a read: the Reeve's own token watches, lists and unwatches
// without an approval.
func TestTheReeveWatchesAnAgentWithItsOwnToken(t *testing.T) {
	srv, db := reeveServer(t)
	ctx := context.Background()
	srv.SetReeveWatches(service.NewReeveWatcher(service.ReeveWatcherOptions{Store: db}))
	rec := callAs(srv, "secret", http.MethodPost, "/api/reeve/sessions", `{}`)
	var started store.ReeveSession
	json.Unmarshal(rec.Body.Bytes(), &started)
	row, _ := db.GetReeveSession(ctx, started.TaskID)
	if err := db.CreateTask(ctx, store.Task{ID: "T-1", Title: "Docker in the sandbox", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}

	rec = callAs(srv, row.APIToken, http.MethodPost, "/api/reeve/watches", `{"task_id": "T-1", "events": ["waiting", "exited"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("watch: %d %s", rec.Code, rec.Body)
	}
	rec = callAs(srv, row.APIToken, http.MethodGet, "/api/reeve/watches", "")
	var list struct {
		Watches []service.ReeveWatchInfo `json:"watches"`
		Events  []string                 `json:"events"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list.Watches) != 1 ||
		list.Watches[0].TaskID != "T-1" || list.Watches[0].ReeveTaskID != started.TaskID ||
		strings.Join(list.Watches[0].Events, ",") != "waiting,exited" || len(list.Events) != len(service.ReeveWatchEvents) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, row.APIToken, http.MethodDelete, "/api/reeve/watches/T-1", ""); rec.Code != http.StatusOK {
		t.Fatalf("unwatch: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, row.APIToken, http.MethodDelete, "/api/reeve/watches/T-1", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unwatch again: %d %s", rec.Code, rec.Body)
	}
}

func TestWatchingRefusesWhatItCannotWatch(t *testing.T) {
	srv, db := reeveServer(t)
	srv.SetReeveWatches(service.NewReeveWatcher(service.ReeveWatcherOptions{Store: db}))
	db.CreateTask(context.Background(), store.Task{ID: "T-1", Title: "t", Status: "Doing"})
	if rec := callAs(srv, "secret", http.MethodPost, "/api/reeve/watches", `{"task_id": "T-404"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("no such task: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodPost, "/api/reeve/watches", `{"task_id": "T-1", "events": ["lunch"]}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown event: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodPost, "/api/reeve/watches", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("no task: %d %s", rec.Code, rec.Body)
	}
}

func TestTheWatchRoutesAreReads(t *testing.T) {
	for _, tc := range [][2]string{{http.MethodGet, "/api/reeve/watches"}, {http.MethodPost, "/api/reeve/watches"}, {http.MethodDelete, "/api/reeve/watches/T-1"}} {
		req, _ := http.NewRequest(tc[0], tc[1], nil)
		if got := ClassifyRequest(req); got.Class != ClassRead || !got.Known {
			t.Errorf("%s %s = %+v, want a known read", tc[0], tc[1], got)
		}
	}
}
