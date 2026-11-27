package chimera

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFailureClassTellsAGoneSessionFromATransientFailure(t *testing.T) {
	serve := func(t *testing.T, h http.HandlerFunc) string {
		server := httptest.NewServer(h)
		t.Cleanup(server.Close)
		return server.URL
	}
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closed.Close()
	cases := []struct {
		name     string
		endpoint func(t *testing.T) string
		timeout  time.Duration
		class    string
		gone     bool
	}{
		{"missing session", func(t *testing.T) string {
			return serve(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
		}, 0, FailureGone, true},
		{"nothing listens", func(*testing.T) string { return closed.URL }, 0, FailureRefused, true},
		{"server error", func(t *testing.T) string {
			return serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) })
		}, 0, FailureServerError, false},
		{"refused credential", func(t *testing.T) string {
			return serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) })
		}, 0, FailureAuth, false},
		{"busy", func(t *testing.T) string {
			return serve(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
		}, 0, FailureBusy, false},
		{"slow", func(t *testing.T) string {
			return serve(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(200 * time.Millisecond) })
		}, 20 * time.Millisecond, FailureTimeout, false},
		{"truncated", func(t *testing.T) string {
			return serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "50")
				_, _ = w.Write([]byte(`{"id":`))
			})
		}, 0, FailureMalformed, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.timeout)
				defer cancel()
			}
			_, err := NewClient(tc.endpoint(t)).GetSession(ctx, "s")
			if got := FailureClass(err); got != tc.class {
				t.Fatalf("class=%q, want %q (err %v)", got, tc.class, err)
			}
			if IsSessionGone(err) != tc.gone {
				t.Fatalf("gone=%v, want %v", !tc.gone, tc.gone)
			}
		})
	}
}

func TestStatusErrorKeepsOnlyTheStartOfABody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("x", 10_000)))
	}))
	defer server.Close()
	_, err := NewClient(server.URL).GetSession(context.Background(), "s")
	if err == nil || len(err.Error()) > 2*maxStatusBody {
		t.Fatalf("error kept %d bytes", len(err.Error()))
	}
}
