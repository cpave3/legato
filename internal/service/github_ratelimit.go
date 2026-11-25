package service

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// secondaryLimitWait is how long a secondary rate limit that names no
// retry-after is waited out.
const secondaryLimitWait = time.Minute

// GitHubRateLimit is GitHub limiting the calls of one App installation, to
// its REST API or its GraphQL API. Every host-side GitHub call (the reviewer
// App's and the author App's) returns it for a limit, and returns it again
// without asking GitHub while the installation backs off: until RetryAt.
type GitHubRateLimit struct {
	// Installation is the App installation limited: the credential and
	// the account it is installed on, "<credential>@<owner>" (an App has
	// one installation per account).
	Installation string
	// Secondary is a secondary limit, waited out for RetryAfter: GitHub's
	// retry-after, else a minute. A primary limit waits for Reset.
	Secondary  bool
	RetryAfter time.Duration
	Reset      time.Time
	// RetryAt is when the installation may call GitHub again.
	RetryAt time.Time
	Status  int
	Message string
}

func (e *GitHubRateLimit) Error() string {
	kind := "rate limit"
	if e.Secondary {
		kind = "secondary rate limit"
	}
	return fmt.Sprintf("GitHub %s on %s: %s (try again at %s)", kind, e.Installation, e.Message, e.RetryAt.Format(time.RFC3339))
}

// readRateLimit says whether GitHub's answer is a rate limit, and which: a
// 403 or 429 with x-ratelimit-remaining 0, a retry-after or a rate-limit
// message, or a GraphQL RATE_LIMITED error. graphQLLimited says the
// answer's GraphQL errors named one.
func readRateLimit(resp *http.Response, message string, graphQLLimited bool, now time.Time) *GitHubRateLimit {
	remaining := resp.Header.Get("x-ratelimit-remaining")
	retryAfter := resp.Header.Get("retry-after")
	refused := resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests
	lower := strings.ToLower(message)
	switch {
	case graphQLLimited:
	case resp.StatusCode == http.StatusTooManyRequests:
	case refused && (remaining == "0" || retryAfter != "" || strings.Contains(lower, "rate limit")):
	default:
		return nil
	}
	limit := &GitHubRateLimit{Status: resp.StatusCode, Message: message}
	reset, resetErr := strconv.ParseInt(resp.Header.Get("x-ratelimit-reset"), 10, 64)
	switch {
	case retryAfter != "":
		limit.Secondary, limit.RetryAfter = true, parseRetryAfter(retryAfter, now)
	case remaining == "0" && resetErr == nil && !strings.Contains(lower, "secondary"):
		limit.Reset = time.Unix(reset, 0)
	default:
		limit.Secondary, limit.RetryAfter = true, secondaryLimitWait
	}
	if limit.Secondary {
		limit.RetryAt = now.Add(limit.RetryAfter)
	} else {
		limit.RetryAt = limit.Reset
	}
	return limit
}

// parseRetryAfter reads retry-after: seconds, or an HTTP date.
func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return secondaryLimitWait
}

// githubInstallation names the installation a credential calls repo with.
func githubInstallation(bc brokerCredential, repo string) string {
	owner, _, _ := strings.Cut(repo, "/")
	return bc.config.Name + "@" + strings.ToLower(owner)
}

// githubBackingOff is the limit an installation still waits out, or nil:
// a call it makes is not sent.
func (c *CredentialChannels) githubBackingOff(bc brokerCredential, repo string) error {
	key := githubInstallation(bc, repo)
	c.github.mu.Lock()
	defer c.github.mu.Unlock()
	limit, ok := c.github.limits[key]
	if !ok {
		return nil
	}
	if !c.broker.now().Before(limit.RetryAt) {
		delete(c.github.limits, key)
		return nil
	}
	held := limit
	return &held
}

// githubLimited names the installation of a limit GitHub answered with and
// keeps it until RetryAt, so the installation's later calls wait; any other
// error is returned as it is.
func (c *CredentialChannels) githubLimited(bc brokerCredential, repo string, err error) error {
	limit, ok := err.(*GitHubRateLimit)
	if !ok {
		return err
	}
	limit.Installation = githubInstallation(bc, repo)
	c.github.mu.Lock()
	defer c.github.mu.Unlock()
	if c.github.limits == nil {
		c.github.limits = map[string]GitHubRateLimit{}
	}
	if held, ok := c.github.limits[limit.Installation]; !ok || limit.RetryAt.After(held.RetryAt) {
		c.github.limits[limit.Installation] = *limit
	}
	return limit
}
