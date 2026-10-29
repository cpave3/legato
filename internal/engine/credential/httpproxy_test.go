package credential

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const realKey = "sk_REAL_KEY_123"

// fakeBackend resolves one placeholder and allows what allow says.
type fakeBackend struct {
	mu           sync.Mutex
	placeholders map[string]string // placeholder -> credential
	decide       func(ProxyUse) Decision
	uses         []ProxyUse
	measured     []map[string]int64
}

func (f *fakeBackend) ResolvePlaceholder(_ context.Context, placeholder string) (string, Execution, bool, error) {
	name, ok := f.placeholders[placeholder]
	return name, Execution{TaskID: "t1", ExecutionID: "ex-1"}, ok, nil
}

func (f *fakeBackend) Authorize(_ context.Context, use ProxyUse) (Decision, error) {
	f.mu.Lock()
	f.uses = append(f.uses, use)
	f.mu.Unlock()
	if f.decide != nil {
		return f.decide(use), nil
	}
	return Decision{Allowed: true}, nil
}

func (f *fakeBackend) Key(_ context.Context, use ProxyUse) ([]byte, func(), error) {
	return []byte(realKey), nil, nil
}

func (f *fakeBackend) Measured(_ context.Context, use ProxyUse, amounts map[string]int64) {
	f.mu.Lock()
	f.measured = append(f.measured, amounts)
	f.mu.Unlock()
}

// upstreamSeen is what the fake upstream received.
type upstreamSeen struct {
	method, path, query string
	header              http.Header
	body                string
}

