package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/opencodeusage"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The whole path, as the operator sees it: the configured OpenCode Go
// reader polls the endpoint's fixture, the reading is kept in the store, and
// /api/usage serves it beside the other providers with the catalog. Usage
// another client of the same key (Manticore) consumed appears on the next
// read, without any Legato OpenCode session, and a refused key keeps the
// last reading as stale while the other providers read on. The test fails
// if the Go reader is disconnected from this path.
func TestOpenCodeUsageFlowsFromTheEndpointToTheAPIBesideTheOthers(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	responses := make(chan string, 4)
	responses <- `{"usage":{"rolling":{"status":"ok","percent":30,"resetsAt":"2026-11-27T13:00:00Z"},"weekly":{"status":"ok","percent":40,"resetsAt":"2026-11-30T00:00:00Z"},"monthly":{"status":"ok","percent":50,"resetsAt":"2026-12-01T00:00:00Z"}}}`
	// Another client used the same key: the account's reading moved.
	responses <- `{"usage":{"rolling":{"status":"ok","percent":61,"resetsAt":"2026-11-27T13:00:00Z"},"weekly":{"status":"ok","percent":72,"resetsAt":"2026-11-30T00:00:00Z"},"monthly":{"status":"ok","percent":83,"resetsAt":"2026-12-01T00:00:00Z"}}}`
	// Then the key was revoked: the endpoint refuses it.
	responses <- ""
	requests := make(chan struct{}, 4)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- struct{}{}
		if r.URL.Path != "/inference/go/v1/usage" || r.Header.Get("Authorization") != "Bearer opencode-key-SENTINEL" {
			t.Errorf("request: path %q auth %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		body := <-responses
		if body == "" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"type":"error","error":{"type":"AuthenticationError","message":"Invalid API key"}}`))
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(endpoint.Close)

	now := time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)
	clock := &usageClock{at: now}
	opencode := &service.OpenCodeUsageReader{Client: &opencodeusage.Client{
		BaseURL: endpoint.URL, APIKey: "opencode-key-SENTINEL", Now: clock.Now,
	}}
	claude := &usageTestReader{provider: service.UsageProviderClaude, usage: service.ProviderUsage{Windows: []service.UsageWindow{
		{Meter: service.UsageMeterFiveHour, UsedPercent: 42, ResetsAt: now.Add(2 * time.Hour)},
		{Meter: service.UsageMeterWeekly, UsedPercent: 91, ResetsAt: now.Add(5 * 24 * time.Hour)},
	}, Source: "endpoint"}}
	svc := service.NewSubscriptionUsageService(service.SubscriptionUsageOptions{
		Store:        db,
		Readers:      []service.SubscriptionUsageReader{claude, opencode},
		PollInterval: 5 * time.Minute,
		Now:          clock.Now,
	})
	srv := New(nil, nil, nil, ":0")
	srv.SetSubscriptionUsage(svc)

	// Before any read the row is there to configure, not missing.
	_, body := getUsage(t, srv)
	row, ok := usageRow(body, "opencode")
	if !ok || row["state"] != "unavailable" || row["reason"] != "not_read" {
		t.Fatalf("before any read: %v", row)
	}

	svc.Poll(context.Background())
	<-requests
	_, body = getUsage(t, srv)
	if row, ok = usageRow(body, "opencode"); !ok {
		t.Fatalf("no opencode row: %v", body["providers"])
	}
	if row["state"] != "ok" || row["source"] != "endpoint" {
		t.Fatalf("row = %v", row)
	}
	windows := row["windows"].([]any)
	if len(windows) != 3 {
		t.Fatalf("windows = %v", windows)
	}
	rolling, weekly, monthly := windows[0].(map[string]any), windows[1].(map[string]any), windows[2].(map[string]any)
	if rolling["meter"] != "five_hour" || rolling["used_percent"] != 30.0 || rolling["resets_at"] != "2026-11-27T13:00:00Z" {
		t.Fatalf("rolling = %v", rolling)
	}
	if weekly["meter"] != "weekly" || weekly["used_percent"] != 40.0 || weekly["resets_at"] != "2026-11-30T00:00:00Z" {
		t.Fatalf("weekly = %v", weekly)
	}
	if monthly["meter"] != "monthly" || monthly["used_percent"] != 50.0 || monthly["resets_at"] != "2026-12-01T00:00:00Z" {
		t.Fatalf("monthly = %v", monthly)
	}
	if claudeRow, _ := usageRow(body, "claude"); claudeRow["state"] != "ok" {
		t.Fatalf("claude = %v", claudeRow)
	}

	// The catalog names the provider and its meters for the surfaces.
	catalog, _ := body["catalog"].([]any)
	if len(catalog) != 3 {
		t.Fatalf("catalog = %v", body["catalog"])
	}
	opencodeCatalog := catalog[2].(map[string]any)
	if opencodeCatalog["provider"] != "opencode" || opencodeCatalog["name"] != "OpenCode Go" {
		t.Fatalf("catalog = %v", opencodeCatalog)
	}
	if meters := opencodeCatalog["meters"].([]any); len(meters) != 3 || meters[2].(map[string]any)["qualified"] != "opencode.monthly" {
		t.Fatalf("meters = %v", meters)
	}

	// The account's reading moved elsewhere; the next read shows it.
	clock.Advance(5 * time.Minute)
	svc.Poll(context.Background())
	<-requests
	_, body = getUsage(t, srv)
	row, _ = usageRow(body, "opencode")
	if rolling = row["windows"].([]any)[0].(map[string]any); rolling["used_percent"] != 61.0 {
		t.Fatalf("the externally consumed usage did not appear: %v", row["windows"])
	}

	// A refused key drops the old reading, as an expired login does: it may
	// be another account's. The row says to replace the key, and the other
	// providers read on.
	clock.Advance(5 * time.Minute)
	svc.Refresh(context.Background())
	<-requests
	_, body = getUsage(t, srv)
	row, _ = usageRow(body, "opencode")
	if row["state"] != "unavailable" || row["reason"] != "invalid_key" {
		t.Fatalf("row = %v", row)
	}
	if len(row["windows"].([]any)) != 0 {
		t.Fatalf("the refused key's old reading was kept: %v", row)
	}
	if claudeRow, _ := usageRow(body, "claude"); claudeRow["state"] != "ok" {
		t.Fatalf("claude broke when opencode failed: %v", claudeRow)
	}
}

// usageClock is a controllable clock for the poller.
type usageClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *usageClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *usageClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.at = c.at.Add(d)
	c.mu.Unlock()
}

func usageRow(body map[string]any, provider string) (map[string]any, bool) {
	rows, _ := body["providers"].([]any)
	for _, row := range rows {
		if r, ok := row.(map[string]any); ok && r["provider"] == provider {
			return r, true
		}
	}
	return nil, false
}
