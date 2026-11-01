package oauthclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Authorization is one authorization code flow with PKCE, waiting for its
// redirect on a loopback listener (RFC 8252 §7.3). The code arrives either
// from the browser through the listener, or through Paste when the browser
// ran on another machine.
type Authorization struct {
	url         string
	redirectURI string
	state       string
	verifier    string

	srv  *http.Server
	once sync.Once
	done chan struct{}
	code string
	err  error

	closeOnce sync.Once
	closeErr  error
}

// StartAuthorization binds a listener on 127.0.0.1 at a free port and
// prepares the authorization address with a random state and a PKCE
// verifier. Close it (or Wait for it) when done.
func StartAuthorization(md *Metadata, clientID, scope string) (*Authorization, error) {
	authURL, err := url.Parse(md.AuthorizationEndpoint)
	if err != nil {
		return nil, fmt.Errorf("oauth: authorization endpoint: %w", err)
	}
	state, err := randomString(24)
	if err != nil {
		return nil, err
	}
	// 32 bytes give a 43-character verifier (RFC 7636 §4.1).
	verifier, err := randomString(32)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("oauth: loopback listener: %w", err)
	}

	a := &Authorization{
		redirectURI: fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port),
		state:       state,
		verifier:    verifier,
		done:        make(chan struct{}),
	}
	sum := sha256.Sum256([]byte(verifier))
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", a.redirectURI)
	q.Set("scope", scope)
	q.Set("state", state)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	q.Set("code_challenge_method", "S256")
	q.Set("resource", md.Resource)
	authURL.RawQuery = q.Encode()
	a.url = authURL.String()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", a.callback)
	a.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = a.srv.Serve(ln) }()
	return a, nil
}

// URL is the address to open in the browser.
func (a *Authorization) URL() string { return a.url }

// RedirectURI is the redirect sent in URL, with the bound port. The code
// exchange must send the same one.
func (a *Authorization) RedirectURI() string { return a.redirectURI }

// Verifier is the PKCE code verifier the code exchange must send.
func (a *Authorization) Verifier() string { return a.verifier }

// Wait returns the authorization code once the redirect arrives, or the
// error it carried. It closes the listener before returning.
func (a *Authorization) Wait(ctx context.Context) (string, error) {
	select {
	case <-a.done:
	case <-ctx.Done():
		_ = a.Close()
		return "", ctx.Err()
	}
	_ = a.Close()
	return a.code, a.err
}

// Paste takes the address the browser was redirected to, for a browser on
// another machine whose redirect to 127.0.0.1 could not reach this one. It
// returns the outcome Wait will return; an address that carries neither a
// code nor an error is refused and leaves the authorization open.
func (a *Authorization) Paste(redirected string) error {
	u, err := url.Parse(redirected)
	if err != nil {
		return errors.New("oauth: the pasted text is not an address")
	}
	q := u.Query()
	if !q.Has("code") && !q.Has("error") {
		return errors.New("oauth: the pasted address carries no authorization code")
	}
	_, err = a.deliver(q)
	return err
}

// Close stops the listener. Wait calls it.
func (a *Authorization) Close() error {
	a.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Shutdown lets the callback page finish before the listener goes.
		a.closeErr = a.srv.Shutdown(ctx)
	})
	return a.closeErr
}

func (a *Authorization) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if !q.Has("code") && !q.Has("error") {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, page("This address carries no authorization."))
		return
	}
	code, err := a.deliver(q)
	switch {
	case err == nil && code != "":
		_, _ = fmt.Fprint(w, page("Authorization complete. You can close this tab."))
	default:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, page("Authorization failed: "+html.EscapeString(err.Error())+". You can close this tab."))
	}
}

// deliver checks a redirect's parameters and records the outcome. The first
// outcome is final.
func (a *Authorization) deliver(q url.Values) (string, error) {
	var code string
	var err error
	switch {
	case subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(a.state)) != 1:
		err = ErrStateMismatch
	case q.Get("error") != "":
		err = &Error{Code: q.Get("error"), Description: q.Get("error_description"), op: "authorization"}
	case q.Get("code") == "":
		err = errors.New("oauth: the redirect carries no authorization code")
	default:
		code = q.Get("code")
	}
	a.once.Do(func() {
		a.code, a.err = code, err
		close(a.done)
	})
	return code, err
}

func page(msg string) string {
	return "<!doctype html><meta charset=utf-8><title>Authorization</title><p>" + msg + "</p>\n"
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("oauth: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
