package oauthclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// RegistrationRequest is what a public client tells the server about itself.
type RegistrationRequest struct {
	ClientName string
	// RedirectURI is registered as given. A loopback redirect is usually
	// registered without a port (RFC 8252 §7.3).
	RedirectURI string
}

// Registration is what the server gave the client.
type Registration struct {
	ClientID string
}

// Register registers a public client (RFC 7591) that uses the authorization
// code grant with refresh tokens and no client secret.
func Register(ctx context.Context, client *http.Client, md *Metadata, r RegistrationRequest) (Registration, error) {
	if md.RegistrationEndpoint == "" {
		return Registration{}, fmt.Errorf("oauth: the authorization server offers no registration")
	}
	body, err := json.Marshal(map[string]any{
		"client_name":                r.ClientName,
		"redirect_uris":              []string{r.RedirectURI},
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
	if err != nil {
		return Registration{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, md.RegistrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return Registration{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient(client).Do(req)
	if err != nil {
		return Registration{}, fmt.Errorf("oauth: registration: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Registration{}, fmt.Errorf("oauth: registration: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return Registration{}, errorFromResponse("registration", resp.StatusCode, data)
	}
	var out struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return Registration{}, fmt.Errorf("oauth: registration: %w", err)
	}
	if out.ClientID == "" {
		return Registration{}, fmt.Errorf("oauth: registration answered no client_id")
	}
	return Registration{ClientID: out.ClientID}, nil
}
