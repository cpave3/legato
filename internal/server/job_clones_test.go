package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type fakeJobClones struct {
	clones  []service.JobCloneInfo
	removed []int64
	err     error
}

func (f *fakeJobClones) ListJobClones(context.Context) ([]service.JobCloneInfo, error) {
	return f.clones, nil
}

func (f *fakeJobClones) RemoveJobClone(_ context.Context, id int64) error {
	if f.err != nil {
		return f.err
	}
	f.removed = append(f.removed, id)
	return nil
}

func TestJobClonesAreListedAndRemoved(t *testing.T) {
	clones := &fakeJobClones{clones: []service.JobCloneInfo{{ID: 3, Repo: "acme/widget", Branch: "stack/two", TaskIDs: []string{"t1", "t2"}, Busy: true}}}
	srv := New(nil, nil, nil, ":0")
	srv.SetJobCloneService(clones)

	var listed struct {
		Clones []service.JobCloneInfo `json:"clones"`
	}
	if status := serveJSON(t, srv, http.MethodGet, "/api/job-clones", "", &listed); status != http.StatusOK || len(listed.Clones) != 1 || listed.Clones[0].Branch != "stack/two" || !listed.Clones[0].Busy {
		t.Fatalf("status %d listed %+v", status, listed)
	}
	if status := serveJSON(t, srv, http.MethodDelete, "/api/job-clones/3", "", nil); status != http.StatusNoContent || len(clones.removed) != 1 || clones.removed[0] != 3 {
		t.Fatalf("status %d removed %v", status, clones.removed)
	}
	clones.err = service.ErrCloneBusy
	if status := serveJSON(t, srv, http.MethodDelete, "/api/job-clones/3", "", nil); status != http.StatusConflict {
		t.Fatalf("removing a clone an agent works in: status %d", status)
	}
	clones.err = store.ErrNotFound
	if status := serveJSON(t, srv, http.MethodDelete, "/api/job-clones/9", "", nil); status != http.StatusNotFound {
		t.Fatalf("removing no clone: status %d", status)
	}
	if status := serveJSON(t, srv, http.MethodDelete, "/api/job-clones/x", "", nil); status != http.StatusBadRequest {
		t.Fatalf("a bad id: status %d", status)
	}
}

func TestJobClonesWithoutTheService(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	if status := serveJSON(t, srv, http.MethodGet, "/api/job-clones", "", nil); status != http.StatusServiceUnavailable {
		t.Fatalf("status %d", status)
	}
}
