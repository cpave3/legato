package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/opencodeusage"
	"github.com/cpave3/legato/internal/engine/store"
)

func opencodeReaderFor(t *testing.T, handler http.HandlerFunc) *OpenCodeUsageReader {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &OpenCodeUsageReader{Client: &opencodeusage.Client{BaseURL: srv.URL, APIKey: "opencode-key-SENTINEL"}}
}

// The reader maps OpenCode's meters onto the shared window meters and says
// where the reading came from.
func TestOpenCodeUsageReaderNamesTheMeters(t *testing.T) {
	reader := opencodeReaderFor(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"usage":{"rolling":{"status":"ok","percent":30,"resetsAt":"2026-11-27T13:00:00Z"},"weekly":{"status":"ok","percent":66,"resetsAt":"2026-11-30T00:00:00Z"},"monthly":{"status":"ok","percent":50,"resetsAt":"2026-12-01T00:00:00Z"}}}`))
	})
	usage, err := reader.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage.Provider != "" || usage.Source != "endpoint" || usage.Plan != "" {
		t.Fatalf("usage = %+v", usage)
	}
	want := []struct {
		meter string
		used  float64
		reset time.Time
	}{
		{UsageMeterFiveHour, 30, time.Date(2026, 11, 27, 13, 0, 0, 0, time.UTC)},
		{UsageMeterWeekly, 66, time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)},
		{UsageMeterMonthly, 50, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)},
	}
	if len(usage.Windows) != len(want) {
		t.Fatalf("windows = %+v, want %+v", usage.Windows, want)
	}
	for i, w := range want {
		got := usage.Windows[i]
		if got.Meter != w.meter || got.UsedPercent != w.used || !got.ResetsAt.Equal(w.reset) {
			t.Errorf("window %d = %+v, want %+v", i, got, w)
		}
	}
}

// The endpoint's failures become the reasons the Usage surfaces show, each
// with what to do; no error carries the key.
func TestOpenCodeUsageReaderGivesReasons(t *testing.T) {
	cases := []struct {
		name       string
		handler    http.HandlerFunc
		wantReason string
		wantWait   time.Duration
	}{
		{
			name: "invalid key",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(401)
				w.Write([]byte(`{"type":"error","error":{"type":"AuthenticationError","message":"Invalid API key"}}`))
			},
			wantReason: UsageReasonInvalidKey,
		},
		{
			name: "no go subscription",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(403)
				w.Write([]byte(`{"type":"error","error":{"type":"EntitlementError","message":"OpenCode Go subscription required."}}`))
			},
			wantReason: UsageReasonNoSubscription,
		},
		{
			name: "rate limited",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "90")
				w.WriteHeader(429)
			},
			wantReason: UsageReasonRateLimited,
			wantWait:   90 * time.Second,
		},
		{
			name:       "shape changed",
			handler:    func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"usage":{}}`)) },
			wantReason: UsageReasonEndpointChanged,
		},
	}
	for _, c := range cases {
		reader := opencodeReaderFor(t, c.handler)
		_, err := reader.Read(context.Background())
		var unavailable *UsageUnavailableError
		if !errors.As(err, &unavailable) || unavailable.Reason != c.wantReason || unavailable.RetryAfter != c.wantWait {
			t.Errorf("%s: err = %v, want %s (wait %s)", c.name, err, c.wantReason, c.wantWait)
		}
	}

	// A network failure is unreachable, with its key never in the error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	reader := &OpenCodeUsageReader{Client: &opencodeusage.Client{BaseURL: url, APIKey: "opencode-key-SENTINEL"}}
	_, err := reader.Read(context.Background())
	var unavailable *UsageUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Reason != UsageReasonUnreachable {
		t.Fatalf("err = %v, want %s", err, UsageReasonUnreachable)
	}
}

