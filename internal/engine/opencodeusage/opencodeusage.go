// Package opencodeusage reads an OpenCode Go subscription's usage limits:
// the rolling 5-hour, weekly and monthly meters with the percent used and
// when each resets. It asks the usage endpoint OpenCode documents for its
// clients, /inference/go/v1/usage, with the Go API key. The reading is the
// account's, so it counts the calls every client — OpenCode itself or
// another tool with the same key — makes against the allowance.
//
// The meter names, their statuses and their reset times are what the
// endpoint says, never a schedule reconstructed from OpenCode's advertised
// limits. Every answer this package does not understand is
// ErrEndpointChanged, never a guess. No error this package returns carries
// the key or the endpoint's body.
package opencodeusage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is where the Go endpoints live.
const DefaultBaseURL = "https://opencode.ai"

// usagePath is the documented usage endpoint of OpenCode Go.
const usagePath = "/inference/go/v1/usage"

// requestTimeout bounds one read.
const requestTimeout = 15 * time.Second

// bodyLimit bounds the endpoint's answer, which is a few hundred bytes.
const bodyLimit = 64 << 10

// The meters this package knows. Others in the answer are ignored.
const (
	Rolling = "rolling"
	Weekly  = "weekly"
	Monthly = "monthly"
)

var knownMeters = []string{Rolling, Weekly, Monthly}

var (
	// ErrNoKey: no API key is configured, so there is nothing to ask with.
	ErrNoKey = errors.New("opencode: no API key")
	// ErrInvalidKey: the endpoint refused the API key.
	ErrInvalidKey = errors.New("opencode: the API key was refused")
	// ErrNoSubscription: the key's workspace has no Go subscription.
	ErrNoSubscription = errors.New("opencode: no Go subscription")
	// ErrEndpointChanged: the endpoint is gone or answered in a shape this
	// package does not know.
	ErrEndpointChanged = errors.New("opencode: usage endpoint changed")
)

// RateLimitedError is a 429: ask again after RetryAfter, zero when the
// endpoint did not say.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("opencode: usage endpoint rate limited; retry after %s", e.RetryAfter)
	}
	return "opencode: usage endpoint rate limited"
}

// Meter is one usage meter: a limit's window.
type Meter struct {
	// Name is one of Rolling, Weekly, Monthly.
	Name string
	// UsedPercent is how much of the limit is used, from 0 to 100. Zero is
	// a real reading; a meter that is missing or null is not reported.
	UsedPercent float64
	// Limited is the endpoint's own word that the limit is used up.
	Limited bool
	// ResetsAt is when the limit resets, as the endpoint says.
	ResetsAt time.Time
}

// Reading is the meters the endpoint reported, in knownMeters order.
type Reading struct {
	Meters []Meter
}

// Client reads usage. The zero value is not usable; set APIKey.
type Client struct {
	// BaseURL is DefaultBaseURL when empty.
	BaseURL string
	// APIKey is the Go API key. Empty means none is configured: Read
	// fails with ErrNoKey without a request.
	APIKey string
	// HTTP is a client that neither follows a redirect nor uses the
	// environment's proxies when nil.
	HTTP *http.Client
	// Now is time.Now when nil.
	Now func() time.Time
}

// Read asks the endpoint for the key's usage.
func (c *Client) Read(ctx context.Context) (Reading, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	if strings.TrimSpace(c.APIKey) == "" {
		return Reading{}, ErrNoKey
	}
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+usagePath, nil)
	if err != nil {
		return Reading{}, errors.New("opencode: building the usage request")
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Reading{}, transportError(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	if err != nil {
		return Reading{}, errors.New("opencode: reading the usage answer failed")
	}

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized:
		return Reading{}, ErrInvalidKey
	case resp.StatusCode == http.StatusForbidden:
		return Reading{}, ErrNoSubscription
	case resp.StatusCode == http.StatusTooManyRequests:
		return Reading{}, &RateLimitedError{RetryAfter: retryAfter(resp.Header.Get("Retry-After"), now())}
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return Reading{}, fmt.Errorf("%w (status %d)", ErrEndpointChanged, resp.StatusCode)
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		// The origin is fixed: a redirect is not followed.
		return Reading{}, fmt.Errorf("%w (redirected, status %d)", ErrEndpointChanged, resp.StatusCode)
	default:
		return Reading{}, fmt.Errorf("opencode: usage endpoint answered status %d", resp.StatusCode)
	}
	return decode(body)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Transport: transport, Timeout: requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

// transportError says what failed without the request, whose URL the
// standard library would quote.
func transportError(err error) error {
	var netErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		return errors.New("opencode: usage endpoint timed out")
	case errors.Is(err, context.Canceled):
		return context.Canceled
	default:
		return errors.New("opencode: usage endpoint unreachable")
	}
}

func retryAfter(value string, now time.Time) time.Duration {
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

type wireMeter struct {
	Status   *string   `json:"status"`
	Percent  *float64  `json:"percent"`
	ResetsAt *string   `json:"resetsAt"`
}

func decode(body []byte) (Reading, error) {
	var raw struct {
		Usage map[string]json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Reading{}, fmt.Errorf("%w (not JSON)", ErrEndpointChanged)
	}
	if len(raw.Usage) == 0 {
		return Reading{}, fmt.Errorf("%w (no usage)", ErrEndpointChanged)
	}
	var reading Reading
	for _, name := range knownMeters {
		value, ok := raw.Usage[name]
		if !ok || string(value) == "null" {
			continue
		}
		meter, err := decodeMeter(name, value)
		if err != nil {
			return Reading{}, err
		}
		reading.Meters = append(reading.Meters, meter)
	}
	if len(reading.Meters) == 0 {
		return Reading{}, fmt.Errorf("%w (no known meter)", ErrEndpointChanged)
	}
	return reading, nil
}

// decodeMeter decodes one meter strictly: the endpoint's statuses are the
// two it documents, the percent is a finite share of the limit, and a limit
// used up is all of it. Anything else is a shape this package does not
// know, not a reading to guess from.
func decodeMeter(name string, value json.RawMessage) (Meter, error) {
	var w wireMeter
	if err := json.Unmarshal(value, &w); err != nil {
		return Meter{}, fmt.Errorf("%w (meter %s)", ErrEndpointChanged, name)
	}
	limited := false
	switch {
	case w.Status == nil:
		return Meter{}, fmt.Errorf("%w (meter %s status)", ErrEndpointChanged, name)
	case *w.Status == "ok":
	case *w.Status == "rate-limited":
		limited = true
	default:
		return Meter{}, fmt.Errorf("%w (meter %s status %q)", ErrEndpointChanged, name, *w.Status)
	}
	if w.Percent == nil || math.IsNaN(*w.Percent) || math.IsInf(*w.Percent, 0) || *w.Percent < 0 || *w.Percent > 100 {
		return Meter{}, fmt.Errorf("%w (meter %s percent)", ErrEndpointChanged, name)
	}
	if limited && *w.Percent != 100 {
		return Meter{}, fmt.Errorf("%w (meter %s limited at %g%%)", ErrEndpointChanged, name, *w.Percent)
	}
	if w.ResetsAt == nil || *w.ResetsAt == "" {
		return Meter{}, fmt.Errorf("%w (meter %s reset time)", ErrEndpointChanged, name)
	}
	at, err := time.Parse(time.RFC3339Nano, *w.ResetsAt)
	if err != nil {
		return Meter{}, fmt.Errorf("%w (meter %s reset time)", ErrEndpointChanged, name)
	}
	return Meter{Name: name, UsedPercent: *w.Percent, Limited: limited, ResetsAt: at.UTC()}, nil
}
