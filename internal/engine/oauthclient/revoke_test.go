package oauthclient_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/cpave3/legato/internal/engine/oauthclient"
)

func TestRevokeSendsTheTokenWithItsHint(t *testing.T) {
	f := newFakeServer(t)
	var got url.Values
	f.mux.HandleFunc("POST /revoke", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.PostForm
		w.WriteHeader(http.StatusOK)
	})
	md := &oauthclient.Metadata{RevocationEndpoint: f.URL + "/revoke"}
	if err := oauthclient.Revoke(context.Background(), f.Client(), md, "client-1", "refresh-1", "refresh_token", oauthclient.WithClientSecret("s")); err != nil {
		t.Fatal(err)
	}
	if got.Get("token") != "refresh-1" || got.Get("token_type_hint") != "refresh_token" || got.Get("client_id") != "client-1" || got.Get("client_secret") != "s" {
		t.Fatalf("revocation form %v", got)
	}
}

func TestRevokeWithoutAnEndpointIsUnsupported(t *testing.T) {
	err := oauthclient.Revoke(context.Background(), nil, &oauthclient.Metadata{}, "c", "t", "refresh_token")
	if err != oauthclient.ErrRevocationUnsupported {
		t.Fatalf("no endpoint: %v", err)
	}
}

func TestRevokeReportsARefusalWithoutTheToken(t *testing.T) {
	f := newFakeServer(t)
	f.mux.HandleFunc("POST /revoke", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unsupported_token_type"})
	})
	err := oauthclient.Revoke(context.Background(), f.Client(), &oauthclient.Metadata{RevocationEndpoint: f.URL + "/revoke"}, "c", "secret-token", "refresh_token")
	if err == nil || contains(err.Error(), "secret-token") {
		t.Fatalf("refusal: %v", err)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
