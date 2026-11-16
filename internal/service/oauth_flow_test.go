package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/oauthclient"
)

type flowMemorySecrets struct{ *memorySecrets }

func (s flowMemorySecrets) Remove(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, name)
	return nil
}

func TestOAuthLocalWebFlowRegistersTheListenerAndWaitsForOperatorConfirmation(t *testing.T) {
	f := newFakeOAuth(t)
	db := newIntegrationStore(t)
	secrets := flowMemorySecrets{&memorySecrets{values: map[string]string{}}}
	flows := NewOAuthFlowService(db, secrets, OAuthFlowOptions{Connections: flowConnections(t, db, f)})
	defer flows.Close()
	ctx, cancel := context.WithCancel(context.Background())
	begun, err := flows.Begin(ctx, OAuthFlowRequest{Connection: "mcp-burnlite", Scope: "read:tasks", Owner: "operator-authority", Mode: "web"})
	if err != nil {
		t.Fatal(err)
	}
	cancel() // Returning from the initiating HTTP request must not end consent.
	if begun.ReturnKey != "" {
		t.Fatal("web flow exposed its internal receiver capability")
	}
	authURL, err := url.Parse(begun.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	redirect := authURL.Query().Get("redirect_uri")
	f.mu.Lock()
	registered := append([]string(nil), f.registered...)
	f.mu.Unlock()
	if len(registered) != 1 || registered[0] != redirect || !strings.HasPrefix(redirect, "http://127.0.0.1:") {
		t.Fatal("did not register the bound local receiver")
	}
	browser := &http.Client{Timeout: time.Second}
	resp, err := browser.Get(begun.AuthorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Request.URL.Path != "/complete" || resp.Request.URL.RawQuery != "" {
		t.Fatal("browser did not finish on a clean local page")
	}
	state, err := flows.Status(context.Background(), begun.ID, "operator-authority")
	if err != nil || state.Status != "awaiting_confirmation" {
		t.Fatalf("status after callback: %+v, %v", state, err)
	}
	if err := flows.Confirm(context.Background(), begun.ID, "operator-authority"); err != nil {
		t.Fatal(err)
	}
	state, err = flows.Status(context.Background(), begun.ID, "operator-authority")
	if err != nil || state.Status != "authorized" {
		t.Fatal("confirmation did not activate the authorization")
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"access-1", "refresh-1", "code-1", authURL.Query().Get("state"), authURL.Query().Get("code_challenge")} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("status exposed OAuth material")
		}
	}
}

type oauthFlowTransportFunc func(*http.Request) (*http.Response, error)

