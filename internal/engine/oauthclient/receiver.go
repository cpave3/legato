package oauthclient

import (
	"context"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AuthorizationResponse is the provider response, not a token. The transaction
// owner must check State and Issuer before exchanging Code. Never log this value.
type AuthorizationResponse struct {
	Code   string `json:"code,omitempty"`
	State  string `json:"state"`
	Issuer string `json:"iss,omitempty"`
	Error  string `json:"error,omitempty"`
}

var ErrAuthorizationResponse = errors.New("oauth: malformed authorization response")
var ErrIssuerMismatch = errors.New("oauth: the authorization response issuer does not match")
var ErrAuthorizationUsed = errors.New("oauth: authorization response already received")

func (r AuthorizationResponse) validate() error {
	if r.State == "" || len(r.State) > 512 || len(r.Code) > 4096 || len(r.Issuer) > 2048 || len(r.Error) > 128 || (r.Code == "") == (r.Error == "") {
		return ErrAuthorizationResponse
	}
	return nil
}

// Check binds a parsed response to the server's saved transaction before a
// durable exchange claim. A provider denial still passes these binding checks.
func (r AuthorizationResponse) Check(state, issuer string, requireIssuer bool) error {
	if err := r.validate(); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(r.State), []byte(state)) != 1 {
		return ErrStateMismatch
	}
	if (requireIssuer && r.Issuer == "") || (r.Issuer != "" && r.Issuer != issuer) {
		return ErrIssuerMismatch
	}
	return nil
}

func safeAuthorizationError(code string) string {
	switch code {
	case "invalid_request", "unauthorized_client", "access_denied", "unsupported_response_type", "invalid_scope", "server_error", "temporarily_unavailable":
		return code
	default:
		return "authorization_failed"
	}
}

// ParseAuthorizationResponse reads only the response fields needed by the
// transaction owner. Provider descriptions and unrelated parameters are discarded.
func ParseAuthorizationResponse(query string) (AuthorizationResponse, error) {
	if len(query) > 8192 {
		return AuthorizationResponse{}, ErrAuthorizationResponse
	}
	values, err := url.ParseQuery(query)
	if err != nil {
		return AuthorizationResponse{}, ErrAuthorizationResponse
	}
	for _, key := range []string{"code", "state", "iss", "error"} {
		if len(values[key]) > 1 {
			return AuthorizationResponse{}, ErrAuthorizationResponse
		}
	}
	r := AuthorizationResponse{Code: values.Get("code"), State: values.Get("state"), Issuer: values.Get("iss"), Error: values.Get("error")}
	if err := r.validate(); err != nil {
		return AuthorizationResponse{}, err
	}
	return r, nil
}

// LoopbackReceiver owns a temporary IP-loopback callback listener. Its handler
// validates the transaction, not this receiver. Close it when the attempt ends.
type LoopbackReceiver struct {
	server     *http.Server
	address    string
	closed     chan struct{}
	closeOnce  sync.Once
	closeErr   error
	mu         sync.Mutex
	delivered  bool
	delivering bool
}

// ListenLoopback binds before registration, so RedirectURI includes the actual
// reserved port. ctx bounds the receiver's lifetime, not an initiating HTTP call.
func ListenLoopback(ctx context.Context, receive func(context.Context, AuthorizationResponse) error) (*LoopbackReceiver, error) {
	return ListenLoopbackAt(ctx, 0, receive)
}

// ErrPortInUse is a fixed receiver port another process holds.
var ErrPortInUse = errors.New("oauth: the callback port is in use")

// ListenLoopbackAt binds 127.0.0.1:port (0 for any free port). A fixed port
// that is taken fails with ErrPortInUse; it is never swapped for another.
func ListenLoopbackAt(ctx context.Context, port int, receive func(context.Context, AuthorizationResponse) error) (*LoopbackReceiver, error) {
	if port < 0 || port > 65535 {
		return nil, errors.New("oauth: invalid callback port")
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		if port != 0 {
			return nil, ErrPortInUse
		}
		return nil, errors.New("oauth: could not start the loopback receiver")
	}
	r := &LoopbackReceiver{address: "http://" + ln.Addr().String(), closed: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, req *http.Request) {
		receiverHeaders(w)
		if req.Host != ln.Addr().String() {
			http.Error(w, "Invalid callback address.", http.StatusBadRequest)
			return
		}
		response, err := ParseAuthorizationResponse(req.URL.RawQuery)
		if err != nil {
			http.Error(w, "Invalid authorization response.", http.StatusBadRequest)
			return
		}
		r.mu.Lock()
		if r.delivered || r.delivering {
			r.mu.Unlock()
			http.Error(w, "This response has already been received.", http.StatusConflict)
			return
		}
		r.delivering = true
		r.mu.Unlock()
		err = receive(req.Context(), response)
		r.mu.Lock()
		r.delivering = false
		r.delivered = err == nil
		r.mu.Unlock()
		if err != nil {
			http.Error(w, "Authorization response refused. Return to Legato to check the connection.", http.StatusBadRequest)
			return
		}
		http.Redirect(w, req, "/complete", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /complete", func(w http.ResponseWriter, req *http.Request) {
		receiverHeaders(w)
		if req.Host != ln.Addr().String() {
			http.Error(w, "Invalid callback address.", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><meta charset=utf-8><title>Legato authorization</title><p>Return to Legato to check the connection. You can close this tab.</p>"))
	})
	r.server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { receiverHeaders(w); mux.ServeHTTP(w, req) }), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() { _ = r.server.Serve(ln) }()
	go func() {
		select {
		case <-ctx.Done():
			_ = r.Close()
		case <-r.closed:
		}
	}()
	return r, nil
}

func receiverHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func (r *LoopbackReceiver) RedirectURI() string {
	return strings.TrimRight(r.address, "/") + "/callback"
}

func (r *LoopbackReceiver) Close() error {
	r.closeOnce.Do(func() { close(r.closed); r.closeErr = r.server.Close() })
	return r.closeErr
}
