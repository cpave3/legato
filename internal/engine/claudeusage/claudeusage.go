// Package claudeusage reads a Claude subscription's usage limits: what
// Claude Code's /usage view shows, the 5-hour and weekly windows with the
// percent used and when each resets. It asks the OAuth usage endpoint Claude
// Code itself calls, with the login Claude Code keeps.
//
// The endpoint is undocumented and behind a beta header, so it may change
// without notice: every answer this package does not understand is
// ErrEndpointChanged, never a guess. The login is read fresh for each call
// and never refreshed: a refresh rotates the refresh token and would sign
// Claude Code out. No error this package returns carries the token or the
// endpoint's body.
package claudeusage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/leaf/retryafter"
)

// DefaultBaseURL is where the usage endpoint lives.
const DefaultBaseURL = "https://api.anthropic.com"

// usagePath and betaHeader are what Claude Code 2.1.332 sends.
const (
	usagePath  = "/api/oauth/usage"
	betaHeader = "oauth-2025-04-20"
)

// requestTimeout bounds one read.
const requestTimeout = 15 * time.Second

// bodyLimit bounds the endpoint's answer, which is a few hundred bytes.
const bodyLimit = 64 << 10

// The windows this package knows. Others in the answer are ignored.
const (
	FiveHour       = "five_hour"
	SevenDay       = "seven_day"
	SevenDayOpus   = "seven_day_opus"
	SevenDaySonnet = "seven_day_sonnet"
)

var knownWindows = []string{FiveHour, SevenDay, SevenDayOpus, SevenDaySonnet}

var (
	// ErrNotSignedIn: there is no Claude.ai login to read.
	ErrNotSignedIn = errors.New("claude: not signed in")
	// ErrLoginExpired: the login's access token has expired or was refused.
	// Claude Code refreshes it the next time it is used.
	ErrLoginExpired = errors.New("claude: login expired")
	// ErrNoSubscription: the login has no subscription usage to report.
	ErrNoSubscription = errors.New("claude: no subscription usage")
	// ErrEndpointChanged: the endpoint is gone or answered in a shape this
	// package does not know.
	ErrEndpointChanged = errors.New("claude: usage endpoint changed")
)

// RateLimitedError is a 429: ask again after RetryAfter, zero when the
// endpoint did not say.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("claude: usage endpoint rate limited; retry after %s", e.RetryAfter)
	}
	return "claude: usage endpoint rate limited"
}

// Window is one usage window.
type Window struct {
	// Name is one of FiveHour, SevenDay, SevenDayOpus, SevenDaySonnet.
	Name string
	// UsedPercent is from 0 to 100.
	UsedPercent float64
	// ResetsAt is zero when the endpoint did not say.
	ResetsAt time.Time
}

// Reading is the windows the endpoint reported, in knownWindows order.
type Reading struct {
	Windows []Window
}

// Login is what a read needs from Claude Code's login.
type Login struct {
	AccessToken string
	// ExpiresAt is zero when the login does not say.
	ExpiresAt time.Time
}

// CredentialSource reads Claude Code's login, returning ErrNotSignedIn when
// there is none.
type CredentialSource interface {
	Login() (Login, error)
}

// Client reads usage. The zero value is not usable; set Credentials.
type Client struct {
	// BaseURL is DefaultBaseURL when empty.
	BaseURL     string
	Credentials CredentialSource
	// HTTP is a client that ignores the environment's proxies when nil.
	HTTP *http.Client
	// Now is time.Now when nil.
	Now func() time.Time
}

// Read asks the endpoint for the login's usage.
func (c *Client) Read(ctx context.Context) (Reading, error) {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	login, err := c.Credentials.Login()
	if err != nil {
		return Reading{}, err
	}
	if login.AccessToken == "" {
		return Reading{}, ErrNotSignedIn
	}
	if !login.ExpiresAt.IsZero() && !now().Before(login.ExpiresAt) {
		return Reading{}, ErrLoginExpired
	}

	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+usagePath, nil)
	if err != nil {
		return Reading{}, errors.New("claude: building the usage request")
	}
	req.Header.Set("Authorization", "Bearer "+login.AccessToken)
	req.Header.Set("anthropic-beta", betaHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return Reading{}, transportError(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	if err != nil {
		return Reading{}, errors.New("claude: reading the usage answer failed")
	}

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized:
		return Reading{}, ErrLoginExpired
	case resp.StatusCode == http.StatusForbidden:
		return Reading{}, ErrNoSubscription
	case resp.StatusCode == http.StatusTooManyRequests:
		return Reading{}, &RateLimitedError{RetryAfter: retryafter.Parse(resp.Header.Get("Retry-After"), now())}
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return Reading{}, fmt.Errorf("%w (status %d)", ErrEndpointChanged, resp.StatusCode)
	default:
		return Reading{}, fmt.Errorf("claude: usage endpoint answered status %d", resp.StatusCode)
	}
	return decode(body)
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Transport: transport, Timeout: requestTimeout}
}

// transportError says what failed without the request, whose URL the
// standard library would quote.
func transportError(err error) error {
	var netErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		return errors.New("claude: usage endpoint timed out")
	case errors.Is(err, context.Canceled):
		return context.Canceled
	default:
		return errors.New("claude: usage endpoint unreachable")
	}
}

type wireWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *string  `json:"resets_at"`
}

func decode(body []byte) (Reading, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return Reading{}, fmt.Errorf("%w (not JSON)", ErrEndpointChanged)
	}
	var reading Reading
	known := false
	for _, name := range knownWindows {
		value, ok := raw[name]
		known = known || ok
		if !ok || string(value) == "null" {
			continue
		}
		var w wireWindow
		if err := json.Unmarshal(value, &w); err != nil || w.Utilization == nil {
			return Reading{}, fmt.Errorf("%w (window %s)", ErrEndpointChanged, name)
		}
		window := Window{Name: name, UsedPercent: *w.Utilization}
		if w.ResetsAt != nil && *w.ResetsAt != "" {
			at, err := time.Parse(time.RFC3339Nano, *w.ResetsAt)
			if err != nil {
				return Reading{}, fmt.Errorf("%w (window %s reset time)", ErrEndpointChanged, name)
			}
			window.ResetsAt = at.UTC()
		}
		reading.Windows = append(reading.Windows, window)
	}
	if !known {
		return Reading{}, fmt.Errorf("%w (no known window)", ErrEndpointChanged)
	}
	if len(reading.Windows) == 0 {
		return Reading{}, ErrNoSubscription
	}
	return reading, nil
}
