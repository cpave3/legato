// These are research characterizations, not acceptance tests for the proposed broker.
// GAP observations deliberately describe current behavior without implementing fixes.
package scenarios_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type secretMemory struct {
	mu     sync.Mutex
	values map[string]string
}

func (m *secretMemory) Read(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[key]
	if !ok {
		return "", errors.New("missing synthetic secret")
	}
	return v, nil
}
func (m *secretMemory) Write(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = value
	return nil
}
func (m *secretMemory) Remove(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, key)
	return nil
}

type registration struct {
	RedirectURIs    []string `json:"redirect_uris"`
	ApplicationType string   `json:"application_type"`
}
type issuedCode struct{ client, redirect, challenge, resource string }
type provider struct {
	srv                                          *httptest.Server
	mu                                           sync.Mutex
	registrations                                map[string]registration
	codes                                        map[string]issuedCode
	dcr, cimd, refuseRedirect, metadataReachable bool
	missingASMetadata                            bool
	tokenCalls                                   int
	metadataURL                                  string
}

func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func newProvider(t *testing.T) *provider {
	t.Helper()
	p := &provider{registrations: map[string]registration{}, codes: map[string]issuedCode{}, dcr: true, metadataReachable: true}
	p.srv = httptest.NewTLSServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.srv.Close)
	return p
}
func (p *provider) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch r.URL.Path {
	case "/.well-known/oauth-protected-resource":
		jsonResponse(w, 200, map[string]any{"resource": p.srv.URL + "/mcp", "authorization_servers": []string{p.srv.URL}})
	case "/.well-known/oauth-authorization-server":
		if p.missingASMetadata {
			http.NotFound(w, r)
			return
		}
		md := map[string]any{"issuer": p.srv.URL, "authorization_endpoint": p.srv.URL + "/authorize", "token_endpoint": p.srv.URL + "/token", "code_challenge_methods_supported": []string{"S256"}, "client_id_metadata_document_supported": p.cimd}
		if p.dcr {
			md["registration_endpoint"] = p.srv.URL + "/register"
		}
		jsonResponse(w, 200, md)
	case "/register":
		var reg registration
		if !p.dcr {
			jsonResponse(w, 404, map[string]string{"error": "unsupported"})
			return
		}
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&reg) != nil || len(reg.RedirectURIs) != 1 {
			jsonResponse(w, 400, map[string]string{"error": "invalid_client_metadata"})
			return
		}
		if p.refuseRedirect {
			jsonResponse(w, 400, map[string]string{"error": "invalid_redirect_uri"})
			return
		}
		id := fmt.Sprintf("synthetic-client-%d", len(p.registrations)+1)
		p.registrations[id] = reg
		jsonResponse(w, 201, map[string]string{"client_id": id, "token_endpoint_auth_method": "none"})
	case "/authorize":
		q := r.URL.Query()
		reg, ok := p.registrations[q.Get("client_id")]
		if !ok && p.cimd && q.Get("client_id") == p.metadataURL && p.metadataReachable {
			// This request is made by the simulated provider, not the browser.
			resp, err := p.srv.Client().Get(p.metadataURL)
			if err == nil {
				defer resp.Body.Close()
				var md struct {
					ClientID string `json:"client_id"`
					registration
				}
				if json.NewDecoder(resp.Body).Decode(&md) == nil && md.ClientID == p.metadataURL {
					reg, ok = md.registration, true
				}
			}
		}
		allowed := false
		for _, u := range reg.RedirectURIs {
			if u == q.Get("redirect_uri") {
				allowed = true
			}
		}
		if !ok || !allowed || q.Get("code_challenge_method") != "S256" || q.Get("resource") != p.srv.URL+"/mcp" {
			jsonResponse(w, 400, map[string]string{"error": "invalid_request"})
			return
		}
		code := fmt.Sprintf("synthetic-code-%d", len(p.codes)+1)
		p.codes[code] = issuedCode{q.Get("client_id"), q.Get("redirect_uri"), q.Get("code_challenge"), q.Get("resource")}
		dest, _ := url.Parse(q.Get("redirect_uri"))
		dest.RawQuery = url.Values{"code": {code}, "state": {q.Get("state")}, "iss": {p.srv.URL}}.Encode()
		http.Redirect(w, r, dest.String(), 302)
	case "/token":
		_ = r.ParseForm()
		record, ok := p.codes[r.PostForm.Get("code")]
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if !ok || record.client != r.PostForm.Get("client_id") || record.redirect != r.PostForm.Get("redirect_uri") || record.resource != r.PostForm.Get("resource") || record.challenge != base64.RawURLEncoding.EncodeToString(sum[:]) {
			jsonResponse(w, 400, map[string]string{"error": "invalid_grant"})
			return
		}
		delete(p.codes, r.PostForm.Get("code"))
		p.tokenCalls++
		jsonResponse(w, 200, map[string]any{"access_token": "synthetic-access", "refresh_token": "synthetic-refresh-" + record.client, "expires_in": 3600, "token_type": "Bearer"})
	default:
		http.NotFound(w, r)
	}
}

