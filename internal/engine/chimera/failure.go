package chimera

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
)

// maxStatusBody bounds how much of an upstream error body an error keeps.
const maxStatusBody = 256

// StatusError is a response from a Chimera endpoint that was not a success.
type StatusError struct {
	Method string
	Path   string
	Code   int
	Status string
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s %s: %s %s", e.Method, e.Path, e.Status, e.Body)
}

func newStatusError(method, path string, resp *http.Response) *StatusError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxStatusBody))
	return &StatusError{Method: method, Path: path, Code: resp.StatusCode, Status: resp.Status, Body: string(body)}
}

// Failure classes name why a call to a Chimera endpoint failed, in words
// that never carry the endpoint or anything the endpoint sent.
const (
	FailureGone        = "gone"        // the session is not served there (404, 410)
	FailureRefused     = "refused"     // nothing listens at the endpoint any more
	FailureAuth        = "auth"        // the endpoint refused Legato's credentials
	FailureServerError = "server_error"
	FailureBusy        = "busy"
	FailureStatus      = "status"
	FailureTimeout     = "timeout"
	FailureCancelled   = "cancelled"
	FailureMalformed   = "malformed"
	FailureConnection  = "connection"
	FailureUnknown     = "unknown"
)

// IsSessionGone reports whether err shows, beyond doubt, that the endpoint no
// longer serves the session: the session is unknown there, or nothing listens.
// Any other failure (a busy or failing server, a timeout, a refused credential,
// a broken body) may pass, and says nothing about whether the session lives.
func IsSessionGone(err error) bool {
	switch FailureClass(err) {
	case FailureGone, FailureRefused:
		return true
	}
	return false
}

// FailureClass names the kind of failure err is, as one of the Failure
// constants. It is safe to log and to show.
func FailureClass(err error) string {
	if err == nil {
		return ""
	}
	var status *StatusError
	if errors.As(err, &status) {
		switch {
		case status.Code == http.StatusNotFound || status.Code == http.StatusGone:
			return FailureGone
		case status.Code == http.StatusUnauthorized || status.Code == http.StatusForbidden:
			return FailureAuth
		case status.Code == http.StatusConflict || status.Code == http.StatusTooManyRequests || status.Code == http.StatusRequestTimeout:
			return FailureBusy
		case status.Code >= 500:
			return FailureServerError
		default:
			return FailureStatus
		}
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return FailureRefused
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return FailureTimeout
	}
	if errors.Is(err, context.Canceled) {
		return FailureCancelled
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return FailureTimeout
	}
	var syntax *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntax) || errors.As(err, &typeErr) || errors.Is(err, io.ErrUnexpectedEOF) {
		return FailureMalformed
	}
	var endpointErr *endpointRequestError
	if errors.As(err, &endpointErr) || errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET) {
		return FailureConnection
	}
	return FailureUnknown
}
