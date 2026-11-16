package oauthclient_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/oauthclient"
)

// mcpProvider is an MCP server at /mcp with its authorization server under
// /as, where every metadata location can be turned on or off.
type mcpProvider struct {
	*httptest.Server
	mu       sync.Mutex
	paths    []string
	prm      map[string]map[string]any // by well-known path
	asm      map[string]map[string]any
	header   string // WWW-Authenticate on the MCP endpoint
	mcpCalls int
}

func newMCPProvider(t *testing.T) *mcpProvider {
	t.Helper()
	p := &mcpProvider{prm: map[string]map[string]any{}, asm: map[string]map[string]any{}}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.paths = append(p.paths, r.Method+" "+r.URL.Path)
		prm, okPRM := p.prm[r.URL.Path]
		asm, okASM := p.asm[r.URL.Path]
		header := p.header
		if r.URL.Path == "/mcp" {
			p.mcpCalls++
		}
		p.mu.Unlock()
		switch {
		case r.URL.Path == "/mcp" && header != "":
			w.Header().Set("WWW-Authenticate", header)
			w.WriteHeader(http.StatusUnauthorized)
		case okPRM:
			writeJSON(w, 200, prm)
		case okASM:
			writeJSON(w, 200, asm)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.Close)
	issuer := p.URL + "/as"
	p.prm["/.well-known/oauth-protected-resource/mcp"] = map[string]any{
		"resource": p.URL + "/mcp", "authorization_servers": []string{issuer}, "scopes_supported": []string{"read", "write"},
	}
	p.asm["/.well-known/oauth-authorization-server/as"] = map[string]any{
		"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token",
		"registration_endpoint": issuer + "/register", "code_challenge_methods_supported": []string{"S256"},
		"scopes_supported": []string{"read", "write", "offline_access"}, "client_id_metadata_document_supported": true,
		"revocation_endpoint": issuer + "/revoke", "authorization_response_iss_parameter_supported": true,
	}
	return p
}

func (p *mcpProvider) requested() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.paths...)
}

func discover(p *mcpProvider, opts oauthclient.StrictOptions) (*oauthclient.Metadata, error) {
	return oauthclient.DiscoverStrict(context.Background(), p.Client(), p.URL+"/mcp", opts)
}