func (f oauthFlowTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOAuthAppDeliveryUsesTheSavedServerVerifierAndCanReachAnotherWebProcess(t *testing.T) {
	provider := newFakeOAuth(t)
	db := newIntegrationStore(t)
	secrets := flowMemorySecrets{&memorySecrets{values: map[string]string{}}}
	first := NewOAuthFlowService(db, secrets, OAuthFlowOptions{Connections: flowConnections(t, db, provider)})
	defer first.Close()
	second := NewOAuthFlowService(db, secrets, OAuthFlowOptions{Connections: flowConnections(t, db, provider)})
	defer second.Close()
	begun, err := first.Begin(context.Background(), OAuthFlowRequest{Connection: "one", Scope: "read:tasks", Owner: "operator", Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"})
	if err != nil {
		t.Fatal(err)
	}
	authURL, _ := url.Parse(begun.AuthorizeURL)
	challenge := authURL.Query().Get("code_challenge")
	response := oauthclient.AuthorizationResponse{Code: "app-code", State: authURL.Query().Get("state"), Issuer: provider.srv.URL}
	for _, bad := range []struct {
		owner, key string
		response   oauthclient.AuthorizationResponse
	}{
		{"another-operator", begun.ReturnKey, response}, {"operator", "wrong-receiver", response}, {"operator", begun.ReturnKey, oauthclient.AuthorizationResponse{Code: "code", State: response.State, Issuer: "https://other.invalid"}},
	} {
		if _, err := second.Deliver(context.Background(), begun.ID, bad.owner, bad.key, bad.response); err == nil {
			t.Fatal("unbound app response was accepted")
		}
	}
	if state, err := second.Status(context.Background(), begun.ID, "operator"); err != nil || state.Status != "pending" {
		t.Fatal("bad app response consumed pending flow")
	}
	if state, err := second.Deliver(context.Background(), begun.ID, "operator", begun.ReturnKey, response); err != nil || state.Status != "awaiting_confirmation" {
		t.Fatalf("delivery: %+v %v", state, err)
	}
	if state, err := second.Deliver(context.Background(), begun.ID, "operator", begun.ReturnKey, response); err != nil || state.Status != "awaiting_confirmation" {
		t.Fatalf("duplicate delivery: %+v %v", state, err)
	}
	provider.mu.Lock()
	forms := append([]url.Values(nil), provider.tokenForms...)
	provider.mu.Unlock()
	if len(forms) != 1 {
		t.Fatalf("the code was exchanged %d times", len(forms))
	}
	hash := sha256.Sum256([]byte(forms[0].Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(hash[:]) != challenge || forms[0].Get("redirect_uri") != "http://127.0.0.1:34567/callback" || forms[0].Get("resource") != provider.srv.URL+"/api/llm/mcp" {
		t.Fatal("server did not exchange with the original PKCE, redirect and resource")
	}
	if err := first.Confirm(context.Background(), begun.ID, "operator"); err != nil {
		t.Fatal(err)
	}
}

func TestOAuthFlowDisconnectInvalidatesAppDeliveryAndStagedConfirmation(t *testing.T) {
	f := newFakeOAuth(t)
	db := newIntegrationStore(t)
	secrets := flowMemorySecrets{&memorySecrets{values: map[string]string{}}}
	flows := NewOAuthFlowService(db, secrets, OAuthFlowOptions{Connections: flowConnections(t, db, f)})
	defer flows.Close()
	begin := func() OAuthFlowBegin {
		t.Helper()
		b, err := flows.Begin(context.Background(), OAuthFlowRequest{Connection: "connection", Owner: "operator", Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	response := func(b OAuthFlowBegin) oauthclient.AuthorizationResponse {
		u, _ := url.Parse(b.AuthorizeURL)
		return oauthclient.AuthorizationResponse{Code: "code", State: u.Query().Get("state")}
	}
	staged, pending := begin(), begin()
	if _, err := flows.Deliver(context.Background(), staged.ID, "operator", staged.ReturnKey, response(staged)); err != nil {
		t.Fatal(err)
	}
	if err := flows.Disconnect(context.Background(), "connection"); err != nil {
		t.Fatal(err)
	}
	if err := flows.Confirm(context.Background(), staged.ID, "operator"); err == nil {
		t.Fatal("disconnected staged credentials were confirmed")
	}
	if state, err := flows.Deliver(context.Background(), pending.ID, "operator", pending.ReturnKey, response(pending)); err != nil || state.Status != "cancelled" {
		t.Fatal("late app callback did not stay cancelled")
	}
	f.mu.Lock()
	issued := f.issued
	f.mu.Unlock()
	if issued != 1 {
		t.Fatal("late delivery issued another token")
	}
}

func TestOAuthFlowCanDeliverThroughTheExistingSealedStoreAfterReconstruction(t *testing.T) {
	f := newFakeOAuth(t)
	hostSecrets, db := newSecretStoreService(t)
	secrets := SealedIntegrationSecrets{Store: hostSecrets}
	first := NewOAuthFlowService(db, secrets, OAuthFlowOptions{Connections: flowConnections(t, db, f), SecretSource: hostSecrets.Source})
	defer first.Close()
	begun, err := first.Begin(context.Background(), OAuthFlowRequest{Connection: "sealed", Owner: "operator", Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"})
	if err != nil {
		t.Fatal(err)
	}
	authURL, _ := url.Parse(begun.AuthorizeURL)
	second := NewOAuthFlowService(db, secrets, OAuthFlowOptions{Connections: flowConnections(t, db, f)})
	defer second.Close()
	if _, err := second.Deliver(context.Background(), begun.ID, "operator", begun.ReturnKey, oauthclient.AuthorizationResponse{Code: "code", State: authURL.Query().Get("state")}); err != nil {
		t.Fatal(err)
	}
	if err := second.Confirm(context.Background(), begun.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.ListSecrets(context.Background())
	if err != nil || len(rows) != 1 {
		t.Fatal("OAuth credentials did not reach the host sealed store")
	}
	if len(rows[0].Nonce) == 0 || strings.Contains(string(rows[0].Ciphertext), "refresh-1") || strings.Contains(string(rows[0].Ciphertext), "access-1") || strings.Contains(string(rows[0].Ciphertext), authURL.Query().Get("state")) {
		t.Fatal("OAuth material was persisted without sealing")
	}
	if state, err := first.Status(context.Background(), begun.ID, "operator"); err != nil || state.Status != "authorized" {
		t.Fatal("reconstructed authorization is not visible across services")
	}
}

func TestOAuthFlowCancellationAndExpiryPreventLateExchange(t *testing.T) {
	for _, terminal := range []string{"cancelled", "expired"} {
		t.Run(terminal, func(t *testing.T) {
			f := newFakeOAuth(t)
			db := newIntegrationStore(t)
			secrets := flowMemorySecrets{&memorySecrets{values: map[string]string{}}}
			now := time.Now()
			flows := NewOAuthFlowService(db, secrets, OAuthFlowOptions{Connections: flowConnections(t, db, f), Now: func() time.Time { return now }})
			defer flows.Close()
			begun, err := flows.Begin(context.Background(), OAuthFlowRequest{Connection: "one", Owner: "operator", Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"})
			if err != nil {
				t.Fatal(err)
			}
			if terminal == "cancelled" {
				if err := flows.Cancel(context.Background(), begun.ID, "operator"); err != nil {
					t.Fatal(err)
				}
			} else {
				now = now.Add(11 * time.Minute)
			}
			u, _ := url.Parse(begun.AuthorizeURL)
			if state, err := flows.Deliver(context.Background(), begun.ID, "operator", begun.ReturnKey, oauthclient.AuthorizationResponse{Code: "late", State: u.Query().Get("state")}); err != nil || state.Status != terminal {
				t.Fatal("late delivery did not preserve terminal state")
			}
			if state, err := flows.Status(context.Background(), begun.ID, "operator"); err != nil || state.Status != terminal {
				t.Fatalf("terminal status: %+v %v", state, err)
			}
			f.mu.Lock()
			issued := f.issued
			f.mu.Unlock()
			if issued != 0 {
				t.Fatal("ended authorization still exchanged a code")
			}
		})
	}
}

func TestOAuthFlowCloseInterruptsItsPendingLocalReceiver(t *testing.T) {
	f := newFakeOAuth(t)
	db := newIntegrationStore(t)
	secrets := flowMemorySecrets{&memorySecrets{values: map[string]string{}}}
	flows := NewOAuthFlowService(db, secrets, OAuthFlowOptions{Connections: flowConnections(t, db, f)})
	begun, err := flows.Begin(context.Background(), OAuthFlowRequest{Connection: "local", Owner: "operator", Mode: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if err := flows.Close(); err != nil {
		t.Fatal(err)
	}
	replacement := NewOAuthFlowService(db, secrets, OAuthFlowOptions{Connections: flowConnections(t, db, f)})
	defer replacement.Close()
	if state, err := replacement.Status(context.Background(), begun.ID, "operator"); err != nil || state.Status != "interrupted" {
		t.Fatalf("closed receiver: %+v %v", state, err)
	}
	secrets.mu.Lock()
	remaining := len(secrets.values)
	secrets.mu.Unlock()
	if remaining != 0 {
		t.Fatal("interrupted receiver retained its PKCE secret")
	}
}

func TestOAuthFlowReauthorizationRemovesSupersededCredentials(t *testing.T) {
	f := newFakeOAuth(t)
	db := newIntegrationStore(t)
	secrets := flowMemorySecrets{&memorySecrets{values: map[string]string{}}}
	flows := NewOAuthFlowService(db, secrets, OAuthFlowOptions{Connections: flowConnections(t, db, f)})
	defer flows.Close()
	authorize := func() OAuthFlowBegin {
		t.Helper()
		b, err := flows.Begin(context.Background(), OAuthFlowRequest{Connection: "one", Owner: "operator", Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"})
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(b.AuthorizeURL)
		if _, err := flows.Deliver(context.Background(), b.ID, "operator", b.ReturnKey, oauthclient.AuthorizationResponse{Code: "code", State: u.Query().Get("state")}); err != nil {
			t.Fatal(err)
		}
		if err := flows.Confirm(context.Background(), b.ID, "operator"); err != nil {
			t.Fatal(err)
		}
		return b
	}
	old := authorize()
	authorize()
	if state, err := flows.Status(context.Background(), old.ID, "operator"); err != nil || state.Status != "superseded" {
		t.Fatal("old authorization not superseded")
	}
	secrets.mu.Lock()
	remaining := len(secrets.values)
	secrets.mu.Unlock()
	if remaining != 1 {
		t.Fatal("reauthorization kept superseded credentials")
	}
}

type failOAuthTokenWrite struct{ flowMemorySecrets }

func (s failOAuthTokenWrite) Write(ctx context.Context, name, value string) error {
	if strings.Contains(value, "access-1") {
		return errors.New("sealed store unavailable")
	}
	return s.flowMemorySecrets.Write(ctx, name, value)
}
func TestOAuthFlowFailedTokenStorageEndsTheConsumedAttempt(t *testing.T) {
	f := newFakeOAuth(t)
	db := newIntegrationStore(t)
	secrets := failOAuthTokenWrite{flowMemorySecrets{&memorySecrets{values: map[string]string{}}}}
	flows := NewOAuthFlowService(db, secrets, OAuthFlowOptions{Connections: flowConnections(t, db, f)})
	defer flows.Close()
	b, err := flows.Begin(context.Background(), OAuthFlowRequest{Connection: "one", Owner: "operator", Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(b.AuthorizeURL)
	if _, err := flows.Deliver(context.Background(), b.ID, "operator", b.ReturnKey, oauthclient.AuthorizationResponse{Code: "code", State: u.Query().Get("state")}); err == nil {
		t.Fatal("storage failure was hidden")
	}
	if state, err := flows.Status(context.Background(), b.ID, "operator"); err != nil || state.Status != "failed" {
		t.Fatalf("consumed attempt: %+v %v", state, err)
	}
}
