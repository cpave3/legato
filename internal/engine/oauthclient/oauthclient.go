// Package oauthclient is an OAuth 2.1 public client: it discovers an
// authorization server from a protected resource (RFC 9728, RFC 8414),
// registers itself (RFC 7591), authorizes with PKCE through a loopback
// redirect (RFC 8252), and exchanges and refreshes tokens. It never puts a
// token, code or verifier in an error.
package oauthclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// maxBody bounds how much of any answer is read.
const maxBody = 1 << 20

// Metadata is what discovery found about a resource and its authorization
// server.
type Metadata struct {
	// Resource is the resource identifier exactly as the protected-resource
	// metadata gave it. It is sent on every authorization and token request.
	Resource              string
	Issuer                string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string
	// CodeChallengeMethods is empty when the server did not list them.
	CodeChallengeMethods []string
}

// Discover reads the protected-resource metadata at the origin of
// resourceURL, then the metadata of its first authorization server. Every
// address must be https, or http to a loopback host.
func Discover(ctx context.Context, client *http.Client, resourceURL string) (*Metadata, error) {
	origin, err := parseSecure("resource URL", resourceURL)
	if err != nil {
		return nil, err
	}
	origin.Path, origin.RawPath, origin.RawQuery, origin.Fragment = "", "", "", ""

	var prm struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err := getJSON(ctx, client, origin.String()+"/.well-known/oauth-protected-resource", &prm); err != nil {
		return nil, fmt.Errorf("oauth: protected-resource metadata: %w", err)
	}
	if prm.Resource == "" {
		return nil, fmt.Errorf("oauth: protected-resource metadata names no resource")
	}
	if len(prm.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("oauth: protected-resource metadata names no authorization server")
	}
	issuer := prm.AuthorizationServers[0]
	issuerURL, err := parseSecure("authorization server", issuer)
	if err != nil {
		return nil, err
	}

	// RFC 8414 §3: the well-known segment goes between the host and the
	// issuer's path.
	wellKnown := *issuerURL
	wellKnown.Path = "/.well-known/oauth-authorization-server" + strings.TrimSuffix(issuerURL.Path, "/")
	wellKnown.RawPath, wellKnown.RawQuery, wellKnown.Fragment = "", "", ""

	var asm struct {
		Issuer                string   `json:"issuer"`
		AuthorizationEndpoint string   `json:"authorization_endpoint"`
		TokenEndpoint         string   `json:"token_endpoint"`
		RegistrationEndpoint  string   `json:"registration_endpoint"`
		CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
	}
	if err := getJSON(ctx, client, wellKnown.String(), &asm); err != nil {
		return nil, fmt.Errorf("oauth: authorization server metadata: %w", err)
	}
	// RFC 8414 §3.3: the issuer must be the one the metadata was fetched for.
	if asm.Issuer != issuer {
		return nil, fmt.Errorf("oauth: authorization server metadata names issuer %q, want %q", asm.Issuer, issuer)
	}
	for _, ep := range []struct{ name, value string }{
		{"authorization endpoint", asm.AuthorizationEndpoint},
		{"token endpoint", asm.TokenEndpoint},
	} {
		if ep.value == "" {
			return nil, fmt.Errorf("oauth: authorization server metadata has no %s", ep.name)
		}
		if _, err := parseSecure(ep.name, ep.value); err != nil {
			return nil, err
		}
	}
	if asm.RegistrationEndpoint != "" {
		if _, err := parseSecure("registration endpoint", asm.RegistrationEndpoint); err != nil {
			return nil, err
		}
	}
	if len(asm.CodeChallengeMethods) > 0 && !slices.Contains(asm.CodeChallengeMethods, "S256") {
		return nil, fmt.Errorf("oauth: authorization server does not support PKCE S256")
	}

	return &Metadata{
		Resource:              prm.Resource,
		Issuer:                asm.Issuer,
		AuthorizationEndpoint: asm.AuthorizationEndpoint,
		TokenEndpoint:         asm.TokenEndpoint,
		RegistrationEndpoint:  asm.RegistrationEndpoint,
		CodeChallengeMethods:  asm.CodeChallengeMethods,
	}, nil
}

// parseSecure parses an absolute address and refuses one that is not https,
// unless it is http to a loopback host.
func parseSecure(name, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("oauth: %s %q is not an absolute address", name, raw)
	}
	switch u.Scheme {
	case "https":
		return u, nil
	case "http":
		if isLoopback(u.Hostname()) {
			return u, nil
		}
	}
	return nil, fmt.Errorf("oauth: %s %q must use https (http only to a loopback host)", name, raw)
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func getJSON(ctx context.Context, client *http.Client, addr string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient(client).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s answered %d", addr, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(v); err != nil {
		return fmt.Errorf("GET %s: %w", addr, err)
	}
	return nil
}
