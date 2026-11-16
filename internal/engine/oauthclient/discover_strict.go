package oauthclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
)

// ErrNoServerMetadata is an authorization server that publishes no metadata
// at any location the specification names. Strict discovery then stops: it
// never guesses /authorize, /token or /register.
var ErrNoServerMetadata = errors.New("oauth: the authorization server publishes no metadata")

// errNotFound is a metadata location that answered 404, so the next one is tried.
var errNotFound = errors.New("not found")

// StrictOptions narrow strict discovery.
type StrictOptions struct {
	// ResourceMetadata is the protected-resource metadata address the MCP
	// server named in its WWW-Authenticate challenge, tried first.
	ResourceMetadata string
	// Issuer, when set, is the only authorization server accepted.
	Issuer string
}

// DiscoverStrict discovers an MCP server's authorization as the MCP
// specification (2026-07-28, Authorization Server Discovery) asks: the
// protected-resource metadata from the challenge, else the path-specific
// then the root well-known location (RFC 9728 §3); the resource it names
// must be the configured resource or its origin. Then the authorization
// server's metadata at the RFC 8414 path-inserted location, the OIDC
// path-inserted location and the OIDC path-appended location, whose issuer
// must match exactly; PKCE S256 must be advertised.
func DiscoverStrict(ctx context.Context, client *http.Client, resourceURL string, opts StrictOptions) (*Metadata, error) {
	resource, err := parseSecure("resource URL", resourceURL)
	if err != nil {
		return nil, err
	}
	if resource.User != nil || resource.Fragment != "" {
		return nil, fmt.Errorf("oauth: resource URL must not carry credentials or a fragment")
	}
	origin := *resource
	origin.Path, origin.RawPath, origin.RawQuery, origin.Fragment = "", "", "", ""

	var candidates []string
	if opts.ResourceMetadata != "" {
		u, err := parseSecure("resource metadata", opts.ResourceMetadata)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, u.String())
	}
	if p := strings.TrimSuffix(resource.EscapedPath(), "/"); p != "" {
		candidates = append(candidates, origin.String()+"/.well-known/oauth-protected-resource"+p)
	}
	candidates = append(candidates, origin.String()+"/.well-known/oauth-protected-resource")

	var prm struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
		ScopesSupported      []string `json:"scopes_supported"`
	}
	found := false
	for _, c := range candidates {
		err := getJSONStrict(ctx, client, c, &prm)
		if errors.Is(err, errNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("oauth: protected-resource metadata: %w", err)
		}
		found = true
		break
	}
	if !found {
		return nil, fmt.Errorf("oauth: the MCP server publishes no protected-resource metadata")
	}
	trim := func(s string) string { return strings.TrimSuffix(s, "/") }
	configured := *resource
	configured.Fragment = ""
	if trim(prm.Resource) != trim(configured.String()) && trim(prm.Resource) != trim(origin.String()) {
		return nil, fmt.Errorf("oauth: protected-resource metadata names resource %q, not this server", prm.Resource)
	}
	if len(prm.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("oauth: protected-resource metadata names no authorization server")
	}
	issuer := prm.AuthorizationServers[0]
	if opts.Issuer != "" {
		if !slices.Contains(prm.AuthorizationServers, opts.Issuer) {
			return nil, fmt.Errorf("oauth: the resource does not name the pinned authorization server")
		}
		issuer = opts.Issuer
	}
	issuerURL, err := parseSecure("authorization server", issuer)
	if err != nil {
		return nil, err
	}
	if issuerURL.RawQuery != "" || issuerURL.Fragment != "" {
		return nil, fmt.Errorf("oauth: authorization server identifier has a query or fragment")
	}

	issuerOrigin := *issuerURL
	issuerOrigin.Path, issuerOrigin.RawPath = "", ""
	path := strings.TrimSuffix(issuerURL.EscapedPath(), "/")
	asCandidates := []string{
		issuerOrigin.String() + "/.well-known/oauth-authorization-server" + path,
		issuerOrigin.String() + "/.well-known/openid-configuration" + path,
	}
	if path != "" {
		asCandidates = append(asCandidates, strings.TrimSuffix(issuer, "/")+"/.well-known/openid-configuration")
	}
	var asm struct {
		Issuer                  string   `json:"issuer"`
		AuthorizationEndpoint   string   `json:"authorization_endpoint"`
		TokenEndpoint           string   `json:"token_endpoint"`
		RegistrationEndpoint    string   `json:"registration_endpoint"`
		RevocationEndpoint      string   `json:"revocation_endpoint"`
		CodeChallengeMethods    []string `json:"code_challenge_methods_supported"`
		ScopesSupported         []string `json:"scopes_supported"`
		TokenAuthMethods        []string `json:"token_endpoint_auth_methods_supported"`
		ResponseIssuerSupported bool     `json:"authorization_response_iss_parameter_supported"`
		CIMDSupported           bool     `json:"client_id_metadata_document_supported"`
	}
	found = false
	for _, c := range asCandidates {
		err := getJSONStrict(ctx, client, c, &asm)
		if errors.Is(err, errNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("oauth: authorization server metadata: %w", err)
		}
		found = true
		break
	}
	if !found {
		return nil, ErrNoServerMetadata
	}
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
	for _, ep := range []struct{ name, value string }{
		{"registration endpoint", asm.RegistrationEndpoint},
		{"revocation endpoint", asm.RevocationEndpoint},
	} {
		if ep.value != "" {
			if _, err := parseSecure(ep.name, ep.value); err != nil {
				return nil, err
			}
		}
	}
	if !slices.Contains(asm.CodeChallengeMethods, "S256") {
		return nil, fmt.Errorf("oauth: authorization server does not advertise PKCE S256")
	}
	return &Metadata{
		Resource:                prm.Resource,
		Issuer:                  asm.Issuer,
		AuthorizationEndpoint:   asm.AuthorizationEndpoint,
		TokenEndpoint:           asm.TokenEndpoint,
		RegistrationEndpoint:    asm.RegistrationEndpoint,
		RevocationEndpoint:      asm.RevocationEndpoint,
		CodeChallengeMethods:    asm.CodeChallengeMethods,
		ResponseIssuerSupported: asm.ResponseIssuerSupported,
		CIMDSupported:           asm.CIMDSupported,
		ResourceScopes:          prm.ScopesSupported,
		ServerScopes:            asm.ScopesSupported,
		TokenAuthMethods:        asm.TokenAuthMethods,
	}, nil
}

