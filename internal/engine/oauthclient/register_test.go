package oauthclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/cpave3/legato/internal/engine/oauthclient"
)

func TestRegisterAsAPublicClient(t *testing.T) {
	f := newFakeServer(t)
	var got map[string]any
	f.mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		writeJSON(w, http.StatusCreated, map[string]any{"client_id": "client-1", "client_name": "x"})
	})

	reg, err := oauthclient.Register(context.Background(), f.Client(), f.metadata(t), oauthclient.RegistrationRequest{
		ClientName:  "Legato (host)",
		RedirectURI: "http://127.0.0.1/callback",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if reg.ClientID != "client-1" {
		t.Fatalf("client id = %q", reg.ClientID)
	}
	want := map[string]any{
		"client_name":                "Legato (host)",
		"redirect_uris":              []any{"http://127.0.0.1/callback"},
		"token_endpoint_auth_method": "none",
		"grant_types":                []any{"authorization_code", "refresh_token"},
		"response_types":             []any{"code"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("request = %v\nwant      %v", got, want)
	}
}

func TestRegisterReturnsTheServersError(t *testing.T) {
	f := newFakeServer(t)
	f.mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid_redirect_uri", "error_description": "not allowed",
		})
	})

	_, err := oauthclient.Register(context.Background(), f.Client(), f.metadata(t), oauthclient.RegistrationRequest{
		ClientName: "x", RedirectURI: "http://127.0.0.1/callback",
	})
	var oerr *oauthclient.Error
	if !errors.As(err, &oerr) {
		t.Fatalf("err = %v, want *oauthclient.Error", err)
	}
	if oerr.Code != "invalid_redirect_uri" || oerr.Description != "not allowed" || oerr.Status != http.StatusBadRequest {
		t.Fatalf("error = %+v", *oerr)
	}
}

func TestRegisterNeedsARegistrationEndpoint(t *testing.T) {
	f := newFakeServer(t)
	delete(f.asMD, "registration_endpoint")

	_, err := oauthclient.Register(context.Background(), f.Client(), f.metadata(t), oauthclient.RegistrationRequest{
		ClientName: "x", RedirectURI: "http://127.0.0.1/callback",
	})
	if err == nil {
		t.Fatal("Register succeeded without a registration endpoint")
	}
}

func TestRegisterNeedsAClientID(t *testing.T) {
	f := newFakeServer(t)
	f.mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusCreated, map[string]any{})
	})

	_, err := oauthclient.Register(context.Background(), f.Client(), f.metadata(t), oauthclient.RegistrationRequest{
		ClientName: "x", RedirectURI: "http://127.0.0.1/callback",
	})
	if err == nil {
		t.Fatal("Register succeeded without a client id")
	}
}