// No key configured is a reason of its own, and it makes no request: the
// operator has not finished setup.
func TestOpenCodeUsageReaderWithoutAKeyMakesNoRequest(t *testing.T) {
	calls := 0
	reader := opencodeReaderFor(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	reader.Client.APIKey = ""
	_, err := reader.Read(context.Background())
	var unavailable *UsageUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Reason != UsageReasonMissingKey {
		t.Fatalf("err = %v, want %s", err, UsageReasonMissingKey)
	}
	if calls != 0 {
		t.Fatalf("%d requests were made without a key", calls)
	}
}

// The provider and meter words every surface shows come from the config
// provider table, not from switches here.
func TestProviderAndMeterWordsComeFromTheTable(t *testing.T) {
	if got := UsageProviderName(UsageProviderOpenCode); got != "OpenCode Go" {
		t.Fatalf("OpenCode Go named %q", got)
	}
	for id, want := range map[string]string{"claude": "Claude", "codex": "Codex", "unknown": "unknown"} {
		if got := UsageProviderName(id); got != want {
			t.Errorf("%s named %q, want %q", id, got, want)
		}
	}
	cases := map[string]string{
		UsageMeterFiveHour:     "5-hour",
		UsageMeterWeekly:       "weekly",
		UsageMeterWeeklyOpus:   "weekly Opus",
		UsageMeterWeeklySonnet: "weekly Sonnet",
		UsageMeterMonthly:      "monthly",
		"rolling":              "rolling",
	}
	for meter, want := range cases {
		if got := UsageMeterLabel(meter); got != want {
			t.Errorf("%s labelled %q, want %q", meter, got, want)
		}
		if meter == UsageMeterMonthly {
			if got := UsageMeterShort(meter); got != "mo" {
				t.Errorf("monthly short %q, want mo", got)
			}
		}
	}
	if got := UsageMeterShort(UsageMeterFiveHour); got != "5h" {
		t.Errorf("five_hour short %q, want 5h", got)
	}
}

// The new reasons have operator words, and the existing ones keep theirs.
func TestUsageReasonTextCoversTheNewReasons(t *testing.T) {
	if got := UsageReasonText(UsageReasonMissingKey); got == UsageReasonText(UsageReasonNotRead) || got == "" {
		t.Fatalf("missing_key says %q", got)
	}
	if got := UsageReasonText(UsageReasonInvalidKey); got == UsageReasonText(UsageReasonNotRead) || got == "" {
		t.Fatalf("invalid_key says %q", got)
	}
	if got := UsageReasonText(UsageReasonRateLimited); got != "asked too often; waiting" {
		t.Fatalf("rate_limited says %q", got)
	}
}

// The catalog tells every surface what exists: the providers Legato knows
// and the meters they report, from the config provider table, not from a
// reading.
func TestUsageCatalogNamesProvidersAndMeters(t *testing.T) {
	catalog := UsageCatalog()
	if len(catalog) != 3 {
		t.Fatalf("catalog = %+v", catalog)
	}
	if catalog[0].Provider != UsageProviderClaude || catalog[2].Provider != UsageProviderOpenCode {
		t.Fatalf("catalog order = %+v", catalog)
	}
	if catalog[2].Name != "OpenCode Go" {
		t.Fatalf("name = %q", catalog[2].Name)
	}
	var qualified []string
	for _, m := range catalog[2].Meters {
		qualified = append(qualified, m.Qualified)
	}
	if len(qualified) != 3 || qualified[2] != "opencode.monthly" {
		t.Fatalf("meters = %+v", catalog[2].Meters)
	}
	monthly := catalog[2].Meters[2]
	if monthly.Short != "mo" || monthly.Long != "monthly" {
		t.Fatalf("monthly = %+v", monthly)
	}
}

// The snapshot carries the catalog beside the readings, so the API's
// consumers can label providers and choose rule and pin meters without a
// list of their own.
func TestTheSnapshotCarriesTheCatalog(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewSubscriptionUsageService(SubscriptionUsageOptions{Store: db})
	snap, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	catalog, ok := body["catalog"].([]any)
	if !ok || len(catalog) != 3 {
		t.Fatalf("catalog = %v", body["catalog"])
	}
	opencode := catalog[2].(map[string]any)
	if opencode["provider"] != "opencode" || opencode["name"] != "OpenCode Go" {
		t.Fatalf("opencode = %v", opencode)
	}
}

