package manticore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// instance is a Manticore that answers each path with one fixed body and
// keeps the requests it was sent.
type instance struct {
	*httptest.Server
	requests []*http.Request
}

func newInstance(t *testing.T, status int, bodies map[string]string) *instance {
	t.Helper()
	i := &instance{}
	i.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i.requests = append(i.requests, r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(bodies[r.URL.Path]))
	}))
	t.Cleanup(i.Close)
	return i
}

func TestSummaryAsksForUsageSinceATimeAndReturnsTheTotals(t *testing.T) {
	server := newInstance(t, http.StatusOK, map[string]string{
		"/api/dashboard/summary": `{"totalRequests":2120,"totalPromptTokens":917536695,"totalCompletionTokens":2588934,"totalTokens":920125629,"tokensPerSecond":99.5,"errorCount":1,"avgLatencyMs":12264.2}`,
	})
	// Midnight in a zone ahead of UTC: Manticore takes UTC times only.
	since := time.Date(2026, 10, 11, 0, 0, 0, 0, time.FixedZone("AEDT", 11*60*60))

	got, err := New(server.URL+"/").Summary(context.Background(), since)

	if err != nil {
		t.Fatal(err)
	}
	want := Summary{Requests: 2120, PromptTokens: 917536695, CompletionTokens: 2588934, TotalTokens: 920125629}
	if got != want {
		t.Errorf("summary = %+v, want %+v", got, want)
	}
	request := server.requests[0]
	if request.Method != http.MethodGet || request.URL.Path != "/api/dashboard/summary" {
		t.Errorf("request = %s %s", request.Method, request.URL.Path)
	}
	if start := request.URL.Query().Get("startDate"); start != "2026-10-10T13:00:00Z" {
		t.Errorf("startDate = %q, want the time in UTC", start)
	}
}

func TestHourlyReturnsTheTokensOfEachHourSinceATime(t *testing.T) {
	server := newInstance(t, http.StatusOK, map[string]string{
		"/api/dashboard/time-series": `[{"bucketStart":"2026-10-10T08:00:00Z","requests":7,"promptTokens":101411,"completionTokens":1611},{"bucketStart":"2026-10-10T09:00:00Z","requests":15,"promptTokens":412320,"completionTokens":2734}]`,
	})

	got, err := New(server.URL).Hourly(context.Background(), time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC))

	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d hours, want 2: %+v", len(got), got)
	}
	first := Bucket{Start: time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC), Requests: 7, PromptTokens: 101411, CompletionTokens: 1611}
	if got[0] != first {
		t.Errorf("first hour = %+v, want %+v", got[0], first)
	}
	if got[1].Tokens() != 412320+2734 {
		t.Errorf("tokens of the second hour = %d", got[1].Tokens())
	}
	query := server.requests[0].URL.Query()
	if query.Get("bucket") != "hour" || query.Get("startDate") != "2026-10-10T08:00:00Z" {
		t.Errorf("query = %v", query)
	}
}

func TestAnInstanceThatRefusesIsAnError(t *testing.T) {
	server := newInstance(t, http.StatusInternalServerError, map[string]string{
		"/api/dashboard/summary": `{"error":{"message":"database is locked"}}`,
	})

	_, err := New(server.URL).Summary(context.Background(), time.Now())

	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, want one that names the status", err)
	}
}

func TestAnAnswerThatIsNotUsageIsAnError(t *testing.T) {
	server := newInstance(t, http.StatusOK, map[string]string{"/api/dashboard/summary": `<html>dashboard</html>`})

	if _, err := New(server.URL).Summary(context.Background(), time.Now()); err == nil {
		t.Fatal("a page that is not JSON should be an error")
	}
}
