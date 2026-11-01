package oauthclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/oauthclient"
)

// tokenEndpoint answers every token request with answer, recording the form.
func tokenEndpoint(t *testing.T, f *fakeServer, status int, answer map[string]any) *url.Values {
	t.Helper()
	got := &url.Values{}
	f.mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		*got = r.PostForm
		writeJSON(w, status, answer)
	})
	return got
}

func assertForm(t *testing.T, got url.Values, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("form = %v, want exactly %v", got, want)
	}
	for k, v := range want {
		if got.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, got.Get(k), v)
		}
	}
}

func TestExchangeSendsTheCodeAndVerifier(t *testing.T) {
	f := newFakeServer(t)
	md := f.metadata(t)
	form := tokenEndpoint(t, f, http.StatusOK, map[string]any{
		"access_token": "access-1", "token_type": "Bearer", "expires_in": 3600,
		"refresh_token": "refresh-1", "scope": "argus:reply",
	})

	before := time.Now()
	tok, err := oauthclient.Exchange(context.Background(), f.Client(), md,
		"client-1", "code-1", "verifier-1", "http://127.0.0.1:5555/callback")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	assertForm(t, *form, map[string]string{
		"grant_type":    "authorization_code",
		"code":          "code-1",
		"redirect_uri":  "http://127.0.0.1:5555/callback",
		"client_id":     "client-1",
		"code_verifier": "verifier-1",
		"resource":      md.Resource,
	})
	if tok.AccessToken != "access-1" || tok.RefreshToken != "refresh-1" || tok.Scope != "argus:reply" {
		t.Fatalf("token = %+v", tok)
	}
	if tok.ExpiresAt.Before(before.Add(time.Hour)) || tok.ExpiresAt.After(time.Now().Add(time.Hour)) {
		t.Fatalf("ExpiresAt = %v, want an hour from now", tok.ExpiresAt)
	}
}

func TestExchangeWithoutExpiryLeavesExpiresAtZero(t *testing.T) {
	f := newFakeServer(t)
	tokenEndpoint(t, f, http.StatusOK, map[string]any{"access_token": "a", "token_type": "Bearer"})

	tok, err := oauthclient.Exchange(context.Background(), f.Client(), f.metadata(t), "c", "code", "v", "r")
	if err != nil {
		t.Fatal(err)
	}
	if !tok.ExpiresAt.IsZero() {
		t.Fatalf("ExpiresAt = %v", tok.ExpiresAt)
	}
}

func TestExchangeNeedsAnAccessToken(t *testing.T) {
	f := newFakeServer(t)
	tokenEndpoint(t, f, http.StatusOK, map[string]any{"token_type": "Bearer"})

	if _, err := oauthclient.Exchange(context.Background(), f.Client(), f.metadata(t), "c", "code", "v", "r"); err == nil {
		t.Fatal("Exchange succeeded without an access token")
	}
}

func TestExchangeReturnsInvalidGrant(t *testing.T) {
	f := newFakeServer(t)
	tokenEndpoint(t, f, http.StatusBadRequest, map[string]any{"error": "invalid_grant"})

	_, err := oauthclient.Exchange(context.Background(), f.Client(), f.metadata(t), "c", "code", "v", "r")
	if !errors.Is(err, oauthclient.ErrInvalidGrant) {
		t.Fatalf("err = %v, want ErrInvalidGrant", err)
	}
}

func TestRefreshSendsTheRefreshToken(t *testing.T) {
	f := newFakeServer(t)
	md := f.metadata(t)
	form := tokenEndpoint(t, f, http.StatusOK, map[string]any{
		"access_token": "access-2", "token_type": "Bearer", "expires_in": 60, "refresh_token": "refresh-2",
	})

	tok, err := oauthclient.Refresh(context.Background(), f.Client(), md, "client-1", "refresh-1")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	assertForm(t, *form, map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": "refresh-1",
		"client_id":     "client-1",
		"resource":      md.Resource,
	})
	if tok.AccessToken != "access-2" || tok.RefreshToken != "refresh-2" {
		t.Fatalf("token = %+v, want the rotated refresh token", tok)
	}
}

func TestRefreshKeepsTheOldRefreshTokenWhenNoneIsReturned(t *testing.T) {
	f := newFakeServer(t)
	tokenEndpoint(t, f, http.StatusOK, map[string]any{"access_token": "access-2", "token_type": "Bearer"})

	tok, err := oauthclient.Refresh(context.Background(), f.Client(), f.metadata(t), "client-1", "refresh-1")
	if err != nil {
		t.Fatal(err)
	}
	if tok.RefreshToken != "refresh-1" {
		t.Fatalf("refresh token = %q, want the old one", tok.RefreshToken)
	}
}

func TestRefreshTellsADeadTokenFromAFailingServer(t *testing.T) {
	t.Run("invalid_grant", func(t *testing.T) {
		f := newFakeServer(t)
		tokenEndpoint(t, f, http.StatusBadRequest, map[string]any{
			"error": "invalid_grant", "error_description": "refresh token was rotated",
		})
		_, err := oauthclient.Refresh(context.Background(), f.Client(), f.metadata(t), "c", "refresh-1")
		var oerr *oauthclient.Error
		if !errors.Is(err, oauthclient.ErrInvalidGrant) || !errors.As(err, &oerr) ||
			oerr.Status != http.StatusBadRequest || oerr.Description != "refresh token was rotated" {
			t.Fatalf("err = %#v, want an invalid_grant *Error", err)
		}
	})
	t.Run("other OAuth error", func(t *testing.T) {
		f := newFakeServer(t)
		tokenEndpoint(t, f, http.StatusBadRequest, map[string]any{"error": "invalid_target"})
		_, err := oauthclient.Refresh(context.Background(), f.Client(), f.metadata(t), "c", "refresh-1")
		var oerr *oauthclient.Error
		if errors.Is(err, oauthclient.ErrInvalidGrant) || !errors.As(err, &oerr) || oerr.Code != "invalid_target" {
			t.Fatalf("err = %v, want invalid_target and not invalid_grant", err)
		}
	})
	t.Run("server error", func(t *testing.T) {
		f := newFakeServer(t)
		md := f.metadata(t)
		f.mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "upstream refresh-1 broke", http.StatusServiceUnavailable)
		})
		_, err := oauthclient.Refresh(context.Background(), f.Client(), md, "c", "refresh-1")
		var oerr *oauthclient.Error
		if errors.Is(err, oauthclient.ErrInvalidGrant) || !errors.As(err, &oerr) || oerr.Status != http.StatusServiceUnavailable {
			t.Fatalf("err = %v, want a 503 that is not invalid_grant", err)
		}
		if strings.Contains(err.Error(), "refresh-1") {
			t.Fatalf("err = %q carries the token", err)
		}
	})
	t.Run("unreachable", func(t *testing.T) {
		f := newFakeServer(t)
		md := f.metadata(t)
		f.Close()
		_, err := oauthclient.Refresh(context.Background(), f.Client(), md, "c", "refresh-1")
		var oerr *oauthclient.Error
		if err == nil || errors.Is(err, oauthclient.ErrInvalidGrant) || errors.As(err, &oerr) {
			t.Fatalf("err = %v, want a network error", err)
		}
		if strings.Contains(err.Error(), "refresh-1") {
			t.Fatalf("err = %q carries the token", err)
		}
	})
}
