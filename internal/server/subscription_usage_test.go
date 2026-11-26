package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type usageTestReader struct {
	provider string
	usage    service.ProviderUsage
	err      error
	reads    int
}

func (r *usageTestReader) Provider() string { return r.provider }
func (r *usageTestReader) Read(context.Context) (service.ProviderUsage, error) {
	r.reads++
	return r.usage, r.err
}

func usageServer(t *testing.T) (*Server, *service.SubscriptionUsageService, *usageTestReader, *usageTestReader) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	claude := &usageTestReader{provider: service.UsageProviderClaude, usage: service.ProviderUsage{Windows: []service.UsageWindow{
		{Meter: service.UsageMeterFiveHour, UsedPercent: 42, ResetsAt: time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)},
		{Meter: service.UsageMeterWeekly, UsedPercent: 91, ResetsAt: time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)},
	}, Source: "endpoint"}}
	codex := &usageTestReader{provider: service.UsageProviderCodex, err: &service.UsageUnavailableError{Reason: service.UsageReasonCodexMissing}}
	svc := service.NewSubscriptionUsageService(service.SubscriptionUsageOptions{
		Store: db, Readers: []service.SubscriptionUsageReader{claude, codex},
	})
	srv := New(nil, nil, nil, ":0")
	srv.SetSubscriptionUsage(svc)
	return srv, svc, claude, codex
}

func getUsage(t *testing.T, srv *Server) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/usage", nil))
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestGetUsageServesTheCacheWithoutReading(t *testing.T) {
	srv, svc, claude, _ := usageServer(t)
	code, body := getUsage(t, srv)
	if code != http.StatusOK || claude.reads != 0 {
		t.Fatalf("GET: %d, reads %d", code, claude.reads)
	}
	providers := body["providers"].([]any)
	if first := providers[0].(map[string]any); first["state"] != "unavailable" || first["reason"] != "not_read" {
		t.Fatalf("before any read: %v", first)
	}

	svc.Refresh(context.Background())
	code, body = getUsage(t, srv)
	providers = body["providers"].([]any)
	claudeUsage, codexUsage := providers[0].(map[string]any), providers[1].(map[string]any)
	if code != http.StatusOK || claudeUsage["state"] != "ok" || claudeUsage["stale"] != false || claudeUsage["as_of"] == nil {
		t.Fatalf("claude: %v", claudeUsage)
	}
	weekly := claudeUsage["windows"].([]any)[1].(map[string]any)
	if weekly["meter"] != "weekly" || weekly["used_percent"] != 91.0 || weekly["resets_at"] != "2026-12-01T09:00:00Z" {
		t.Fatalf("weekly: %v", weekly)
	}
	if codexUsage["state"] != "unavailable" || codexUsage["reason"] != "codex_missing" || len(codexUsage["windows"].([]any)) != 0 {
		t.Fatalf("codex: %v", codexUsage)
	}
	if body["poll_interval_seconds"] != 300.0 {
		t.Fatalf("poll interval: %v", body["poll_interval_seconds"])
	}
}

func TestGetUsageWithoutTheServiceIsEmptyNotAnError(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	code, body := getUsage(t, srv)
	if code != http.StatusOK || len(body["providers"].([]any)) != 0 {
		t.Fatalf("GET: %d %v", code, body)
	}
}

func TestRefreshUsageAsksForAReadAndAnswersAtOnce(t *testing.T) {
	refreshed := make(chan struct{}, 1)
	srv, svc, claude, _ := usageServer(t)
	_ = svc
	srv.SetSubscriptionUsage(usageWithForward{SubscriptionUsageSource: svc, forward: func() { refreshed <- struct{}{} }})
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/usage/refresh", strings.NewReader("")))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST: %d", rec.Code)
	}
	select {
	case <-refreshed:
	case <-time.After(time.Second):
		t.Fatal("no refresh was asked for")
	}
	if claude.reads != 0 {
		t.Fatal("the request read a provider itself")
	}
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/usage/refresh", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET refresh: %d", rec.Code)
	}
}

type usageWithForward struct {
	SubscriptionUsageSource
	forward func()
}

func (u usageWithForward) RequestRefresh() { u.forward() }

func TestUsageChangesAreBroadcast(t *testing.T) {
	srv, _, _, _ := usageServer(t)
	bus := events.New()
	stop := srv.StartSubscriptionUsageEvents(bus)
	defer stop()
	ch := make(chan WSMessage, 4)
	srv.hub.add(&wsClient{sendFn: func(msg WSMessage) { ch <- msg }})
	for _, kind := range []events.EventType{events.EventSubscriptionUsageChanged, events.EventSubscriptionUsageInvalidated} {
		bus.Publish(events.Event{Type: kind, At: time.Now()})
		select {
		case msg := <-ch:
			if msg.Type != MsgUsageChanged {
				t.Fatalf("broadcast %q", msg.Type)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("event %v was not broadcast", kind)
		}
	}
}

func TestUsagePinAPIKeepsAndClearsAnOperatorPin(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "pin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	usage := service.NewSubscriptionUsageService(service.SubscriptionUsageOptions{Store: db})
	policy := service.NewUsagePolicyService(service.UsagePolicyOptions{Store: db, Config: func(context.Context) (config.UsageConfig, error) {
		return config.UsageConfig{Profiles: []config.UsageProfile{{Name: "save", Agent: "chimera"}}}, nil
	}, Readings: usage.ReadingsSnapshot})
	usage.SetPolicy(policy, policy.Pin)
	srv := New(nil, nil, nil, ":0")
	srv.SetSubscriptionUsage(usage)
	for _, step := range []struct {
		method, body string
		want         int
		pin          bool
	}{
		{"PUT", `{"profile":"missing"}`, 400, false},
		{"PUT", `{"profile":"save","until":"2020-01-01T00:00:00Z"}`, 400, false},
		{"PUT", `{"profile":"save","unknown":true}`, 400, false},
		{"PUT", `{"profile":"save"}`, 200, true},
		{"DELETE", "", 200, false},
	} {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(step.method, "/api/usage/pin", strings.NewReader(step.body)))
		if rec.Code != step.want {
			t.Fatalf("%s %s = %d %s", step.method, step.body, rec.Code, rec.Body.String())
		}

		code, body := getUsage(t, srv)
		if code != 200 {
			t.Fatalf("GET policy: %d %+v", code, body)
		}
		data, _ := json.Marshal(body)
		var snap service.SubscriptionUsage
		if err := json.Unmarshal(data, &snap); err != nil {
			t.Fatal(err)
		}
		if snap.Policy == nil || (snap.Policy.Active.Pin != nil) != step.pin {
			t.Fatalf("HTTP policy = %+v", snap.Policy)
		}
		if step.pin && snap.Policy.Active.Pin.Profile != "save" {
			t.Fatalf("HTTP pin = %+v", snap.Policy.Active.Pin)
		}
	}
}
