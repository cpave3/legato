package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/claudeusage"
	"github.com/cpave3/legato/internal/engine/codexusage"
)

func claudeReaderFor(t *testing.T, handler http.HandlerFunc) *ClaudeUsageReader {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	path := filepath.Join(t.TempDir(), ".credentials.json")
	expires := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	os.WriteFile(path, []byte(`{"claudeAiOauth":{"accessToken":"tok-SENTINEL","expiresAt":`+expires+`,"subscriptionType":"max"}}`), 0o600)
	return &ClaudeUsageReader{Client: &claudeusage.Client{BaseURL: srv.URL, Credentials: claudeusage.FileCredentials{Path: path}}}
}

func TestClaudeUsageReaderNamesTheMeters(t *testing.T) {
	reader := claudeReaderFor(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"five_hour":{"utilization":42,"resets_at":"2026-11-26T10:00:00Z"},"seven_day":{"utilization":91,"resets_at":"2026-12-01T09:00:00Z"},"seven_day_opus":{"utilization":3,"resets_at":null},"seven_day_sonnet":null}`))
	})
	usage, err := reader.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var meters []string
	for _, w := range usage.Windows {
		meters = append(meters, w.Meter)
	}
	if strings.Join(meters, ",") != "five_hour,weekly,weekly_opus" || usage.Source != "endpoint" || usage.Windows[1].UsedPercent != 91 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestClaudeUsageReaderGivesReasons(t *testing.T) {
	for status, want := range map[int]string{401: UsageReasonLoginExpired, 403: UsageReasonNoSubscription, 404: UsageReasonEndpointChanged, 500: UsageReasonError} {
		reader := claudeReaderFor(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })
		_, err := reader.Read(context.Background())
		var unavailable *UsageUnavailableError
		if !errors.As(err, &unavailable) || unavailable.Reason != want {
			t.Errorf("status %d: err = %v, want %s", status, err, want)
		}
	}
	reader := claudeReaderFor(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(429)
	})
	_, err := reader.Read(context.Background())
	var unavailable *UsageUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Reason != UsageReasonRateLimited || unavailable.RetryAfter != 90*time.Second {
		t.Fatalf("429: err = %v", err)
	}
}

type fakeCodexAppServer struct {
	reading codexusage.Reading
	err     error
}

func (f fakeCodexAppServer) Read(context.Context) (codexusage.Reading, error) {
	return f.reading, f.err
}

func writeCodexLog(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "2026", "11", "25")
	os.MkdirAll(dir, 0o700)
	line := `{"timestamp":"2026-11-25T09:30:00Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":5.0,"window_minutes":10080,"resets_at":1795252211},"secondary":null,"credits":{"has_credits":true,"unlimited":false,"balance":"62500.0"}}}}`
	os.WriteFile(filepath.Join(dir, "rollout-a.jsonl"), []byte(line+"\n"), 0o600)
	return root
}

func TestCodexUsageReaderAsksTheAppServerFirst(t *testing.T) {
	reader := &CodexUsageReader{
		AppServer: fakeCodexAppServer{reading: codexusage.Reading{Source: codexusage.SourceAppServer, PlanType: "pro", Windows: []codexusage.Window{
			{Minutes: 10080, UsedPercent: 5}, {Minutes: 300, UsedPercent: 61.5}, {Minutes: 1440, UsedPercent: 1},
		}}},
		SessionsDir: writeCodexLog(t),
	}
	usage, err := reader.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if usage.Source != "app_server" || usage.AsOf != nil || usage.Plan != "pro" || len(usage.Windows) != 2 ||
		usage.Windows[0].Meter != UsageMeterWeekly || usage.Windows[1].Meter != UsageMeterFiveHour || usage.Windows[1].WindowMinutes != 300 {
		t.Fatalf("usage = %+v (a window of another length is left out)", usage)
	}
}

func TestCodexUsageReaderFallsBackToTheLogs(t *testing.T) {
	for _, appErr := range []error{codexusage.ErrCodexMissing, codexusage.ErrUnsupported, errors.New("timed out")} {
		reader := &CodexUsageReader{AppServer: fakeCodexAppServer{err: appErr}, SessionsDir: writeCodexLog(t)}
		usage, err := reader.Read(context.Background())
		if err != nil {
			t.Fatalf("%v: %v", appErr, err)
		}
		if usage.Source != "logs" || usage.AsOf == nil || !usage.AsOf.Equal(time.Date(2026, 11, 25, 9, 30, 0, 0, time.UTC)) ||
			len(usage.Windows) != 1 || usage.Windows[0].Meter != UsageMeterWeekly || usage.Credits == nil || usage.Credits.Balance != "62500.0" {
			t.Fatalf("%v: usage = %+v", appErr, usage)
		}
	}
}

func TestCodexUsageReaderGivesTheAppServersReasonWithoutLogs(t *testing.T) {
	for appErr, want := range map[error]string{
		codexusage.ErrCodexMissing: UsageReasonCodexMissing,
		codexusage.ErrNotSignedIn:  UsageReasonNotSignedIn,
		codexusage.ErrNoData:       UsageReasonNoData,
	} {
		reader := &CodexUsageReader{AppServer: fakeCodexAppServer{err: appErr}, SessionsDir: filepath.Join(t.TempDir(), "none")}
		_, err := reader.Read(context.Background())
		var unavailable *UsageUnavailableError
		if !errors.As(err, &unavailable) || unavailable.Reason != want {
			t.Errorf("%v: err = %v, want %s", appErr, err, want)
		}
	}
}
