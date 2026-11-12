package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func reeveAutoServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.yaml"), nil, 0o600)
	t.Setenv("LEGATO_CONFIG", filepath.Join(dir, "config.yaml"))
	db, err := store.New(filepath.Join(dir, "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	settings := service.NewSettingsService(db, nil)
	approvals := service.NewApprovalService(db, service.ApprovalServiceOptions{})
	rules := &service.ReeveRules{Settings: settings, Approvals: approvals, Store: db}
	rules.Register()
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	srv.SetSettingsService(settings)
	srv.SetReeveRules(rules)
	return srv, db
}

func TestTheOffSwitchTurnsAutoOffWithoutASignature(t *testing.T) {
	srv, db := reeveAutoServer(t)
	ctx := context.Background()
	db.SetSetting(ctx, "reeve.levels.change", json.RawMessage(`"auto"`), "test")
	db.SetSetting(ctx, "reeve.host_operations_auto", json.RawMessage(`true`), "test")

	var status service.AutoStatus
	rec := callUpdates(srv, http.MethodGet, "/api/reeve/auto", "")
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &status) != nil || !status.On || !status.HostAuto {
		t.Fatalf("GET before: %d %s", rec.Code, rec.Body)
	}
	rec = callUpdates(srv, http.MethodPost, "/api/reeve/auto/off", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "reeve.host_operations_auto") {
		t.Fatalf("POST off: %d %s", rec.Code, rec.Body)
	}
	rec = callUpdates(srv, http.MethodGet, "/api/reeve/auto", "")
	if json.Unmarshal(rec.Body.Bytes(), &status) != nil || status.On {
		t.Fatalf("GET after: %d %s", rec.Code, rec.Body)
	}
}

func TestHostAutoTurnsOffDirectlyButOnOnlyWithASignature(t *testing.T) {
	srv, db := reeveAutoServer(t)
	db.SetSetting(context.Background(), "reeve.host_operations_auto", json.RawMessage(`true`), "test")
	if rec := callUpdates(srv, http.MethodPut, "/api/config/settings/reeve.host_operations_auto", `{"value": false}`); rec.Code != http.StatusOK {
		t.Fatalf("turning it off: %d %s", rec.Code, rec.Body)
	}
	// No device is enrolled to sign: refused, never downgraded.
	if rec := callUpdates(srv, http.MethodPut, "/api/config/settings/reeve.host_operations_auto", `{"value": true}`); rec.Code != http.StatusConflict {
		t.Fatalf("turning it on: %d %s", rec.Code, rec.Body)
	}
	if rec := callUpdates(srv, http.MethodPut, "/api/config/settings/reeve.levels.change", `{"value": "auto"}`); rec.Code != http.StatusConflict {
		t.Fatalf("change to auto: %d %s", rec.Code, rec.Body)
	}
}

func TestTheOffSwitchRoutesAreClassed(t *testing.T) {
	for method, want := range map[string]RouteClass{http.MethodGet: ClassRead, http.MethodPost: ClassChange} {
		path := "/api/reeve/auto"
		if method == http.MethodPost {
			path += "/off"
		}
		req, _ := http.NewRequest(method, path, nil)
		if got := ClassifyRequest(req); got.Class != want || !got.Known {
			t.Errorf("%s %s = %+v", method, path, got)
		}
	}
}
