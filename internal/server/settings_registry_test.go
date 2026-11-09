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

func settingsServer(t *testing.T, file string) *Server {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(file), 0o600)
	t.Setenv("LEGATO_CONFIG", filepath.Join(dir, "config.yaml"))
	db, err := store.New(filepath.Join(dir, "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	srv.SetSettingsService(service.NewSettingsService(db, nil))
	return srv
}

type listedSettings struct {
	Settings []service.SettingValue `json:"settings"`
}

func findSetting(t *testing.T, srv *Server, key string) service.SettingValue {
	t.Helper()
	rec := callUpdates(srv, http.MethodGet, "/api/config/settings", "")
	var got listedSettings
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body)
	}
	for _, s := range got.Settings {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("%s not listed", key)
	return service.SettingValue{}
}

func TestSettingsAreListedWithTheirSource(t *testing.T) {
	srv := settingsServer(t, "icons: nerdfonts\n")
	if got := findSetting(t, srv, "icons"); got.Source != service.SettingFromFile || !got.Pinned || got.Value != "nerdfonts" {
		t.Fatalf("icons = %+v", got)
	}
	if got := findSetting(t, srv, "agents.reviews"); got.Source != service.SettingFromDefault || got.Class != "change" || got.Mode != "restart" {
		t.Fatalf("agents.reviews = %+v", got)
	}
}

func TestASettingIsSetAndReset(t *testing.T) {
	srv := settingsServer(t, "")
	rec := callUpdates(srv, http.MethodPut, "/api/config/settings/agents.reviews", `{"value": false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	if got := findSetting(t, srv, "agents.reviews"); got.Source != service.SettingFromDatabase || got.Value != false {
		t.Fatalf("after PUT = %+v", got)
	}
	if rec := callUpdates(srv, http.MethodDelete, "/api/config/settings/agents.reviews", ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE: %d %s", rec.Code, rec.Body)
	}
	if got := findSetting(t, srv, "agents.reviews"); got.Source != service.SettingFromDefault {
		t.Fatalf("after DELETE = %+v", got)
	}
}

func TestASettingChangeIsRefusedWithItsReason(t *testing.T) {
	srv := settingsServer(t, "icons: unicode\n")
	for path, want := range map[string]int{
		"/api/config/settings/icons":        http.StatusConflict,   // pinned
		"/api/config/settings/theme.colour": http.StatusNotFound,   // not a setting
		"/api/config/settings/updates.mode": http.StatusBadRequest, // not a mode
	} {
		rec := callUpdates(srv, http.MethodPut, path, `{"value": "sometimes"}`)
		if rec.Code != want {
			t.Errorf("PUT %s: %d %s, want %d", path, rec.Code, rec.Body, want)
		}
	}
	rec := callUpdates(srv, http.MethodPut, "/api/config/settings/icons", `{"value": "nerdfonts"}`)
	if !strings.Contains(rec.Body.String(), "config.yaml") {
		t.Errorf("a pinned refusal does not say where to change it: %s", rec.Body)
	}
	_ = context.Background()
}

func TestListEntriesAreAddedAndRemovedButNotTheFiles(t *testing.T) {
	srv := settingsServer(t, "workspaces:\n  - name: Work\n")
	if rec := callUpdates(srv, http.MethodPut, "/api/config/lists/workspaces/Home", `{"value": {"color": "#00ff00"}}`); rec.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	if rec := callUpdates(srv, http.MethodPut, "/api/config/lists/workspaces/work", `{"value": {}}`); rec.Code != http.StatusConflict {
		t.Fatalf("PUT a name the file has: %d %s", rec.Code, rec.Body)
	}
	rec := callUpdates(srv, http.MethodGet, "/api/config/lists", "")
	var got struct {
		Lists []service.SettingList `json:"lists"`
	}
	json.Unmarshal(rec.Body.Bytes(), &got)
	var names []string
	for _, l := range got.Lists {
		if l.Key == "workspaces" {
			for _, e := range l.Entries {
				names = append(names, e.Name+":"+e.Source)
			}
		}
	}
	if strings.Join(names, ",") != "Work:config.yaml,Home:database" {
		t.Fatalf("workspaces = %v", names)
	}
	if rec := callUpdates(srv, http.MethodDelete, "/api/config/lists/workspaces/Home", ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE: %d %s", rec.Code, rec.Body)
	}
	if rec := callUpdates(srv, http.MethodDelete, "/api/config/lists/workspaces/Home", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE again: %d", rec.Code)
	}
}

func TestSettingsExportDownloadsYAML(t *testing.T) {
	srv := settingsServer(t, "icons: nerdfonts\n")
	callUpdates(srv, http.MethodPut, "/api/config/settings/agents.reviews", `{"value": false}`)
	rec := callUpdates(srv, http.MethodGet, "/api/config/export", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "reviews: false") || strings.Contains(rec.Body.String(), "icons") {
		t.Fatalf("export: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "legato-settings.yaml") {
		t.Fatalf("headers = %v", rec.Header())
	}
	if rec := callUpdates(srv, http.MethodGet, "/api/config/export?pinned=1", ""); !strings.Contains(rec.Body.String(), "icons: nerdfonts") {
		t.Fatalf("pinned export: %s", rec.Body)
	}
}

func TestAnOwnRuleOfTheReeveIsRefusedWithNoDeviceToSign(t *testing.T) {
	srv := settingsServer(t, "")
	rec := callUpdates(srv, http.MethodPut, "/api/config/settings/reeve.levels.risky", `{"value": "approve"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "signed approval") {
		t.Fatalf("PUT an own rule: %d %s", rec.Code, rec.Body)
	}
}
