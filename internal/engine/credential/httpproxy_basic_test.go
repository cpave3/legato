package credential

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func basicAuth(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

// newBasicProxy serves one credential that signs with HTTP Basic as user.
func newBasicProxy(t *testing.T, upstream *httptest.Server, user string, backend *fakeBackend) *httptest.Server {
	t.Helper()
	u, _ := url.Parse(upstream.URL)
	p := &HTTPProxy{
		Credentials: map[string]ProxyCredential{
			"jira": {Name: "jira", Upstream: u, Username: user, Key: KeyPlace{Header: "Authorization", Scheme: "basic"}, Inspector: elevenLabsLike()},
		},
		Backend:   backend,
		Transport: upstream.Client().Transport,
	}
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	return srv
}

func TestABasicKeySignsAsTheCredentialsUserWithTheRealKey(t *testing.T) {
	upstream, seen := newUpstream(t, nil)
	proxy := newBasicProxy(t, upstream, "bot@example.com", backendWith(map[string]string{"lgk_one": "jira"}))

	// The agent sends its placeholder as the password, whatever user it names.
	resp, body := send(t, "GET", proxy.URL+"/c/jira/v1/voices", map[string]string{"Authorization": basicAuth("me@example.com", "lgk_one")}, "")
	if resp.StatusCode != 200 || body != `{"ok":true}` {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	if got := (*seen)[0].header.Get("Authorization"); got != basicAuth("bot@example.com", realKey) {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestABasicKeyWithoutThePlaceholderAsItsPasswordIsRefused(t *testing.T) {
	upstream, seen := newUpstream(t, nil)
	proxy := newBasicProxy(t, upstream, "bot@example.com", backendWith(map[string]string{"lgk_one": "jira"}))
	for _, header := range []string{"Bearer lgk_one", "Basic lgk_one", "Basic " + base64.StdEncoding.EncodeToString([]byte("lgk_one")), basicAuth("lgk_one", "x"), ""} {
		resp, _ := send(t, "GET", proxy.URL+"/c/jira/v1/voices", map[string]string{"Authorization": header}, "")
		if resp.StatusCode != 401 {
			t.Fatalf("%q: status %d", header, resp.StatusCode)
		}
	}
	if len(*seen) != 0 {
		t.Fatalf("upstream saw %d requests", len(*seen))
	}
}

func TestABasicKeyNeverComesBackInAResponseHeader(t *testing.T) {
	upstream, _ := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Echo-Auth", r.Header.Get("Authorization"))
		w.Header().Set("X-Echo-Key", "token="+realKey)
		w.Header().Set("X-Request-Id", "abc")
		_, _ = io.WriteString(w, "fine")
	})
	proxy := newBasicProxy(t, upstream, "bot@example.com", backendWith(map[string]string{"lgk_one": "jira"}))
	resp, _ := send(t, "GET", proxy.URL+"/c/jira/v1/voices", map[string]string{"Authorization": basicAuth("x", "lgk_one")}, "")
	for name, values := range resp.Header {
		for _, v := range values {
			if strings.Contains(v, realKey) || strings.Contains(v, base64.StdEncoding.EncodeToString([]byte("bot@example.com:"+realKey))) {
				t.Fatalf("header %s carries the key: %q", name, v)
			}
		}
	}
	if resp.Header.Get("X-Request-Id") != "abc" {
		t.Fatalf("headers = %v", resp.Header)
	}
}

func TestABasicKeyPlaceIsAHeaderWithoutAFormat(t *testing.T) {
	for _, bad := range []KeyPlace{
		{Scheme: "basic"},
		{Query: "key", Scheme: "basic"},
		{Header: "Authorization", Format: "Basic {secret}", Scheme: "basic"},
		{Header: "Authorization", Scheme: "digest"},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("%+v: no error", bad)
		}
	}
	if err := (KeyPlace{Header: "Authorization", Scheme: "basic"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
