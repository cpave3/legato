package oauthclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Token is what the token endpoint answered.
type Token struct {
	AccessToken  string
	RefreshToken string
	// ExpiresAt is when the access token expires, from expires_in; zero
	// when the server did not say.
	ExpiresAt time.Time
	Scope     string
}

// TokenOption changes a token request.
type TokenOption func(url.Values)

// WithClientSecret sends the client's secret in the request body
// (client_secret_post), for a server that gave one at registration. An
// empty secret sends nothing.
func WithClientSecret(secret string) TokenOption {
	return func(form url.Values) {
		if secret != "" {
			form.Set("client_secret", secret)
		}
	}
}

// Exchange trades an authorization code for tokens. redirectURI and
// verifier must be the ones the authorization used.
func Exchange(ctx context.Context, client *http.Client, md *Metadata, clientID, code, verifier, redirectURI string, opts ...TokenOption) (*Token, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
		"resource":      {md.Resource},
	}
	for _, opt := range opts {
		opt(form)
	}
	return requestToken(ctx, client, md, "code exchange", form)
}

// Refresh gets a new access token. When the server rotates the refresh
// token, the new one is in the answer; otherwise the answer keeps
// refreshToken. A dead refresh token gives an error that matches
// ErrInvalidGrant.
func Refresh(ctx context.Context, client *http.Client, md *Metadata, clientID, refreshToken string, opts ...TokenOption) (*Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
		"resource":      {md.Resource},
	}
	for _, opt := range opts {
		opt(form)
	}
	tok, err := requestToken(ctx, client, md, "refresh", form)
	if err != nil {
		return nil, err
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = refreshToken
	}
	return tok, nil
}

func requestToken(ctx context.Context, client *http.Client, md *Metadata, op string, form url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, md.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("oauth: %s: %w", op, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient(client).Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth: %s: %w", op, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("oauth: %s: %w", op, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errorFromResponse(op, resp.StatusCode, data)
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		// The body may hold tokens, so only say it was not JSON.
		return nil, fmt.Errorf("oauth: %s: the answer is not a token response", op)
	}
	if out.AccessToken == "" {
		return nil, errors.New("oauth: " + op + ": the answer has no access token")
	}
	tok := &Token{AccessToken: out.AccessToken, RefreshToken: out.RefreshToken, Scope: out.Scope}
	if out.ExpiresIn > 0 {
		tok.ExpiresAt = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	}
	return tok, nil
}