type fixture struct {
	p        *provider
	db       *store.Store
	secrets  *secretMemory
	settings *service.BurnliteSettings
	web      *httptest.Server
	browser  *http.Client
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{p: newProvider(t), secrets: &secretMemory{values: map[string]string{}}}
	var err error
	f.db, err = store.New(filepath.Join(t.TempDir(), "research.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Close() })
	f.settings = f.newSettings()
	s := server.New(nil, nil, nil, ":0")
	s.SetAuthToken("synthetic-operator-authority")
	s.SetBurnliteSettings(f.settings)
	f.web = httptest.NewTLSServer(s.Handler())
	t.Cleanup(f.web.Close)
	f.browser = f.web.Client()
	f.browser.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return f
}
func (f *fixture) newSettings() *service.BurnliteSettings {
	return service.NewBurnliteSettings(f.db, service.NewIntegrationConnector(f.db, f.secrets, f.p.srv.Client()), service.BurnliteSettingsOptions{})
}
func (f *fixture) connect(t *testing.T) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"url": f.p.srv.URL})
	req, _ := http.NewRequest("POST", f.web.URL+"/api/settings/integrations/burnlite/connect", strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer synthetic-operator-authority")
	resp, err := f.browser.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("connect status %d", resp.StatusCode)
	}
	var out struct {
		URL string `json:"authorize_url"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil || out.URL == "" {
		t.Fatal("missing authorization URL")
	}
	return out.URL
}
func (f *fixture) consent(t *testing.T, authURL string) string {
	t.Helper()
	resp, err := f.browser.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("consent status %d", resp.StatusCode)
	}
	return resp.Header.Get("Location")
}
func (f *fixture) callback(t *testing.T, location string) {
	t.Helper()
	resp, err := f.browser.Get(location)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 302 {
		t.Fatalf("callback status %d", resp.StatusCode)
	}
	u, _ := url.Parse(resp.Header.Get("Location"))
	if u.Query().Get("result") != "connected" {
		t.Fatal("callback did not connect")
	}
}
func clientID(t *testing.T, authURL string) string {
	t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("client_id")
}

func TestScenarioAutomaticDCRAndServerCallback(t *testing.T) {
	f := newFixture(t)
	authURL := f.connect(t)
	id := clientID(t, authURL)
	f.p.mu.Lock()
	reg := f.p.registrations[id]
	f.p.mu.Unlock()
	if len(reg.RedirectURIs) != 1 || reg.RedirectURIs[0] != f.web.URL+"/oauth/callback" {
		t.Fatal("server did not register its own callback")
	}
	// The simulated browser has no Authorization header on consent or callback.
	f.callback(t, f.consent(t, authURL))
	in, err := f.db.GetIntegration(context.Background(), "burnlite")
	if err != nil || in.ClientID != id {
		t.Fatal("server did not retain connection")
	}
	f.p.mu.Lock()
	calls := f.p.tokenCalls
	f.p.mu.Unlock()
	if calls != 1 {
		t.Fatal("expected exactly one server token exchange")
	}
	t.Log("WORKS: operator entered only MCP URL; Legato automatically sent redirect_uris in DCR, browser followed HTTPS callback without Legato bearer, server retained credentials")
}

func TestScenarioTLSProxyUsesInternalHTTPOriginToday(t *testing.T) {
	f := newFixture(t)
	s := server.New(nil, nil, nil, ":0")
	s.SetAuthToken("synthetic-operator-authority")
	s.SetBurnliteSettings(f.settings)
	req := httptest.NewRequest("POST", "http://internal-proxy:3080/api/settings/integrations/burnlite/connect", strings.NewReader(`{"url":"`+f.p.srv.URL+`"}`))
	req.Header.Set("Authorization", "Bearer synthetic-operator-authority")
	req.Header.Set("X-Forwarded-Host", "external.example.test")
	req.Header.Set("X-Forwarded-Proto", "https")
	out := httptest.NewRecorder()
	s.Handler().ServeHTTP(out, req)
	if out.Code != 200 {
		t.Fatalf("status %d", out.Code)
	}
	var body struct {
		URL string `json:"authorize_url"`
	}
	_ = json.Unmarshal(out.Body.Bytes(), &body)
	u, _ := url.Parse(body.URL)
	if u.Query().Get("redirect_uri") != "http://internal-proxy:3080/oauth/callback" {
		t.Fatal("baseline behavior changed; review gap")
	}
	t.Log("GAP: current route registers internal HTTP callback behind TLS termination; configured trusted origin is needed, not blind forwarding-header trust")
}

func TestScenarioCIMDOnlyFailsCurrentConnector(t *testing.T) {
	f := newFixture(t)
	f.p.dcr = false
	f.p.cimd = true
	_, err := f.settings.Begin(context.Background(), f.p.srv.URL, f.web.URL+"/oauth/callback")
	if err == nil || !strings.Contains(err.Error(), "offers no registration") {
		t.Fatal("expected current connector's DCR-only failure")
	}
	t.Log("GAP: current connector requires registration_endpoint despite advertised CIMD; automated metadata publication/selection must be explicit in plan")
}

func TestScenarioProviderRejectsRedirectDuringDCR(t *testing.T) {
	f := newFixture(t)
	f.p.refuseRedirect = true
	_, err := f.settings.Begin(context.Background(), f.p.srv.URL, f.web.URL+"/oauth/callback")
	if err == nil || !strings.Contains(err.Error(), "invalid_redirect_uri") {
		t.Fatal("provider redirect restriction did not reach caller")
	}
	t.Log("LIMIT: DCR can register automatically but cannot override provider redirect policy; show a specific recovery path, not a generic client-credentials form")
}

func TestScenarioReauthRegistersAgainToday(t *testing.T) {
	f := newFixture(t)
	first := f.connect(t)
	f.callback(t, f.consent(t, first))
	second := f.connect(t)
	if clientID(t, first) == clientID(t, second) {
		t.Fatal("baseline registration reuse changed")
	}
	t.Log("GAP: re-auth creates another registration today; persist issuer/redirect/client configuration and reuse it")
}

func TestScenarioOlderAttemptCanOverwriteNewerConnectionToday(t *testing.T) {
	f := newFixture(t)
	older := f.connect(t)
	newer := f.connect(t)
	oldReturn := f.consent(t, older)
	newReturn := f.consent(t, newer)
	f.callback(t, newReturn)
	f.callback(t, oldReturn)
	in, err := f.db.GetIntegration(context.Background(), "burnlite")
	if err != nil || in.ClientID != clientID(t, older) {
		t.Fatal("baseline older-attempt overwrite changed")
	}
	t.Log("GAP: older attempt can replace newer completed connection; generation check must occur at final activation")
}

func TestScenarioDisconnectDoesNotCancelPendingAttemptToday(t *testing.T) {
	f := newFixture(t)
	authURL := f.connect(t)
	back := f.consent(t, authURL)
	if err := f.settings.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.callback(t, back)
	if _, err := f.db.GetIntegration(context.Background(), "burnlite"); err != nil {
		t.Fatal("baseline pending attempt no longer reconnects")
	}
	t.Log("GAP: callback can reconnect after operator disconnects today; disconnect must invalidate pending and staged generations")
}

func TestScenarioCallbackOnSecondProcessLosesPendingStateToday(t *testing.T) {
	f := newFixture(t)
	authURL := f.connect(t)
	back := f.consent(t, authURL)
	if err := f.newSettings().Callback(context.Background(), back); !errors.Is(err, service.ErrUnknownConnection) {
		t.Fatal("expected process-local pending state gap")
	}
	f.p.mu.Lock()
	calls := f.p.tokenCalls
	f.p.mu.Unlock()
	if calls != 0 {
		t.Fatal("unknown callback unexpectedly exchanged code")
	}
	t.Log("GAP: a second service over the same store cannot finish the first process's attempt; durable transaction and owner routing are required")
}

func TestScenarioSDKCIMDPublishesRedirectWithoutDCR(t *testing.T) {
	for _, reachable := range []bool{true, false} {
		t.Run(fmt.Sprintf("provider_can_fetch_metadata_%t", reachable), func(t *testing.T) {
			p := newProvider(t)
			p.dcr = false
			p.cimd = true
			p.metadataReachable = reachable
			// This is a synthetic metadata publisher, not a Legato implementation.
			var publisher *httptest.Server
			publisher = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				jsonResponse(w, 200, map[string]any{"client_id": publisher.URL + "/client.json", "client_name": "research client", "redirect_uris": []string{publisher.URL + "/oauth/callback"}, "grant_types": []string{"authorization_code", "refresh_token"}, "response_types": []string{"code"}, "token_endpoint_auth_method": "none"})
			}))
			t.Cleanup(publisher.Close)
			p.metadataURL = publisher.URL + "/client.json"
			noFollow := p.srv.Client()
			noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			h, err := mcpauth.NewAuthorizationCodeHandler(&mcpauth.AuthorizationCodeHandlerConfig{
				ClientIDMetadataDocumentConfig: &mcpauth.ClientIDMetadataDocumentConfig{URL: p.metadataURL}, RedirectURL: publisher.URL + "/oauth/callback", Client: p.srv.Client(), AcceptUnadvertisedIss: true,
				AuthorizationCodeFetcher: func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
					resp, err := noFollow.Get(args.URL)
					if err != nil {
						return nil, err
					}
					defer resp.Body.Close()
					if resp.StatusCode != 302 {
						return nil, errors.New("synthetic provider could not resolve client metadata")
					}
					loc, err := resp.Location()
					if err != nil {
						return nil, err
					}
					return &mcpauth.AuthorizationResult{Code: loc.Query().Get("code"), State: loc.Query().Get("state"), Iss: loc.Query().Get("iss")}, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("POST", p.srv.URL+"/mcp", nil)
			challenge := &http.Response{StatusCode: 401, Header: http.Header{"Www-Authenticate": {`Bearer resource_metadata="` + p.srv.URL + `/.well-known/oauth-protected-resource"`}}, Body: http.NoBody}
			err = h.Authorize(context.Background(), req, challenge)
			if !reachable {
				if err == nil || !strings.Contains(err.Error(), "synthetic provider could not resolve client metadata") {
					t.Fatal("expected the metadata reachability failure, not an unrelated error")
				}
				p.mu.Lock()
				calls := p.tokenCalls
				p.mu.Unlock()
				if calls != 0 {
					t.Fatal("unresolved metadata unexpectedly led to a token exchange")
				}
				t.Log("LIMIT: callback construction alone does not help a CIMD-only provider that cannot fetch the metadata")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			source, err := h.TokenSource(context.Background())
			if err != nil || source == nil {
				t.Fatal("no SDK token source")
			}
			tok, err := source.Token()
			if err != nil || tok.AccessToken == "" {
				t.Fatal("no token from SDK CIMD exchange")
			}
			p.mu.Lock()
			count, calls := len(p.registrations), p.tokenCalls
			p.mu.Unlock()
			if count != 0 || calls != 1 {
				t.Fatal("CIMD unexpectedly used DCR or missed exchange")
			}
			t.Log("WORKS AT SDK BOUNDARY: provider fetched generated client metadata, matched exact callback and exchanged with PKCE; no DCR or manual redirect entry")
		})
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestScenarioPhoneLosesCallbackRouteAfterConsent(t *testing.T) {
	f := newFixture(t)
	authURL := f.connect(t)
	back := f.consent(t, authURL)
	base := f.browser.Transport
	blocked := errors.New("synthetic phone network cannot reach callback")
	f.browser.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/oauth/callback" {
			return nil, blocked
		}
		return base.RoundTrip(r)
	})
	if _, err := f.browser.Get(back); !errors.Is(err, blocked) {
		t.Fatal("expected injected phone reachability failure")
	}
	f.p.mu.Lock()
	calls := f.p.tokenCalls
	f.p.mu.Unlock()
	if calls != 0 {
		t.Fatal("server exchanged a code the phone never delivered")
	}
	if _, err := f.db.GetIntegration(context.Background(), "burnlite"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("connection exists despite undelivered callback")
	}
	t.Log("LIMIT: DCR and consent succeeded but injected phone route loss prevented callback and token exchange; provider never directly called Legato")
}

func TestScenarioSDKFallsBackToUnadvertisedEndpointsToday(t *testing.T) {
	f := newFixture(t)
	f.p.missingASMetadata = true
	h, err := mcpauth.NewAuthorizationCodeHandler(&mcpauth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &mcpauth.DynamicClientRegistrationConfig{Metadata: &oauthex.ClientRegistrationMetadata{RedirectURIs: []string{f.web.URL + "/oauth/callback"}}},
		RedirectURL:                     f.web.URL + "/oauth/callback", Client: f.p.srv.Client(), AcceptUnadvertisedIss: true,
		AuthorizationCodeFetcher: func(ctx context.Context, args *mcpauth.AuthorizationArgs) (*mcpauth.AuthorizationResult, error) {
			resp, err := f.browser.Get(args.URL)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != 302 {
				return nil, errors.New("unexpected synthetic consent status")
			}
			loc, err := resp.Location()
			if err != nil {
				return nil, err
			}
			return &mcpauth.AuthorizationResult{Code: loc.Query().Get("code"), State: loc.Query().Get("state"), Iss: loc.Query().Get("iss")}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	challenge := &http.Response{StatusCode: 401, Header: http.Header{"Www-Authenticate": {`Bearer resource_metadata="` + f.p.srv.URL + `/.well-known/oauth-protected-resource"`}}, Body: http.NoBody}
	if err := h.Authorize(context.Background(), httptest.NewRequest("POST", f.p.srv.URL+"/mcp", nil), challenge); err != nil {
		t.Fatal(err)
	}
	f.p.mu.Lock()
	registrations, calls := len(f.p.registrations), f.p.tokenCalls
	f.p.mu.Unlock()
	if registrations != 1 || calls != 1 {
		t.Fatal("SDK no longer performs the characterized legacy flow")
	}
	t.Log("GAP AT SDK BOUNDARY: absent authorization-server metadata still led to guessed /register, /authorize and /token requests; strict discovery preflight must prevent this downgrade")
}