func newUpstream(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *[]upstreamSeen) {
	t.Helper()
	var mu sync.Mutex
	seen := &[]upstreamSeen{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		*seen = append(*seen, upstreamSeen{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), string(body)})
		mu.Unlock()
		if respond != nil {
			respond(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

func newProxy(t *testing.T, upstream *httptest.Server, key KeyPlace, backend *fakeBackend) *httptest.Server {
	t.Helper()
	u, _ := url.Parse(upstream.URL)
	p := &HTTPProxy{
		Credentials: map[string]ProxyCredential{
			"elevenlabs": {Name: "elevenlabs", Upstream: u, Key: key, Inspector: elevenLabsLike()},
			"openai":     {Name: "openai", Upstream: u, Key: key, Inspector: elevenLabsLike()},
		},
		Backend:   backend,
		Transport: upstream.Client().Transport,
	}
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	return srv
}

func backendWith(placeholders map[string]string) *fakeBackend {
	return &fakeBackend{placeholders: placeholders}
}

func send(t *testing.T, method, rawURL string, header map[string]string, body string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(method, rawURL, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(data)
}

func TestTheProxyReplacesThePlaceholderWithTheKey(t *testing.T) {
	upstream, seen := newUpstream(t, nil)
	backend := backendWith(map[string]string{"lgk_one": "elevenlabs"})
	proxy := newProxy(t, upstream, KeyPlace{Header: "xi-api-key"}, backend)

	resp, body := send(t, "POST", proxy.URL+"/c/elevenlabs/v1/text-to-speech/abc?output=mp3", map[string]string{"xi-api-key": "lgk_one", "Content-Type": "application/json"}, `{"text":"héllo"}`)
	if resp.StatusCode != 200 || body != `{"ok":true}` {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	got := (*seen)[0]
	if got.method != "POST" || got.path != "/v1/text-to-speech/abc" || got.query != "output=mp3" || got.body != `{"text":"héllo"}` {
		t.Fatalf("upstream saw %+v", got)
	}
	if got.header.Get("xi-api-key") != realKey {
		t.Fatalf("upstream key = %q", got.header.Get("xi-api-key"))
	}
	for name, values := range got.header {
		for _, v := range values {
			if strings.Contains(v, "lgk_one") {
				t.Fatalf("upstream header %s holds the placeholder", name)
			}
		}
	}
	if len(backend.uses) != 1 || backend.uses[0].Operation != "tts.speak" || backend.uses[0].Amounts["characters"] != 5 || backend.uses[0].ExecutionID != "ex-1" {
		t.Fatalf("uses = %+v", backend.uses)
	}
}

func TestTheProxyRefusesAMissingOrForeignPlaceholder(t *testing.T) {
	upstream, seen := newUpstream(t, nil)
	backend := backendWith(map[string]string{"lgk_openai": "openai"})
	proxy := newProxy(t, upstream, KeyPlace{Header: "xi-api-key"}, backend)

	for name, header := range map[string]map[string]string{
		"none":    {},
		"unknown": {"xi-api-key": "lgk_nobody"},
		"foreign": {"xi-api-key": "lgk_openai"},
		"real":    {"xi-api-key": realKey},
	} {
		resp, _ := send(t, "GET", proxy.URL+"/c/elevenlabs/v1/voices", header, "")
		if resp.StatusCode != 401 {
			t.Errorf("%s: status %d, want 401", name, resp.StatusCode)
		}
	}
	if len(*seen) != 0 {
		t.Fatalf("upstream was called %d times", len(*seen))
	}
}

func TestABearerFormatAndAQueryParameterCarryTheKey(t *testing.T) {
	upstream, seen := newUpstream(t, nil)
	backend := backendWith(map[string]string{"lgk_one": "elevenlabs"})
	bearer := newProxy(t, upstream, KeyPlace{Header: "Authorization", Format: "Bearer {secret}"}, backend)
	send(t, "GET", bearer.URL+"/c/elevenlabs/v1/voices", map[string]string{"Authorization": "Bearer lgk_one"}, "")
	if got := (*seen)[0].header.Get("Authorization"); got != "Bearer "+realKey {
		t.Fatalf("Authorization = %q", got)
	}

	query := newProxy(t, upstream, KeyPlace{Query: "key"}, backend)
	send(t, "GET", query.URL+"/c/elevenlabs/v1/voices?key=lgk_one&page=2", nil, "")
	q, _ := url.ParseQuery((*seen)[1].query)
	if q.Get("key") != realKey || q.Get("page") != "2" || strings.Contains((*seen)[1].query, "lgk_one") {
		t.Fatalf("query = %q", (*seen)[1].query)
	}
}

func TestTheAgentNeverReceivesTheKey(t *testing.T) {
	upstream, _ := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Echo-Key", r.Header.Get("xi-api-key"))
		w.Header().Set("X-Request-Id", "abc")
		_, _ = io.WriteString(w, "fine")
	})
	proxy := newProxy(t, upstream, KeyPlace{Header: "xi-api-key"}, backendWith(map[string]string{"lgk_one": "elevenlabs"}))
	resp, _ := send(t, "GET", proxy.URL+"/c/elevenlabs/v1/voices", map[string]string{"xi-api-key": "lgk_one"}, "")
	if resp.Header.Get("X-Echo-Key") != "" || resp.Header.Get("X-Request-Id") != "abc" {
		t.Fatalf("headers = %v", resp.Header)
	}
}

func TestHopByHopAndProxyHeadersAreRemoved(t *testing.T) {
	upstream, seen := newUpstream(t, nil)
	proxy := newProxy(t, upstream, KeyPlace{Header: "xi-api-key"}, backendWith(map[string]string{"lgk_one": "elevenlabs"}))
	send(t, "GET", proxy.URL+"/c/elevenlabs/v1/voices", map[string]string{
		"xi-api-key": "lgk_one", "Proxy-Authorization": "Basic x", "Connection": "X-Drop", "X-Drop": "1", "Te": "trailers", "X-Keep": "yes",
	}, "")
	h := (*seen)[0].header
	for _, gone := range []string{"Proxy-Authorization", "X-Drop", "Te"} {
		if h.Get(gone) != "" {
			t.Errorf("%s reached the upstream", gone)
		}
	}
	if h.Get("X-Keep") != "yes" {
		t.Error("X-Keep was removed")
	}
}

func TestARedirectIsPassedBackUnfollowed(t *testing.T) {
	upstream, seen := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.example/steal", http.StatusFound)
	})
	proxy := newProxy(t, upstream, KeyPlace{Header: "xi-api-key"}, backendWith(map[string]string{"lgk_one": "elevenlabs"}))
	resp, _ := send(t, "GET", proxy.URL+"/c/elevenlabs/v1/voices", map[string]string{"xi-api-key": "lgk_one"}, "")
	if resp.StatusCode != 302 || resp.Header.Get("Location") != "https://elsewhere.example/steal" || len(*seen) != 1 {
		t.Fatalf("status %d location %q calls %d", resp.StatusCode, resp.Header.Get("Location"), len(*seen))
	}
}

func TestAnAbsoluteRequestURIIsRefused(t *testing.T) {
	upstream, seen := newUpstream(t, nil)
	proxy := newProxy(t, upstream, KeyPlace{Header: "xi-api-key"}, backendWith(map[string]string{"lgk_one": "elevenlabs"}))
	// Sent as a forward proxy request: the request line names another host.
	proxyURL, _ := url.Parse(proxy.URL)
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	req, _ := http.NewRequest("GET", "http://example.com/c/elevenlabs/v1/voices", nil)
	req.Header.Set("xi-api-key", "lgk_one")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 || len(*seen) != 0 {
		t.Fatalf("status %d calls %d", resp.StatusCode, len(*seen))
	}
}

func TestAnUnmatchedRouteIsRefusedWithItsMethodAndPath(t *testing.T) {
	upstream, seen := newUpstream(t, nil)
	proxy := newProxy(t, upstream, KeyPlace{Header: "xi-api-key"}, backendWith(map[string]string{"lgk_one": "elevenlabs"}))
	resp, body := send(t, "PUT", proxy.URL+"/c/elevenlabs/v9/anything", map[string]string{"xi-api-key": "lgk_one"}, "")
	var got map[string]string
	_ = json.Unmarshal([]byte(body), &got)
	if resp.StatusCode != 403 || got["method"] != "PUT" || got["path"] != "/v9/anything" || len(*seen) != 0 {
		t.Fatalf("status %d body %s calls %d", resp.StatusCode, body, len(*seen))
	}
}

func TestADeniedOrPendingUseIsNotForwarded(t *testing.T) {
	upstream, seen := newUpstream(t, nil)
	backend := backendWith(map[string]string{"lgk_one": "elevenlabs"})
	proxy := newProxy(t, upstream, KeyPlace{Header: "xi-api-key"}, backend)

	backend.decide = func(ProxyUse) Decision { return Decision{Reason: "elevenlabs does not allow account.manage"} }
	resp, body := send(t, "GET", proxy.URL+"/c/elevenlabs/v1/user", map[string]string{"xi-api-key": "lgk_one"}, "")
	var denied map[string]string
	_ = json.Unmarshal([]byte(body), &denied)
	if resp.StatusCode != 403 || denied["operation"] != "account.manage" || denied["resource"] != "/v1/user" && !strings.HasSuffix(denied["resource"], "/v1/user") {
		t.Fatalf("denied: status %d body %s", resp.StatusCode, body)
	}

	backend.decide = func(ProxyUse) Decision { return Decision{Pending: true, ApprovalID: "ap-12345678"} }
	resp, body = send(t, "GET", proxy.URL+"/c/elevenlabs/v1/voices", map[string]string{"xi-api-key": "lgk_one"}, "")
	var pending map[string]string
	_ = json.Unmarshal([]byte(body), &pending)
	if resp.StatusCode != 403 || pending["approval"] != "ap-12345678" || pending["status"] != "pending" {
		t.Fatalf("pending: status %d body %s", resp.StatusCode, body)
	}
	if len(*seen) != 0 {
		t.Fatalf("upstream was called %d times", len(*seen))
	}
}

func TestATooLargeMeteredBodyIs413(t *testing.T) {
	upstream, seen := newUpstream(t, nil)
	proxy := newProxy(t, upstream, KeyPlace{Header: "xi-api-key"}, backendWith(map[string]string{"lgk_one": "elevenlabs"}))
	resp, _ := send(t, "POST", proxy.URL+"/c/elevenlabs/v1/text-to-speech/abc", map[string]string{"xi-api-key": "lgk_one"}, `{"text":"`+strings.Repeat("a", MaxMeteredBody)+`"}`)
	if resp.StatusCode != 413 || len(*seen) != 0 {
		t.Fatalf("status %d calls %d", resp.StatusCode, len(*seen))
	}
}

func TestBytesAreCountedWhileTheResponseStreams(t *testing.T) {
	upstream, _ := newUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		flusher := w.(http.Flusher)
		for i := 0; i < 4; i++ {
			_, _ = io.WriteString(w, strings.Repeat("x", 1000))
			flusher.Flush() // chunked: no Content-Length
		}
	})
	backend := backendWith(map[string]string{"lgk_one": "elevenlabs"})
	proxy := newProxy(t, upstream, KeyPlace{Header: "xi-api-key"}, backend)
	_, body := send(t, "POST", proxy.URL+"/c/elevenlabs/v1/text-to-speech/abc", map[string]string{"xi-api-key": "lgk_one"}, `{"text":"hi"}`)
	if len(body) != 4000 {
		t.Fatalf("body length %d", len(body))
	}
	if len(backend.measured) != 1 || backend.measured[0]["bytes"] != 4000 {
		t.Fatalf("measured = %v", backend.measured)
	}
	if backend.uses[0].Amounts["requests"] != 1 || backend.uses[0].Amounts["bytes"] != int64(len(`{"text":"hi"}`)) {
		t.Fatalf("use amounts = %v", backend.uses[0].Amounts)
	}
}