func TestStrictDiscoveryReadsPathSpecificMetadata(t *testing.T) {
	p := newMCPProvider(t)
	md, err := discover(p, oauthclient.StrictOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if md.Resource != p.URL+"/mcp" || md.Issuer != p.URL+"/as" || !md.CIMDSupported || md.RevocationEndpoint != p.URL+"/as/revoke" || !md.ResponseIssuerSupported {
		t.Fatalf("metadata %+v", md)
	}
	if strings.Join(md.ResourceScopes, " ") != "read write" || strings.Join(md.ServerScopes, " ") != "read write offline_access" {
		t.Fatalf("scopes %v %v", md.ResourceScopes, md.ServerScopes)
	}
}

func TestStrictDiscoveryFallsBackToRootAndOIDCLocations(t *testing.T) {
	p := newMCPProvider(t)
	p.prm["/.well-known/oauth-protected-resource"] = p.prm["/.well-known/oauth-protected-resource/mcp"]
	delete(p.prm, "/.well-known/oauth-protected-resource/mcp")
	p.asm["/.well-known/openid-configuration/as"] = p.asm["/.well-known/oauth-authorization-server/as"]
	delete(p.asm, "/.well-known/oauth-authorization-server/as")
	if _, err := discover(p, oauthclient.StrictOptions{}); err != nil {
		t.Fatal(err)
	}
	// OIDC's appended form, for an issuer with a path.
	p.asm["/as/.well-known/openid-configuration"] = p.asm["/.well-known/openid-configuration/as"]
	delete(p.asm, "/.well-known/openid-configuration/as")
	if _, err := discover(p, oauthclient.StrictOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestStrictDiscoveryFollowsTheChallengedMetadata(t *testing.T) {
	p := newMCPProvider(t)
	p.prm["/custom-prm"] = p.prm["/.well-known/oauth-protected-resource/mcp"]
	delete(p.prm, "/.well-known/oauth-protected-resource/mcp")
	p.header = `Bearer resource_metadata="` + p.URL + `/custom-prm", scope="read"`
	challenge, err := oauthclient.Challenge(context.Background(), p.Client(), p.URL+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if challenge.ResourceMetadata != p.URL+"/custom-prm" || challenge.Scope != "read" {
		t.Fatalf("challenge %+v", challenge)
	}
	if _, err := discover(p, oauthclient.StrictOptions{ResourceMetadata: challenge.ResourceMetadata}); err != nil {
		t.Fatal(err)
	}
}

// Without authorization server metadata, discovery fails: it never guesses
// /authorize, /token or /register.
func TestStrictDiscoveryNeverGuessesEndpoints(t *testing.T) {
	p := newMCPProvider(t)
	delete(p.asm, "/.well-known/oauth-authorization-server/as")
	_, err := discover(p, oauthclient.StrictOptions{})
	if !errors.Is(err, oauthclient.ErrNoServerMetadata) {
		t.Fatalf("missing metadata: %v", err)
	}
	for _, path := range p.requested() {
		for _, guessed := range []string{"/authorize", "/token", "/register"} {
			if strings.HasSuffix(path, guessed) {
				t.Fatalf("guessed %s", path)
			}
		}
	}
}

func TestStrictDiscoveryRefusesMismatchedBindings(t *testing.T) {
	for name, change := range map[string]func(p *mcpProvider){
		"another resource": func(p *mcpProvider) {
			p.prm["/.well-known/oauth-protected-resource/mcp"]["resource"] = "https://other.example.test/mcp"
		},
		"another issuer": func(p *mcpProvider) {
			p.asm["/.well-known/oauth-authorization-server/as"]["issuer"] = "https://evil.example.test/as"
		},
		"no S256": func(p *mcpProvider) {
			p.asm["/.well-known/oauth-authorization-server/as"]["code_challenge_methods_supported"] = []string{"plain"}
		},
		"PKCE unadvertised": func(p *mcpProvider) {
			delete(p.asm["/.well-known/oauth-authorization-server/as"], "code_challenge_methods_supported")
		},
		"no authorization server": func(p *mcpProvider) {
			p.prm["/.well-known/oauth-protected-resource/mcp"]["authorization_servers"] = []string{}
		},
		"no token endpoint": func(p *mcpProvider) {
			delete(p.asm["/.well-known/oauth-authorization-server/as"], "token_endpoint")
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := newMCPProvider(t)
			change(p)
			if _, err := discover(p, oauthclient.StrictOptions{}); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	p := newMCPProvider(t)
	if _, err := discover(p, oauthclient.StrictOptions{Issuer: "https://pinned.example.test"}); err == nil {
		t.Fatal("accepted an issuer other than the pinned one")
	}
	if md, err := discover(p, oauthclient.StrictOptions{Issuer: p.URL + "/as"}); err != nil || md.Issuer != p.URL+"/as" {
		t.Fatalf("pinned issuer: %v", err)
	}
}

// A resource identified by its origin (root metadata) is accepted for an
// endpoint on that origin; the resource sent is the one the metadata names.
func TestStrictDiscoveryAcceptsAnOriginResource(t *testing.T) {
	p := newMCPProvider(t)
	p.prm["/.well-known/oauth-protected-resource/mcp"]["resource"] = p.URL
	md, err := discover(p, oauthclient.StrictOptions{})
	if err != nil || md.Resource != p.URL {
		t.Fatalf("origin resource: %+v %v", md, err)
	}
}

func TestAuthorizationOmitsAnEmptyScope(t *testing.T) {
	md := &oauthclient.Metadata{AuthorizationEndpoint: "https://as.example.test/authorize", Resource: "https://mcp.example.test/mcp"}
	a, err := oauthclient.StartRedirectAuthorization(md, "client", "", "http://127.0.0.1:1/callback")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(a.URL(), "scope=") {
		t.Fatalf("empty scope sent: %s", a.URL())
	}
}