// getJSONStrict is getJSON that reports a 404 as errNotFound, so the next
// metadata location can be tried.
func getJSONStrict(ctx context.Context, client *http.Client, addr string, v any) error {
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
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return errNotFound
	default:
		return fmt.Errorf("GET %s answered %d", addr, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(v); err != nil {
		return fmt.Errorf("GET %s: %w", addr, err)
	}
	return nil
}

// ChallengeResult is what an MCP server's 401 said about its authorization.
type ChallengeResult struct {
	ResourceMetadata string
	Scope            string
}

// Challenge asks the MCP endpoint, without credentials, how it wants to be
// authorized: an MCP initialize request whose 401 answer's WWW-Authenticate
// names the resource metadata and scope. Any other answer gives an empty result.
func Challenge(ctx context.Context, client *http.Client, resourceURL string) (ChallengeResult, error) {
	if _, err := parseSecure("resource URL", resourceURL); err != nil {
		return ChallengeResult{}, err
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-07-28","capabilities":{},"clientInfo":{"name":"Legato","version":"1"}}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resourceURL, strings.NewReader(body))
	if err != nil {
		return ChallengeResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := httpClient(client).Do(req)
	if err != nil {
		return ChallengeResult{}, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		return ChallengeResult{}, nil
	}
	params := bearerParams(resp.Header.Values("WWW-Authenticate"))
	return ChallengeResult{ResourceMetadata: params["resource_metadata"], Scope: params["scope"]}, nil
}

// bearerParams reads the auth-params of a Bearer challenge (RFC 6750 §3).
func bearerParams(headers []string) map[string]string {
	out := map[string]string{}
	for _, h := range headers {
		h = strings.TrimSpace(h)
		if len(h) < 6 || !strings.EqualFold(h[:6], "bearer") {
			continue
		}
		rest := h[6:]
		for rest != "" {
			rest = strings.TrimLeft(rest, " ,")
			eq := strings.IndexByte(rest, '=')
			if eq <= 0 {
				break
			}
			key := strings.ToLower(strings.TrimSpace(rest[:eq]))
			rest = rest[eq+1:]
			var val string
			if strings.HasPrefix(rest, `"`) {
				end := strings.IndexByte(rest[1:], '"')
				if end < 0 {
					break
				}
				val, rest = rest[1:end+1], rest[end+2:]
			} else {
				end := strings.IndexAny(rest, ", ")
				if end < 0 {
					end = len(rest)
				}
				val, rest = rest[:end], rest[end:]
			}
			if _, seen := out[key]; !seen {
				out[key] = val
			}
		}
	}
	return out
}
