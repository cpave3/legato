package github

import (
	"errors"
	"fmt"
	"net/http"
)

var ErrReadOnlyViolation = errors.New("GitHub client is read-only")

type readOnlyTransport struct {
	underlying http.RoundTripper
}

// NewReadOnlyTransport rejects every non-GET request before network dispatch.
func NewReadOnlyTransport(underlying http.RoundTripper) http.RoundTripper {
	if underlying == nil {
		underlying = http.DefaultTransport
	}
	return &readOnlyTransport{underlying: underlying}
}

func (t *readOnlyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodGet {
		return nil, fmt.Errorf("%w: rejected %s request", ErrReadOnlyViolation, request.Method)
	}
	return t.underlying.RoundTrip(request)
}
