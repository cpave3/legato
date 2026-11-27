package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/claudeusage"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestClaudeAccountReadingsRemainIndependentThroughPollAndReopen(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	failWork := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/oauth/usage" {
			t.Error("unexpected credential operation")
			w.WriteHeader(400)
			return
		}
		used := 12
		if r.Header.Get("Authorization") == "Bearer fixture-work" {
			used = 91
			if failWork {
				w.WriteHeader(401)
				return
			}
		}
		fmt.Fprintf(w, `{"seven_day":{"utilization":%d,"resets_at":"2030-12-01T09:00:00Z"}}`, used)
	}))
	defer srv.Close()
	db, err := store.New(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var readers []SubscriptionUsageReader
	var paths []string
	for _, account := range []string{"", "work"} {
		path := filepath.Join(t.TempDir(), ".credentials.json")
		token := "fixture-default"
		if account != "" {
			token = "fixture-work"
		}
		if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"expiresAt":%d}}`, token, now.Add(time.Hour).UnixMilli())), 0600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
		readers = append(readers, &ClaudeUsageReader{Account: account, Label: "Work", Client: &claudeusage.Client{BaseURL: srv.URL, Credentials: claudeusage.FileCredentials{Path: path}}})
	}
	before := make([][]byte, len(paths))
	beforeInfo := make([]os.FileInfo, len(paths))
	for i, path := range paths {
		before[i], _ = os.ReadFile(path)
		beforeInfo[i], _ = os.Stat(path)
	}
	options := SubscriptionUsageOptions{Store: db, Readers: readers, Now: func() time.Time { return now }}
	svc := NewSubscriptionUsageService(options)
	svc.Poll(ctx)
	snap, err := svc.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Providers) != 2 || snap.Providers[0].Provider != "claude" || snap.Providers[1].Provider != "claude:work" || snap.Providers[0].Windows[0].UsedPercent != 12 || snap.Providers[1].Windows[0].UsedPercent != 91 || snap.Providers[1].Kind != "claude" || snap.Providers[1].Account != "work" {
		t.Fatalf("accounts collided: %+v", snap.Providers)
	}
	found := false
	for _, p := range snap.Catalog {
		if p.Provider == "claude:work" && p.Meters[1].Qualified == "claude.work.weekly" {
			found = true
		}
	}
	if !found {
		t.Fatalf("named meters absent: %+v", snap.Catalog)
	}
	failWork = true
	now = now.Add(6 * time.Minute)
	svc = NewSubscriptionUsageService(options)
	svc.Poll(ctx)
	snap, err = svc.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Providers[0].State != UsageOK || snap.Providers[1].Reason != UsageReasonLoginExpired || len(snap.Providers[1].Windows) != 0 {
		t.Fatalf("account error leaked: %+v", snap.Providers)
	}
	for i, path := range paths {
		data, _ := os.ReadFile(path)
		info, _ := os.Stat(path)
		if string(data) != string(before[i]) || !info.ModTime().Equal(beforeInfo[i].ModTime()) {
			t.Fatal("poll modified login")
		}
	}
}

func TestUsageRulesSelectNamedAccountMeterWithoutChangingLegacyIDs(t *testing.T) {
	now := time.Now()
	order := 1
	profiles := []config.UsageProfile{{Name: "Work", Agent: "claude-code", Account: "work"}, {Name: "Default", Agent: "claude-code"}}
	rule := config.UsageRule{Name: "Work full", Order: &order, Meter: "claude.work.weekly", AtLeast: 90, Profile: "Default"}
	readings := []ProviderUsage{
		{Provider: "claude", State: UsageOK, AsOf: &now, Windows: []UsageWindow{{Meter: "weekly", UsedPercent: 5}}},
		{Provider: "claude:work", State: UsageOK, AsOf: &now, Windows: []UsageWindow{{Meter: "weekly", UsedPercent: 95}}},
	}
	got, _, _ := EvaluateUsageRules([]config.UsageRule{rule}, profiles, "Work", readings, UsageRuleLatch{}, now)
	if got != "Default" {
		t.Fatalf("named account rule selected %q", got)
	}
	rule.Meter = "claude.weekly"
	got, _, _ = EvaluateUsageRules([]config.UsageRule{rule}, profiles, "Work", readings, UsageRuleLatch{}, now)
	if got != "Work" {
		t.Fatalf("legacy default meter read another account: %q", got)
	}
}

func TestNamedAccountResetPinCapturesItsOwnDeadline(t *testing.T) {
	f := newUsageFixture(t)
	ctx := context.Background()
	named := &fakeUsageReader{provider: "claude:work", usage: ProviderUsage{Windows: []UsageWindow{{Meter: "weekly", UsedPercent: 40, ResetsAt: f.now.Add(time.Hour)}}}}
	f.svc = NewSubscriptionUsageService(SubscriptionUsageOptions{Store: f.db, Readers: []SubscriptionUsageReader{f.claude, named}, Now: func() time.Time { return f.now }})
	f.claude.set(ProviderUsage{Windows: claudeWindows(30)}, nil)
	cfg := config.UsageConfig{Profiles: []config.UsageProfile{{Name: "work", Agent: "claude-code", Account: "work"}}}
	policy := NewUsagePolicyService(UsagePolicyOptions{Store: f.db, Config: func(context.Context) (config.UsageConfig, error) { return cfg, nil }, Readings: f.svc.ReadingsSnapshot, Now: func() time.Time { return f.now }})
	f.svc.Poll(ctx)
	if err := policy.Pin(ctx, &UsagePinRequest{Profile: "work", UntilReset: "claude.work.weekly"}); err != nil {
		t.Fatal(err)
	}
	got, err := policy.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Active.Pin == nil || got.Active.Pin.Until == nil || !got.Active.Pin.Until.Equal(f.now.Add(time.Hour)) {
		t.Fatalf("wrong account reset: %+v", got.Active.Pin)
	}
	if err := policy.Pin(ctx, &UsagePinRequest{Profile: "work", UntilReset: "claude.absent.weekly"}); err == nil {
		t.Fatal("absent account pin accepted")
	}
}
