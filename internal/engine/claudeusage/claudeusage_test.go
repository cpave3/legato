package claudeusage

import (
	"bytes"
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
)

// sentinel is a token no answer, error or log may ever contain.
const sentinel = "sk-ant-oat01-SENTINEL-never-show-me"

var now = time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)

// A response in the shape Claude Code 2.1.332 reads, the one this package
// relies on. Unknown windows and fields are ignored.
const fullResponse = `{
  "five_hour": {"utilization": 42.0, "resets_at": "2026-11-26T10:00:00.123456+00:00"},
  "seven_day": {"utilization": 91.5, "resets_at": "2026-12-01T09:00:00+00:00"},
  "seven_day_opus": null,
  "seven_day_sonnet": {"utilization": 12, "resets_at": "2026-12-01T09:00:00+00:00"},
  "seven_day_oauth_apps": null,
  "seven_day_cowork": null,
  "extra_usage": {"is_enabled": false, "monthly_limit": null, "used_credits": null, "utilization": null},
  "something_new": {"whatever": true}
}`

func writeCredentials(t *testing.T, token string, expires time.Time) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".credentials.json")
	body := `{"claudeAiOauth":{"accessToken":"` + token + `","refreshToken":"rt-SENTINEL","expiresAt":` +
		itoa(expires.UnixMilli()) + `,"scopes":["user:inference","user:profile"],"subscriptionType":"max"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func client(t *testing.T, url, credentials string) *Client {
	t.Helper()
	return &Client{
		BaseURL:     url,
		Credentials: FileCredentials{Path: credentials},
		Now:         func() time.Time { return now },
	}
}

func TestReadDecodesTheWindowsAndSendsTheLogin(t *testing.T) {
	var gotAuth, gotBeta, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotBeta, gotPath = r.Header.Get("Authorization"), r.Header.Get("anthropic-beta"), r.URL.String()
		w.Write([]byte(fullResponse))
	}))
	defer srv.Close()

	reading, err := client(t, srv.URL, writeCredentials(t, sentinel, now.Add(time.Hour))).Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if gotAuth != "Bearer "+sentinel || gotBeta != "oauth-2025-04-20" || gotPath != "/api/oauth/usage" {
		t.Fatalf("request: auth %q beta %q path %q", gotAuth, gotBeta, gotPath)
	}
	want := []Window{
		{Name: FiveHour, UsedPercent: 42, ResetsAt: time.Date(2026, 11, 26, 10, 0, 0, 123456000, time.UTC)},
		{Name: SevenDay, UsedPercent: 91.5, ResetsAt: time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)},
		{Name: SevenDaySonnet, UsedPercent: 12, ResetsAt: time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)},
	}
	if len(reading.Windows) != len(want) {
		t.Fatalf("windows = %+v, want %+v", reading.Windows, want)
	}
	for i, w := range want {
		got := reading.Windows[i]
		if got.Name != w.Name || got.UsedPercent != w.UsedPercent || !got.ResetsAt.Equal(w.ResetsAt) {
			t.Errorf("window %d = %+v, want %+v", i, got, w)
		}
	}
}

func TestReadWithoutCredentialsIsNotSignedIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request may be made without a login")
	}))
	defer srv.Close()
	_, err := client(t, srv.URL, filepath.Join(t.TempDir(), "missing.json")).Read(context.Background())
	if !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("err = %v, want ErrNotSignedIn", err)
	}

	// A file without a Claude.ai login (an API-key user) is not signed in either.
	path := filepath.Join(t.TempDir(), ".credentials.json")
	os.WriteFile(path, []byte(`{"mcpOAuth":{}}`), 0o600)
	_, err = client(t, srv.URL, path).Read(context.Background())
	if !errors.Is(err, ErrNotSignedIn) {
		t.Fatalf("err = %v, want ErrNotSignedIn", err)
	}
}

func TestAnExpiredLoginIsNotSentAndTheFileIsLeftAlone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request may be made with an expired token")
	}))
	defer srv.Close()
	path := writeCredentials(t, sentinel, now.Add(-time.Minute))
	before, _ := os.ReadFile(path)
	info, _ := os.Stat(path)

	_, err := client(t, srv.URL, path).Read(context.Background())
	if !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("err = %v, want ErrLoginExpired", err)
	}
	after, _ := os.ReadFile(path)
	info2, _ := os.Stat(path)
	if !bytes.Equal(before, after) || !info.ModTime().Equal(info2.ModTime()) {
		t.Fatal("the credentials file changed")
	}
}

func TestStatusesBecomeTypedErrorsWithoutTheToken(t *testing.T) {
	cases := []struct {
		name   string
		status int
		header map[string]string
		body   string
		check  func(error) bool
	}{
		{"401 is an expired login", 401, nil, `{"error":"` + sentinel + `"}`, func(err error) bool { return errors.Is(err, ErrLoginExpired) }},
		{"403 is no subscription", 403, nil, ``, func(err error) bool { return errors.Is(err, ErrNoSubscription) }},
		{"every window null is no subscription", 200, nil, `{"five_hour":null,"seven_day":null}`, func(err error) bool { return errors.Is(err, ErrNoSubscription) }},
		{"404 is a changed endpoint", 404, nil, ``, func(err error) bool { return errors.Is(err, ErrEndpointChanged) }},
		{"garbage is a changed endpoint", 200, nil, `<html>` + sentinel, func(err error) bool { return errors.Is(err, ErrEndpointChanged) }},
		{"a window of another shape is a changed endpoint", 200, nil, `{"five_hour":{"utilization":"lots"}}`, func(err error) bool { return errors.Is(err, ErrEndpointChanged) }},
		{"500 is a failure", 500, nil, sentinel, func(err error) bool { return err != nil }},
		{"429 says when to ask again", 429, map[string]string{"Retry-After": "120"}, ``, func(err error) bool {
			var limited *RateLimitedError
			return errors.As(err, &limited) && limited.RetryAfter == 2*time.Minute
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := client(t, srv.URL, writeCredentials(t, sentinel, now.Add(time.Hour))).Read(context.Background())
			if !tc.check(err) {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), sentinel) || strings.Contains(err.Error(), "SENTINEL") {
				t.Fatalf("the error shows the token or the body: %v", err)
			}
		})
	}
}

func TestAnUnreachableEndpointDoesNotShowTheToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	_, err := client(t, url, writeCredentials(t, sentinel, now.Add(time.Hour))).Read(context.Background())
	if err == nil || strings.Contains(err.Error(), "SENTINEL") {
		t.Fatalf("err = %v", err)
	}
}

func TestCredentialsPathFollowsClaudeConfigDir(t *testing.T) {
	if got := CredentialsPath(func(k string) string {
		if k == "CLAUDE_CONFIG_DIR" {
			return "/x/claude"
		}
		return ""
	}, "/home/me"); got != "/x/claude/.credentials.json" {
		t.Fatalf("got %q", got)
	}
	if got := CredentialsPath(func(string) string { return "" }, "/home/me"); got != "/home/me/.claude/.credentials.json" {
		t.Fatalf("got %q", got)
	}
}
