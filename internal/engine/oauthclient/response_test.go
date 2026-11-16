package oauthclient_test

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/oauthclient"
)

func TestAuthorizationAcceptsANativeResponseOnlyForItsStateAndIssuer(t *testing.T) {
	f := newFakeServer(t)
	a, err := oauthclient.StartRedirectAuthorization(f.metadata(t), "client", "read:tasks", "http://127.0.0.1:34567/callback")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.AcceptResponse(oauthclient.AuthorizationResponse{Code: "sentinel", State: a.State(), Issuer: "https://another-issuer.example"}); !errors.Is(err, oauthclient.ErrIssuerMismatch) {
		t.Fatalf("wrong issuer: %v", err)
	}
	if err := a.AcceptResponse(oauthclient.AuthorizationResponse{Code: "sentinel", State: "another-state", Issuer: f.URL}); !errors.Is(err, oauthclient.ErrStateMismatch) {
		t.Fatalf("wrong state: %v", err)
	}
	if err := a.AcceptResponse(oauthclient.AuthorizationResponse{Code: "accepted-code", State: a.State(), Issuer: f.URL}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	code, err := a.Wait(ctx)
	if err != nil || code != "accepted-code" {
		t.Fatal("invalid native responses consumed the legitimate transaction")
	}
	if err := a.AcceptResponse(oauthclient.AuthorizationResponse{Code: "replay", State: a.State(), Issuer: f.URL}); !errors.Is(err, oauthclient.ErrAuthorizationUsed) {
		t.Fatalf("duplicate response: %v", err)
	}
}

func TestAuthorizationRequiresAnIssuerWhenAdvertised(t *testing.T) {
	f := newFakeServer(t)
	f.asMD["authorization_response_iss_parameter_supported"] = true
	a, err := oauthclient.StartRedirectAuthorization(f.metadata(t), "client", "read:tasks", "http://127.0.0.1:34567/callback")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Paste(a.RedirectURI() + "?" + url.Values{"code": {"code"}, "state": {a.State()}}.Encode()); !errors.Is(err, oauthclient.ErrIssuerMismatch) {
		t.Fatalf("missing advertised issuer: %v", err)
	}
	if err := a.AcceptResponse(oauthclient.AuthorizationResponse{Error: "access_denied", State: a.State(), Issuer: f.URL}); err == nil {
		t.Fatal("denial was not surfaced")
	}
}
