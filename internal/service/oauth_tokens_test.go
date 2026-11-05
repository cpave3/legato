package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// fakeOAuth is an authorization server like Burnlite's: any registered
// redirect, the scope it was asked for, and refresh tokens that rotate.
type fakeOAuth struct {
	srv        *httptest.Server
	mu         sync.Mutex
	registered []string
	scope      string
	refresh    string // the refresh token that works now
	refreshes  int
	refuseAll  bool
	issued     int
	// confidential: like Burnlite, every registered client gets a secret,
	// which the token endpoint needs.
	confidential bool
	secretsSeen  []string
}

func newFakeOAuth(t *testing.T) *fakeOAuth {
	t.Helper()
	f := &fakeOAuth{}
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	srv := f.srv
	writeJSON := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"resource": srv.URL + "/api/llm/mcp", "authorization_servers": []string{srv.URL}})
	})
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"issuer": srv.URL, "authorization_endpoint": srv.URL + "/authorize", "token_endpoint": srv.URL + "/token",
			"registration_endpoint": srv.URL + "/register", "code_challenge_methods_supported": []string{"S256"}})
	})
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RedirectURIs []string `json:"redirect_uris"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.registered = body.RedirectURIs
		confidential := f.confidential
		f.mu.Unlock()
		reg := map[string]any{"client_id": "legato-client"}
		if confidential {
			reg["client_secret"] = "client-secret-1"
		}
		writeJSON(w, 201, reg)
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.mu.Lock()
		f.scope = q.Get("scope")
		f.mu.Unlock()
		to, _ := url.Parse(q.Get("redirect_uri"))
		to.RawQuery = url.Values{"code": {"code-1"}, "state": {q.Get("state")}}.Encode()
		http.Redirect(w, r, to.String(), http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		f.secretsSeen = append(f.secretsSeen, r.PostForm.Get("client_secret"))
		if f.confidential && r.PostForm.Get("client_secret") != "client-secret-1" {
			writeJSON(w, 401, map[string]any{"error": "invalid_client", "error_description": "Invalid client credentials"})
			return
		}
		if r.PostForm.Get("grant_type") == "refresh_token" {
			f.refreshes++
			if f.refuseAll || r.PostForm.Get("refresh_token") != f.refresh {
				writeJSON(w, 400, map[string]any{"error": "invalid_grant"})
				return
			}
		}
		f.issued++
		f.refresh = fmt.Sprintf("refresh-%d", f.issued)
		writeJSON(w, 200, map[string]any{"access_token": fmt.Sprintf("access-%d", f.issued), "refresh_token": f.refresh, "expires_in": 3600, "token_type": "Bearer"})
	})
	return f
}

// connectBurnlite connects as the web UI does: the browser comes back to
// Legato's own callback address, which hands Legato the address it was sent to.
func connectBurnlite(t *testing.T, f *fakeOAuth, db *store.Store, secrets IntegrationSecrets) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	callback := "https://legato.local:3080/oauth/callback"
	pending, err := NewIntegrationConnector(db, secrets, f.srv.Client()).BeginWith(ctx, ConnectRequest{
		Name: "burnlite", URL: f.srv.URL, ClientName: "Legato (test)", Scope: "read:tasks write:tasks", RedirectURI: callback,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	// The browser opens Burnlite's page and is sent back to Legato.
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noFollow.Get(pending.URL())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	back := resp.Header.Get("Location")
	if !strings.HasPrefix(back, callback+"?") || !strings.Contains(back, "state="+url.QueryEscape(pending.State())) {
		t.Fatalf("sent back to %q (state %q)", back, pending.State())
	}
	if err := pending.Paste(back); err != nil {
		t.Fatal(err)
	}
	if err := pending.Finish(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestConnectingThroughLegatosCallbackKeepsTheConnection(t *testing.T) {
	f := newFakeOAuth(t)
	db := newIntegrationStore(t)
	secrets := &memorySecrets{values: map[string]string{}}
	connectBurnlite(t, f, db, secrets)

	if len(f.registered) != 1 || f.registered[0] != "https://legato.local:3080/oauth/callback" || f.scope != "read:tasks write:tasks" {
		t.Errorf("registered %v, scope %q", f.registered, f.scope)
	}
	in, err := db.GetIntegration(context.Background(), "burnlite")
	if err != nil || in.URL != f.srv.URL || in.ClientID != "legato-client" || in.State != store.IntegrationConnected {
		t.Fatalf("integration = %+v, %v", in, err)
	}
	if v, _ := secrets.Read(context.Background(), IntegrationRefreshSecret("burnlite")); v != "refresh-1" {
		t.Errorf("refresh = %q", v)
	}
	if IntegrationRefreshSecret("argus") != ArgusRefreshSecret {
		t.Error("Argus's refresh token moved")
	}
}

func TestOAuthTokensRefreshKeepTheRotatedTokenAndReuseTheAccessToken(t *testing.T) {
	f := newFakeOAuth(t)
	db := newIntegrationStore(t)
	secrets := &memorySecrets{values: map[string]string{}}
	connectBurnlite(t, f, db, secrets)
	tokens := NewOAuthTokens(db, secrets, f.srv.Client(), "burnlite")
	ctx := context.Background()

	first, err := tokens.Token(ctx, false)
	if err != nil || first != "access-2" {
		t.Fatalf("token = %q, %v", first, err)
	}
	if again, _ := tokens.Token(ctx, false); again != first || f.refreshes != 1 {
		t.Errorf("token %q after %d refreshes; want the one in hand", again, f.refreshes)
	}
	if v, _ := secrets.Read(ctx, IntegrationRefreshSecret("burnlite")); v != "refresh-2" {
		t.Errorf("kept refresh = %q, want the rotated one", v)
	}
	if forced, _ := tokens.Token(ctx, true); forced != "access-3" {
		t.Errorf("forced = %q", forced)
	}
}

func TestARefusedRefreshTokenEndsTheConnection(t *testing.T) {
	f := newFakeOAuth(t)
	db := newIntegrationStore(t)
	secrets := &memorySecrets{values: map[string]string{}}
	connectBurnlite(t, f, db, secrets)
	f.refuseAll = true
	ctx := context.Background()

	_, err := NewOAuthTokens(db, secrets, f.srv.Client(), "burnlite").Token(ctx, false)
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("err = %v", err)
	}
	if in, _ := db.GetIntegration(ctx, "burnlite"); in.State != store.IntegrationDisconnected {
		t.Errorf("state = %s", in.State)
	}
	// Not connected at all.
	if _, err := NewOAuthTokens(db, secrets, f.srv.Client(), "nothing").Token(ctx, false); !errors.Is(err, ErrNotConnected) {
		t.Errorf("err = %v", err)
	}
}

func TestAServerThatGivesASecretGetsItBackOnEveryTokenRequest(t *testing.T) {
	f := newFakeOAuth(t)
	f.confidential = true
	db := newIntegrationStore(t)
	secrets := &memorySecrets{values: map[string]string{}}
	connectBurnlite(t, f, db, secrets)
	ctx := context.Background()

	if v, _ := secrets.Read(ctx, IntegrationClientSecret("burnlite")); v != "client-secret-1" {
		t.Errorf("kept client secret = %q", v)
	}
	if token, err := NewOAuthTokens(db, secrets, f.srv.Client(), "burnlite").Token(ctx, false); err != nil || token != "access-2" {
		t.Fatalf("token = %q, %v", token, err)
	}
	for i, sent := range f.secretsSeen {
		if sent != "client-secret-1" {
			t.Errorf("token request %d sent secret %q", i, sent)
		}
	}
}

func TestAPublicClientSendsNoSecret(t *testing.T) {
	f := newFakeOAuth(t)
	db := newIntegrationStore(t)
	secrets := &memorySecrets{values: map[string]string{}}
	connectBurnlite(t, f, db, secrets)
	if _, err := NewOAuthTokens(db, secrets, f.srv.Client(), "burnlite").Token(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, kept := secrets.values[IntegrationClientSecret("burnlite")]; kept {
		t.Error("a client secret was kept for a public client")
	}
	for i, sent := range f.secretsSeen {
		if sent != "" {
			t.Errorf("token request %d sent secret %q", i, sent)
		}
	}
}
