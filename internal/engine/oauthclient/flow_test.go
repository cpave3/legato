package oauthclient_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/oauthclient"
)

// TestTheWholeFlow follows discovery, registration, authorization through
// the browser and the code exchange, with the server checking the PKCE
// binding and the redirect as a real one would.
func TestTheWholeFlow(t *testing.T) {
	f := newFakeServer(t)
	var challenge, redirect string
	f.mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{"client_id": "client-1"})
	})
	f.mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		challenge, redirect = q.Get("code_challenge"), q.Get("redirect_uri")
		to, _ := url.Parse(redirect)
		to.RawQuery = url.Values{"code": {"code-1"}, "state": {q.Get("state")}}.Encode()
		http.Redirect(w, r, to.String(), http.StatusFound)
	})
	f.mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge ||
			r.PostForm.Get("redirect_uri") != redirect || r.PostForm.Get("code") != "code-1" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": "access-1", "token_type": "Bearer", "expires_in": 3600, "refresh_token": "refresh-1",
		})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	md, err := oauthclient.Discover(ctx, f.Client(), f.URL)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := oauthclient.Register(ctx, f.Client(), md, oauthclient.RegistrationRequest{
		ClientName: "test", RedirectURI: "http://127.0.0.1/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := oauthclient.StartAuthorization(md, reg.ClientID, "argus:reply")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	go func() {
		// The browser: open the address and follow the redirect.
		if resp, err := http.Get(a.URL()); err == nil {
			resp.Body.Close()
		}
	}()
	code, err := a.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := oauthclient.Exchange(ctx, f.Client(), md, reg.ClientID, code, a.Verifier(), a.RedirectURI())
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if tok.AccessToken != "access-1" || tok.RefreshToken != "refresh-1" {
		t.Fatalf("token = %+v", tok)
	}
}
