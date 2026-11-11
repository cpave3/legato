package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/engine/engram"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type fakeReeveEngram struct {
	view       service.ReeveEngramView
	proposed   string
	proposeErr error
	removed    []string
	closed     []string
}

func (f *fakeReeveEngram) View(context.Context) (service.ReeveEngramView, error) { return f.view, nil }
func (f *fakeReeveEngram) ProposeSeed(_ context.Context, seed, _ string) (*service.Approval, error) {
	if f.proposeErr != nil {
		return nil, f.proposeErr
	}
	f.proposed = seed
	return &service.Approval{Approval: store.Approval{ID: "ap-seed"}}, nil
}
func (f *fakeReeveEngram) RemoveMemory(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	return nil
}
func (f *fakeReeveEngram) CloseThread(_ context.Context, id string) error {
	f.closed = append(f.closed, id)
	return nil
}

func TestTheReevesEngramIsShownAndItsMemoriesAndThreadsRemoved(t *testing.T) {
	srv, _ := reeveServer(t)
	fake := &fakeReeveEngram{view: service.ReeveEngramView{Enabled: true, Name: "reeve-1a2b3c4d", Exists: true,
		Memories: []engram.Memory{{ID: "m1", Body: "Short answers"}}, Threads: []engram.Thread{}}}
	srv.SetReeveEngram(fake)
	rec := callAs(srv, "secret", http.MethodGet, "/api/reeve/engram", "")
	var view service.ReeveEngramView
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &view) != nil || view.Name != "reeve-1a2b3c4d" || len(view.Memories) != 1 {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodDelete, "/api/reeve/engram/memories/m1", ""); rec.Code != http.StatusOK || len(fake.removed) != 1 || fake.removed[0] != "m1" {
		t.Fatalf("remove memory: %d %s", rec.Code, rec.Body)
	}
	if rec := callAs(srv, "secret", http.MethodDelete, "/api/reeve/engram/threads/t1", ""); rec.Code != http.StatusOK || len(fake.closed) != 1 || fake.closed[0] != "t1" {
		t.Fatalf("remove thread: %d %s", rec.Code, rec.Body)
	}
}

func TestChangingTheReevesSeedWaitsForASignature(t *testing.T) {
	srv, _ := reeveServer(t)
	fake := &fakeReeveEngram{}
	srv.SetReeveEngram(fake)
	rec := callAs(srv, "secret", http.MethodPut, "/api/reeve/engram/seed", `{"seed": "Be brief."}`)
	var body map[string]string
	if rec.Code != http.StatusAccepted || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body["status"] != "needs_signature" || body["approval_id"] != "ap-seed" || fake.proposed != "Be brief." {
		t.Fatalf("put: %d %s", rec.Code, rec.Body)
	}
	fake.proposeErr = service.ErrNoDeviceEnrolled
	if rec := callAs(srv, "secret", http.MethodPut, "/api/reeve/engram/seed", `{"seed": "Be brief."}`); rec.Code != http.StatusConflict {
		t.Fatalf("with no device: %d %s", rec.Code, rec.Body)
	}
}

func TestWithoutTheReevesEngramServiceItsRoutesSaySo(t *testing.T) {
	srv, _ := reeveServer(t)
	if rec := callAs(srv, "secret", http.MethodGet, "/api/reeve/engram", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("get: %d", rec.Code)
	}
}
