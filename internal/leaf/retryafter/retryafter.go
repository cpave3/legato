// Package retryafter reads an HTTP Retry-After header.
//
// It is a leaf package: it imports only the standard library, so any
// Legato package, an engine package included, may use it (see
// docs/adr/0001-stdlib-leaf-packages.md).
package retryafter

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Parse reads a Retry-After value in either form RFC 9110 allows: a
// whole number of seconds, or an HTTP date. It returns how long to wait
// from now, or 0 when the value is empty, malformed, not positive, or a
// date that is not after now.
func Parse(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}
