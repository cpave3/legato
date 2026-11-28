package chimera

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const controlAuthPassword = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func controlAuthURL(t *testing.T, base string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword("legato", controlAuthPassword)
	return u.String()
}

type controlAuthTransport func(*http.Request) (*http.Response, error)

func (f controlAuthTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClientControlAuthStripsUserinfoBeforeTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "legato" || password != controlAuthPassword {
			t.Error("handler did not receive private Basic credentials")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"id":"session"}`))
	}))
	defer srv.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := NewClientWithHTTPClient(controlAuthURL(t, srv.URL), &http.Client{
		Transport: controlAuthTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.User != nil {
				t.Error("private userinfo reached transport URL")
			}
			return transport.RoundTrip(r)
		}),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	snapshot, err := client.GetSession(ctx, "session")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ID != "session" {
		t.Fatalf("snapshot ID = %q", snapshot.ID)
	}
}

func TestClientControlAuthRefusesRedirectsWithoutMutatingCaller(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(fmt.Sprintf("custom=%t", custom), func(t *testing.T) {
			var targetCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				targetCalls.Add(1)
				_, _ = w.Write([]byte(`{}`))
			}))
			defer target.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
			}))
			defer origin.Close()
			privateURL := controlAuthURL(t, origin.URL)
			client := NewClient(privateURL)
			var policyCalls int
			caller := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
				policyCalls++
				return nil
			}}
			if custom {
				client = NewClientWithHTTPClient(privateURL, caller)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, err := client.GetSession(ctx, "session"); err == nil {
				t.Error("credential-bearing request followed redirect")
			}
			if err := client.ResolvePermission(ctx, "session", "permission", PermissionAction{Decision: "allow"}); err == nil {
				t.Error("credential-bearing permission followed redirect")
			}
			if err := client.Subscribe(ctx, "session", "", func(EventEnvelope) {
				t.Error("redirected stream delivered an event")
			}); err == nil {
				t.Error("credential-bearing stream followed redirect")
			}
			if got := targetCalls.Load(); got != 0 {
				t.Errorf("target calls = %d, want 0", got)
			}
			if policyCalls != 0 {
				t.Error("credential-bearing request used caller redirect policy")
			}
			if custom {
				// The caller's own client must still follow redirects as configured.
				resp, err := caller.Get(origin.URL)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if policyCalls != 1 || targetCalls.Load() != 1 {
					t.Error("caller client redirect behavior was changed")
				}
			}
		})
	}
}

func TestClientControlAuthTransportErrorsDoNotExposeCredentials(t *testing.T) {
	privateURL := controlAuthURL(t, "http://127.0.0.1:1")
	failure := errors.New("transport failed")
	client := NewClientWithHTTPClient(privateURL, &http.Client{
		Transport: controlAuthTransport(func(r *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("%w: endpoint %s password %s authorization %s", failure, privateURL, controlAuthPassword, r.Header.Get("Authorization"))
		}),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := client.GetSession(ctx, "session")
	if err == nil {
		t.Fatal("expected transport error")
	}
	for _, secret := range []string{privateURL, controlAuthPassword, "legato:", "Basic "} {
		if strings.Contains(err.Error(), secret) {
			t.Error("transport error exposes private credentials")
		}
	}
	if !errors.Is(err, failure) {
		t.Error("transport error lost its cause")
	}
	if err := client.Subscribe(ctx, "session", "", func(EventEnvelope) {}); err == nil || strings.Contains(err.Error(), controlAuthPassword) || strings.Contains(err.Error(), "Basic ") {
		t.Error("SSE transport error missing or exposes credentials")
	}
}

func TestClientControlAuthProductionRequests(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "legato" || password != controlAuthPassword {
			t.Error("handler did not receive private Basic credentials")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		if strings.HasSuffix(r.URL.Path, "/events") {
			if r.Header.Get("Accept") != "text/event-stream" {
				t.Error("missing SSE Accept header")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"idle\",\"eventId\":\"event-2\"}\n\n"))
			return
		}
		_, _ = w.Write([]byte(`{"id":"session"}`))
	}))
	defer srv.Close()
	client := NewClient(controlAuthURL(t, srv.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.ResolvePermission(ctx, "session", "permission", PermissionAction{Decision: "allow"}); err != nil {
		t.Fatal(err)
	}
	if err := client.AnswerQuestion(ctx, "session", "question", QuestionAnswer{Dismissed: true}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := client.GetSession(ctx, "session")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ID != "session" {
		t.Fatalf("snapshot ID = %q", snapshot.ID)
	}
	var events []EventEnvelope
	if err := client.Subscribe(ctx, "session", "event-1", func(event EventEnvelope) { events = append(events, event) }); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].EventID != "event-2" {
		t.Fatalf("events = %+v", events)
	}
	want := []string{
		"POST /v1/sessions/session/permissions/permission",
		"POST /v1/sessions/session/questions/question",
		"GET /v1/sessions/session",
		"GET /v1/sessions/session/events?since=event-1",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}