func TestASelfSignedUpstreamIsRefusedWithoutItsCA(t *testing.T) {
	upstream, seen := newUpstream(t, nil)
	u, _ := url.Parse(upstream.URL)
	p := &HTTPProxy{
		Credentials: map[string]ProxyCredential{"elevenlabs": {Name: "elevenlabs", Upstream: u, Key: KeyPlace{Header: "xi-api-key"}, Inspector: elevenLabsLike()}},
		Backend:     backendWith(map[string]string{"lgk_one": "elevenlabs"}),
	}
	proxy := httptest.NewServer(p)
	defer proxy.Close()
	resp, _ := send(t, "GET", proxy.URL+"/c/elevenlabs/v1/voices", map[string]string{"xi-api-key": "lgk_one"}, "")
	if resp.StatusCode != 502 || len(*seen) != 0 {
		t.Fatalf("status %d calls %d", resp.StatusCode, len(*seen))
	}
}

func TestAnUnknownCredentialIs404(t *testing.T) {
	upstream, _ := newUpstream(t, nil)
	proxy := newProxy(t, upstream, KeyPlace{Header: "xi-api-key"}, backendWith(nil))
	for _, path := range []string{"/c/nope/v1/voices", "/", "/other"} {
		resp, _ := send(t, "GET", proxy.URL+path, nil, "")
		if resp.StatusCode != 404 {
			t.Errorf("%s: status %d", path, resp.StatusCode)
		}
	}
}
