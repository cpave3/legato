package opencodeusage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sentinel is a key no answer, error or log may ever contain.
const sentinel = "sk-opencode-SENTINEL-never-show-me"

var now = time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)

// A response in the shape OpenCode's Go usage endpoint documents, the one
// this package relies on. Each meter is status, percent and resetsAt.
const fullResponse = `{
  "usage": {
    "rolling": { "status": "ok", "percent": 30, "resetsAt": "2026-11-21T15:00:00.000Z" },
    "weekly": { "status": "rate-limited", "percent": 100, "resetsAt": "2026-11-23T00:00:00.000Z" },
    "monthly": { "status": "ok", "percent": 50, "resetsAt": "2026-12-01T00:00:00.000Z" }
  }
}`

func client(url string) *Client {
	return &Client{BaseURL: url, APIKey: sentinel, Now: func() time.Time { return now }}
}

func TestReadDecodesTheMetersAndSendsTheKey(t *testing.T) {
	var gotAuth, gotPath string
	var gotAccept bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath, gotAccept = r.Header.Get("Authorization"), r.URL.String(), r.Header.Get("Accept") == "application/json"
		w.Write([]byte(fullResponse))
	}))
	defer srv.Close()

	reading, err := client(srv.URL).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if gotAuth != "Bearer "+sentinel || gotPath != "/inference/go/v1/usage" || !gotAccept {
		t.Fatalf("request: auth %q path %q accept %v", gotAuth, gotPath, gotAccept)
	}
	want := []Meter{
		{Name: Rolling, UsedPercent: 30, ResetsAt: time.Date(2026, 11, 21, 15, 0, 0, 0, time.UTC)},
		{Name: Weekly, UsedPercent: 100, Limited: true, ResetsAt: time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)},
		{Name: Monthly, UsedPercent: 50, ResetsAt: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)},
	}
	if len(reading.Meters) != len(want) {
		t.Fatalf("meters = %+v, want %+v", reading.Meters, want)
	}
	for i, m := range want {
		got := reading.Meters[i]
		if got.Name != m.Name || got.UsedPercent != m.UsedPercent || got.Limited != m.Limited || !got.ResetsAt.Equal(m.ResetsAt) {
			t.Errorf("meter %d = %+v, want %+v", i, got, m)
		}
	}
}

