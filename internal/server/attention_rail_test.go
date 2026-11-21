package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// railSettings is a settings registry whose effective configuration has
// the rail switched as given.
type railSettings struct {
	SettingsRegistry
	rail config.RailConfig
}

func (r railSettings) Effective(context.Context) (*config.Config, error) {
	cfg := config.Defaults()
	cfg.Web.Rail = r.rail
	return cfg, nil
}

func railServer(t *testing.T, rail config.RailConfig) *Server {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "rail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	attention := service.NewAttentionService(db, nil)
	for index, signal := range []string{service.AttentionSignalOperatorAction, service.AttentionSignalReviewReady, service.AttentionSignalPRMergeBlocked, service.AttentionSignalUpdateAvailable} {
		if _, err := attention.Upsert(context.Background(), service.AttentionInput{
			Producer: "rail", SignalKind: signal, SourceKey: fmt.Sprintf("s-%d", index), SourceRevision: "1",
			Title: signal, Reason: "Rail test", Category: "test", Urgency: "normal",
			Navigation: service.AttentionNavigation{Kind: "task", Target: "task-1"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	srv := New(nil, nil, nil, "")
	srv.SetAttentionService(attention)
	srv.SetSettingsService(railSettings{rail: rail})
	return srv
}

func listAttention(t *testing.T, srv *Server, query string) []service.AttentionItem {
	t.Helper()
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/attention"+query, nil))
	var body struct {
		Items []service.AttentionItem `json:"items"`
		Total int                     `json:"total"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Total != len(body.Items) {
		t.Fatalf("response %d %s: %v", response.Code, response.Body.String(), err)
	}
	return body.Items
}

func TestTheRailViewOfAttentionFollowsTheRailSettings(t *testing.T) {
	on := true
	srv := railServer(t, config.RailConfig{Review: &on})

	all := listAttention(t, srv, "")
	if len(all) != 4 || all[0].RailGroup != "" {
		t.Fatalf("the plain list = %+v", all)
	}
	rail := listAttention(t, srv, "?view=rail")
	groups := map[string]string{}
	for _, item := range rail {
		groups[item.SignalKind] = item.RailGroup
	}
	want := map[string]string{service.AttentionSignalOperatorAction: "actions", service.AttentionSignalReviewReady: "review", service.AttentionSignalUpdateAvailable: "legato"}
	if fmt.Sprint(groups) != fmt.Sprint(want) {
		t.Fatalf("rail = %v, want %v", groups, want)
	}
	response := httptest.NewRecorder()
	srv.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/attention?view=rail", nil))
	var body struct {
		Hidden      int      `json:"hidden_groups"`
		HiddenNames []string `json:"hidden_names"`
	}
	// Merge stays off: one group hidden.
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Hidden != 1 || len(body.HiddenNames) != 1 || body.HiddenNames[0] != "merge" {
		t.Fatalf("hidden_groups = %d, %v", body.Hidden, err)
	}
	if count, err := srv.railCount(context.Background()); err != nil || count != len(rail) {
		t.Fatalf("rail count = %d, %v; want %d", count, err, len(rail))
	}
}

func TestTheRailViewSaysHowLongAClosedSheetStaysAway(t *testing.T) {
	snooze := func(srv *Server) int {
		t.Helper()
		response := httptest.NewRecorder()
		srv.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/attention?view=rail", nil))
		var body struct {
			Snooze *int `json:"snooze_seconds"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Snooze == nil {
			t.Fatalf("rail view %s: %v", response.Body.String(), err)
		}
		return *body.Snooze
	}
	if got := snooze(railServer(t, config.RailConfig{})); got != 1800 {
		t.Fatalf("snooze_seconds unset = %d, want 1800", got)
	}
	if got := snooze(railServer(t, config.RailConfig{Snooze: "1h"})); got != 3600 {
		t.Fatalf("snooze_seconds with 1h = %d, want 3600", got)
	}
}
