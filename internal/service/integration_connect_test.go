package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// fakeArgusAuth is Argus's discovery, registration, authorization and token
// endpoints, checking PKCE and the redirect as Argus does.
func fakeArgusAuth(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	writeJSON := func(w http.ResponseWriter, status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	var challenge, redirect, resource string
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"resource": srv.URL + "/mcp", "authorization_servers": []string{srv.URL}})
	})
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"issuer": srv.URL, "authorization_endpoint": srv.URL + "/authorize", "token_endpoint": srv.URL + "/token",
			"registration_endpoint": srv.URL + "/register", "code_challenge_methods_supported": []string{"S256"}})
	})
	mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if uris, _ := body["redirect_uris"].([]any); len(uris) != 1 || uris[0] != "http://127.0.0.1/callback" {
			writeJSON(w, 400, map[string]any{"error": "invalid_redirect_uri"})
			return
		}
		writeJSON(w, 201, map[string]any{"client_id": "legato-client"})
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("scope") != "argus:reply" {
			writeJSON(w, 400, map[string]any{"error": "invalid_scope"})
			return
		}
		challenge, redirect, resource = q.Get("code_challenge"), q.Get("redirect_uri"), q.Get("resource")
		to, _ := url.Parse(redirect)
		to.RawQuery = url.Values{"code": {"code-1"}, "state": {q.Get("state")}}.Encode()
		http.Redirect(w, r, to.String(), http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge || r.PostForm.Get("redirect_uri") != redirect ||
			r.PostForm.Get("resource") != srv.URL+"/mcp" || resource != srv.URL+"/mcp" {
			writeJSON(w, 400, map[string]any{"error": "invalid_grant"})
			return
		}
		writeJSON(w, 200, map[string]any{"access_token": "a", "refresh_token": "refresh-from-connect", "expires_in": 3600, "token_type": "Bearer"})
	})
	return srv
}

func TestConnectingToArgusKeepsTheRegistrationAndTheRefreshToken(t *testing.T) {
	srv := fakeArgusAuth(t)
	db := newIntegrationStore(t)
	secrets := &memorySecrets{values: map[string]string{}}
	connector := NewIntegrationConnector(db, secrets, srv.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pending, err := connector.Begin(ctx, "argus", srv.URL, "Legato (test)")
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	go func() {
		// The browser follows Argus's redirect to Legato's listener.
		if resp, err := http.Get(pending.URL()); err == nil {
			resp.Body.Close()
		}
	}()
	if err := pending.Finish(ctx); err != nil {
		t.Fatal(err)
	}
	in, err := db.GetIntegration(ctx, "argus")
	if err != nil || in.ClientID != "legato-client" || in.Resource != srv.URL+"/mcp" || in.TokenEndpoint != srv.URL+"/token" || in.State != store.IntegrationConnected {
		t.Fatalf("integration = %+v, %v", in, err)
	}
	if v, _ := secrets.Read(ctx, ArgusRefreshSecret); v != "refresh-from-connect" {
		t.Fatalf("refresh = %q", v)
	}
	if err := connector.Disconnect(ctx, "argus"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetIntegration(ctx, "argus"); err == nil {
		t.Fatal("still connected")
	}
}