// Zero is a real reading: a meter at 0 percent is reported, not dropped.
func TestZeroPercentIsAReading(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"usage":{"rolling":{"status":"ok","percent":0,"resetsAt":"2026-11-27T13:00:00Z"},"weekly":{"status":"ok","percent":0,"resetsAt":"2026-11-30T00:00:00Z"},"monthly":{"status":"ok","percent":0,"resetsAt":"2026-12-01T00:00:00Z"}}}`))
	}))
	defer srv.Close()
	reading, err := client(srv.URL).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for _, m := range reading.Meters {
		if m.UsedPercent != 0 {
			t.Errorf("%s = %+v, want percent 0", m.Name, m)
		}
	}
	if len(reading.Meters) != 3 {
		t.Fatalf("meters = %+v", reading.Meters)
	}
}

// Without a key nothing is asked: the operator has not named a secret yet.
func TestReadWithoutAKeyFailsBeforeAnyRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request may be made without a key")
	}))
	defer srv.Close()
	_, err := (&Client{BaseURL: srv.URL, Now: func() time.Time { return now }}).Read(context.Background())
	if !errors.Is(err, ErrNoKey) {
		t.Fatalf("err = %v, want ErrNoKey", err)
	}
	// A key of only spaces is no key either.
	_, err = (&Client{BaseURL: srv.URL, APIKey: "   ", Now: func() time.Time { return now }}).Read(context.Background())
	if !errors.Is(err, ErrNoKey) {
		t.Fatalf("err = %v, want ErrNoKey", err)
	}
}

// The endpoint's statuses become typed errors, and none of them carries
// the key or the body.
func TestStatusesBecomeTypedErrorsWithoutTheKey(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{name: "invalid key", status: 401, body: `{"type":"error","error":{"type":"AuthenticationError","message":"Invalid API key"}}`, wantErr: ErrInvalidKey},
		{name: "no go subscription", status: 403, body: `{"type":"error","error":{"type":"EntitlementError","message":"OpenCode Go subscription required."}}`, wantErr: ErrNoSubscription},
		{name: "gone", status: 410, body: `gone`, wantErr: ErrEndpointChanged},
		{name: "not found", status: 404, body: `nope`, wantErr: ErrEndpointChanged},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			w.Write([]byte(c.body))
		}))
		_, err := client(srv.URL).Read(context.Background())
		srv.Close()
		if !errors.Is(err, c.wantErr) {
			t.Fatalf("%s: err = %v, want %v", c.name, err, c.wantErr)
		}
		if strings.Contains(err.Error(), sentinel) || strings.Contains(err.Error(), c.body) {
			t.Errorf("%s: error quotes the key or body: %v", c.name, err)
		}
	}

	// A server error is a plain failure, not a shape change.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("boom"))
	}))
	defer srv.Close()
	_, err := client(srv.URL).Read(context.Background())
	if err == nil || errors.Is(err, ErrEndpointChanged) {
		t.Fatalf("server error = %v, want a plain failure", err)
	}
	if strings.Contains(err.Error(), "boom") {
		t.Fatalf("error quotes the body: %v", err)
	}
}

// A 429 says how long to wait, as seconds or an HTTP date.
func TestRateLimitsCarryRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(429)
	}))
	defer srv.Close()
	_, err := client(srv.URL).Read(context.Background())
	var limited *RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter != 90*time.Second {
		t.Fatalf("err = %v, want RetryAfter 90s", err)
	}

	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", now.Add(2*time.Minute).UTC().Format(http.TimeFormat))
		w.WriteHeader(429)
	}))
	defer srv.Close()
	_, err = client(srv.URL).Read(context.Background())
	if !errors.As(err, &limited) || limited.RetryAfter != 2*time.Minute {
		t.Fatalf("err = %v, want RetryAfter 2m", err)
	}
}

// An answer this package does not know is a shape change, never a guess:
// an unknown status, a percent out of range, a limited meter below 100,
// a missing reset time, no known meter, or not JSON at all.
func TestUnknownAnswersAreEndpointChanged(t *testing.T) {
	cases := map[string]string{
		"unknown status":        `{"usage":{"rolling":{"status":"ok","percent":30,"resetsAt":"2026-11-27T13:00:00Z"},"weekly":{"status":"paused","percent":10,"resetsAt":"2026-11-30T00:00:00Z"},"monthly":{"status":"ok","percent":50,"resetsAt":"2026-12-01T00:00:00Z"}}}`,
		"percent above 100":     `{"usage":{"rolling":{"status":"ok","percent":140,"resetsAt":"2026-11-27T13:00:00Z"},"weekly":{"status":"ok","percent":10,"resetsAt":"2026-11-30T00:00:00Z"},"monthly":{"status":"ok","percent":50,"resetsAt":"2026-12-01T00:00:00Z"}}}`,
		"percent below 0":       `{"usage":{"rolling":{"status":"ok","percent":-1,"resetsAt":"2026-11-27T13:00:00Z"},"weekly":{"status":"ok","percent":10,"resetsAt":"2026-11-30T00:00:00Z"},"monthly":{"status":"ok","percent":50,"resetsAt":"2026-12-01T00:00:00Z"}}}`,
		"limited below 100":     `{"usage":{"rolling":{"status":"rate-limited","percent":87,"resetsAt":"2026-11-27T13:00:00Z"},"weekly":{"status":"ok","percent":10,"resetsAt":"2026-11-30T00:00:00Z"},"monthly":{"status":"ok","percent":50,"resetsAt":"2026-12-01T00:00:00Z"}}}`,
		"null percent":          `{"usage":{"rolling":{"status":"ok","percent":null,"resetsAt":"2026-11-27T13:00:00Z"},"weekly":{"status":"ok","percent":10,"resetsAt":"2026-11-30T00:00:00Z"},"monthly":{"status":"ok","percent":50,"resetsAt":"2026-12-01T00:00:00Z"}}}`,
		"missing reset time":    `{"usage":{"rolling":{"status":"ok","percent":30,"resetsAt":""},"weekly":{"status":"ok","percent":10,"resetsAt":"2026-11-30T00:00:00Z"},"monthly":{"status":"ok","percent":50,"resetsAt":"2026-12-01T00:00:00Z"}}}`,
		"invalid reset time":    `{"usage":{"rolling":{"status":"ok","percent":30,"resetsAt":"tomorrow"},"weekly":{"status":"ok","percent":10,"resetsAt":"2026-11-30T00:00:00Z"},"monthly":{"status":"ok","percent":50,"resetsAt":"2026-12-01T00:00:00Z"}}}`,
		"no known meter":        `{"usage":{"something":{"status":"ok","percent":30,"resetsAt":"2026-11-27T13:00:00Z"}}}`,
		"empty usage":           `{"usage":{}}`,
		"not json":              `}`,
		"usage of another type": `{"usage":[]}`,
	}
	for name, body := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(body))
		}))
		_, err := client(srv.URL).Read(context.Background())
		srv.Close()
		if !errors.Is(err, ErrEndpointChanged) {
			t.Errorf("%s: err = %v, want ErrEndpointChanged", name, err)
		}
	}
}

// A meter that is missing or null is left out, not guessed and not a shape
// change: OpenCode may add or drop a window.
func TestMissingMetersAreSkippedNotInvented(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"usage":{"rolling":{"status":"ok","percent":30,"resetsAt":"2026-11-27T13:00:00Z"},"monthly":null,"something_new":{"status":"ok","percent":1,"resetsAt":"2026-11-27T13:00:00Z"}}}`))
	}))
	defer srv.Close()
	reading, err := client(srv.URL).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(reading.Meters) != 1 || reading.Meters[0].Name != Rolling {
		t.Fatalf("meters = %+v, want only rolling", reading.Meters)
	}
}

// The origin is fixed: a redirect is an answer this package does not follow,
// and the key never leaves with the follow-up request an attacker could
// have pointed at.
func TestARedirectIsNotFollowed(t *testing.T) {
	var elsewhere http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {
		t.Error("the request followed a redirect")
	}
	other := httptest.NewServer(elsewhere)
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/collect", http.StatusFound)
	}))
	defer srv.Close()
	_, err := client(srv.URL).Read(context.Background())
	if !errors.Is(err, ErrEndpointChanged) {
		t.Fatalf("err = %v, want ErrEndpointChanged", err)
	}
}

// A broken network says so without the key, whose URL the standard library
// would quote.
func TestAnUnreachableEndpointDoesNotShowTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	_, err := client(url).Read(context.Background())
	if err == nil {
		t.Fatal("no error")
	}
	if strings.Contains(err.Error(), sentinel) || strings.Contains(err.Error(), url) {
		t.Fatalf("error quotes the key or URL: %v", err)
	}
}
