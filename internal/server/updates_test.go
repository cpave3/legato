package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
)

type fakeUpdates struct {
	mu       sync.Mutex
	status   service.UpdateStatus
	checkErr error
	applied  chan struct{}
}

func (f *fakeUpdates) Status(context.Context) service.UpdateStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

func (f *fakeUpdates) Check(context.Context) (service.UpdateStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, f.checkErr
}

func (f *fakeUpdates) Apply(context.Context) error {
	f.applied <- struct{}{}
	return nil
}

func (f *fakeUpdates) SetMode(_ context.Context, mode string) error {
	if mode != service.UpdateModeAuto && mode != service.UpdateModeAsk && mode != service.UpdateModeOff {
		return service.ErrUnknownUpdateMode
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status.Mode = mode
	return nil
}

func updateServer(t *testing.T, status service.UpdateStatus) (*Server, *fakeUpdates) {
	t.Helper()
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	updates := &fakeUpdates{status: status, applied: make(chan struct{}, 1)}
	srv.SetUpdateService(updates)
	return srv, updates
}

func callUpdates(srv *Server, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

var availableUpdate = service.UpdateStatus{
	Version: "v1.2.0", Release: true, Mode: "ask", State: service.UpdateAvailable,
	Available: true, Latest: "v1.3.0", LatestURL: "https://github.com/cpave3/legato/releases/tag/v1.3.0",
}

func TestUpdatesSayWhereTheyStand(t *testing.T) {
	srv, _ := updateServer(t, availableUpdate)

	rec := callUpdates(srv, http.MethodGet, "/api/updates", "")

	var got service.UpdateStatus
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Latest != "v1.3.0" || !got.Available {
		t.Errorf("GET /api/updates: %d %s", rec.Code, rec.Body)
	}
}

func TestAnUpdateStartsInTheBackground(t *testing.T) {
	srv, updates := updateServer(t, availableUpdate)

	if rec := callUpdates(srv, http.MethodPost, "/api/updates/apply", ""); rec.Code != http.StatusAccepted {
		t.Fatalf("POST apply: %d %s", rec.Code, rec.Body)
	}

	select {
	case <-updates.applied:
	case <-time.After(time.Second):
		t.Fatal("the update did not start")
	}
}

func TestAnUpdateIsRefusedWithNothingToInstall(t *testing.T) {
	for name, status := range map[string]service.UpdateStatus{
		"up to date":          {Version: "v1.3.0", Release: true, State: service.UpdateUpToDate},
		"a build from source": {Version: "dev"},
	} {
		t.Run(name, func(t *testing.T) {
			srv, updates := updateServer(t, status)
			if rec := callUpdates(srv, http.MethodPost, "/api/updates/apply", ""); rec.Code != http.StatusConflict {
				t.Errorf("POST apply: %d %s", rec.Code, rec.Body)
			}
			select {
			case <-updates.applied:
				t.Error("applied")
			case <-time.After(20 * time.Millisecond):
			}
		})
	}
}

func TestACheckThatFailsSaysWhy(t *testing.T) {
	srv, updates := updateServer(t, availableUpdate)
	updates.checkErr = errors.New("GitHub answered 403 Forbidden")

	rec := callUpdates(srv, http.MethodPost, "/api/updates/check", "")

	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "403") {
		t.Errorf("POST check: %d %s", rec.Code, rec.Body)
	}
}

func TestTheUpdateModeIsASetting(t *testing.T) {
	srv, _ := updateServer(t, availableUpdate)

	if rec := callUpdates(srv, http.MethodPut, "/api/settings/updates", `{"mode":"auto"}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"mode":"auto"`) {
		t.Errorf("PUT auto: %d %s", rec.Code, rec.Body)
	}
	if rec := callUpdates(srv, http.MethodPut, "/api/settings/updates", `{"mode":"sometimes"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("PUT sometimes: %d %s", rec.Code, rec.Body)
	}
}

func TestWebUIsAreToldWhenUpdatesChange(t *testing.T) {
	bus := events.New()
	srv := NewWithSwarm(nil, nil, nil, ":0", nil, bus, "")
	ch := make(chan WSMessage, 4)
	srv.hub.add(&wsClient{sendFn: func(msg WSMessage) { ch <- msg }})
	srv.StartUpdateEvents()

	bus.Publish(events.Event{Type: events.EventUpdateChanged})

	select {
	case msg := <-ch:
		if msg.Type != MsgUpdateChanged {
			t.Errorf("broadcast %q", msg.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("not told")
	}
}

func TestUpdatesWithoutAnUpdater(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")

	if rec := callUpdates(srv, http.MethodGet, "/api/updates", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET: %d", rec.Code)
	}
}

// The settings every page loads carry the updates, so the bar along the top
// costs no request of its own.
func TestTheSettingsCarryTheUpdates(t *testing.T) {
	srv, _ := updateServer(t, availableUpdate)

	rec := callUpdates(srv, http.MethodGet, "/api/settings", "")

	var settings struct {
		Updates *service.UpdateStatus `json:"updates"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &settings); err != nil || settings.Updates == nil || settings.Updates.Latest != "v1.3.0" {
		t.Errorf("settings = %s", rec.Body)
	}

	without := New(nil, nil, nil, ":0")
	without.SetAuthToken("secret")
	if rec := callUpdates(without, http.MethodGet, "/api/settings", ""); strings.Contains(rec.Body.String(), `"updates"`) {
		t.Errorf("a server without an updater says it has updates: %s", rec.Body)
	}
}
