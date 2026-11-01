package oauthclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/oauthclient"
)

// fakeServer is a resource server that is also its own authorization
// server, as Argus is. Handlers can be replaced per test.
type fakeServer struct {
	*httptest.Server
	mux        *http.ServeMux
	resourceMD map[string]any
	asMD       map[string]any
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{mux: http.NewServeMux()}
	f.Server = httptest.NewServer(f.mux)
	t.Cleanup(f.Close)
	f.resourceMD = map[string]any{
		"resource":              f.URL + "/mcp",
		"authorization_servers": []string{f.URL},
	}
	f.asMD = map[string]any{
		"issuer":                           f.URL,
		"authorization_endpoint":           f.URL + "/authorize",
		"token_endpoint":                   f.URL + "/token",
		"registration_endpoint":            f.URL + "/register",
		"code_challenge_methods_supported": []string{"S256"},
	}
	f.mux.HandleFunc("GET /.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, f.resourceMD)
	})
	f.mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, f.asMD)
	})
	return f
}

func (f *fakeServer) metadata(t *testing.T) *oauthclient.Metadata {
	t.Helper()
	md, err := oauthclient.Discover(context.Background(), f.Client(), f.URL)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	return md
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestDiscoverReadsBothMetadataDocuments(t *testing.T) {
	f := newFakeServer(t)

	md, err := oauthclient.Discover(context.Background(), f.Client(), f.URL)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := oauthclient.Metadata{
		Resource:              f.URL + "/mcp",
		Issuer:                f.URL,
		AuthorizationEndpoint: f.URL + "/authorize",
		TokenEndpoint:         f.URL + "/token",
		RegistrationEndpoint:  f.URL + "/register",
		CodeChallengeMethods:  []string{"S256"},
	}
	if md.Resource != want.Resource || md.Issuer != want.Issuer ||
		md.AuthorizationEndpoint != want.AuthorizationEndpoint ||
		md.TokenEndpoint != want.TokenEndpoint ||
		md.RegistrationEndpoint != want.RegistrationEndpoint ||
		strings.Join(md.CodeChallengeMethods, ",") != "S256" {
		t.Fatalf("metadata = %+v, want %+v", *md, want)
	}
}

func TestDiscoverUsesTheOriginOfTheGivenURL(t *testing.T) {
	f := newFakeServer(t)

	md, err := oauthclient.Discover(context.Background(), f.Client(), f.URL+"/some/page")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if md.TokenEndpoint != f.URL+"/token" {
		t.Fatalf("token endpoint = %q", md.TokenEndpoint)
	}
}

func TestDiscoverKeepsTheResourceExactlyAsGiven(t *testing.T) {
	f := newFakeServer(t)
	f.resourceMD["resource"] = f.URL + "/mcp/"

	md := f.metadata(t)
	if md.Resource != f.URL+"/mcp/" {
		t.Fatalf("resource = %q, want it unchanged", md.Resource)
	}
}

func TestDiscoverFindsAnAuthorizationServerWithAPath(t *testing.T) {
	f := newFakeServer(t)
	f.resourceMD["authorization_servers"] = []string{f.URL + "/tenant"}
	f.asMD["issuer"] = f.URL + "/tenant"
	f.mux.HandleFunc("GET /.well-known/oauth-authorization-server/tenant", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, f.asMD)
	})

	md := f.metadata(t)
	if md.Issuer != f.URL+"/tenant" {
		t.Fatalf("issuer = %q", md.Issuer)
	}
}

func TestDiscoverRefuses(t *testing.T) {
	cases := map[string]func(f *fakeServer){
		"no authorization server": func(f *fakeServer) { f.resourceMD["authorization_servers"] = []string{} },
		"no resource":             func(f *fakeServer) { delete(f.resourceMD, "resource") },
		"issuer that differs":     func(f *fakeServer) { f.asMD["issuer"] = "https://elsewhere.example" },
		"no token endpoint":       func(f *fakeServer) { delete(f.asMD, "token_endpoint") },
		"no authorization endpoint": func(f *fakeServer) {
			delete(f.asMD, "authorization_endpoint")
		},
		"plain http token endpoint off loopback": func(f *fakeServer) {
			f.asMD["token_endpoint"] = "http://argus.example/token"
		},
		"plain http authorization endpoint off loopback": func(f *fakeServer) {
			f.asMD["authorization_endpoint"] = "http://argus.example/authorize"
		},
		"plain http registration endpoint off loopback": func(f *fakeServer) {
			f.asMD["registration_endpoint"] = "http://argus.example/register"
		},
		"plain http authorization server off loopback": func(f *fakeServer) {
			f.resourceMD["authorization_servers"] = []string{"http://argus.example"}
		},
		"no S256": func(f *fakeServer) { f.asMD["code_challenge_methods_supported"] = []string{"plain"} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeServer(t)
			change(f)
			if _, err := oauthclient.Discover(context.Background(), f.Client(), f.URL); err == nil {
				t.Fatal("Discover succeeded, want an error")
			}
		})
	}
}

func TestDiscoverRefusesPlainHTTPOffLoopback(t *testing.T) {
	_, err := oauthclient.Discover(context.Background(), http.DefaultClient, "http://argus.example")
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("err = %v, want a refusal naming https", err)
	}
}

func TestDiscoverAcceptsMissingChallengeMethods(t *testing.T) {
	f := newFakeServer(t)
	delete(f.asMD, "code_challenge_methods_supported")

	md := f.metadata(t)
	if len(md.CodeChallengeMethods) != 0 {
		t.Fatalf("methods = %v", md.CodeChallengeMethods)
	}
}

func TestDiscoverReportsAFailedFetch(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	if _, err := oauthclient.Discover(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("Discover succeeded against a 404")
	}
}
