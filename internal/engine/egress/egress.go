// Package egress is the outbound trust boundary for requests Legato makes to
// servers an operator named: an upstream MCP server and the OAuth servers
// behind it. Every request goes through one policy: HTTPS only, addresses
// checked when the connection is made (so a name that rebinds to an internal
// address is refused), no environment proxies, verified TLS, same-origin
// redirects for reads only, bounded responses and timeouts. Private networks
// are reachable only through prefixes an operator named, and loopback only as
// a literal address with Loopback; link-local and cloud metadata never are.
// It knows nothing of Legato.
package egress

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrDenied is a request the policy refuses.
	ErrDenied = errors.New("egress denied")
	// ErrTooLarge is a response larger than the policy allows.
	ErrTooLarge = errors.New("response too large")
)

const (
	DefaultTimeout          = 30 * time.Second
	DefaultMaxResponseBytes = 4 << 20
	DefaultMaxRedirects     = 3
)

// Policy is one connection's outbound rules. The zero value reaches public
// HTTPS addresses only.
type Policy struct {
	// Private are the private-network prefixes an operator allowed, each
	// checked with CheckPrivate. Nothing private is reachable otherwise.
	Private []netip.Prefix
	// LoopbackFixture lets a test reach its own loopback servers, over plain
	// HTTP to a loopback literal or HTTPS. Never set outside tests: a test in
	// this package fails if any non-test file names it.
	LoopbackFixture bool
	// Loopback lets a connection reach a loopback literal (127.0.0.1, ::1)
	// over http or https: an address the operator typed for a server run on
	// this host, such as a local Burnlite. Names stay refused, localhost and
	// a name that resolves to loopback included.
	Loopback bool
	// RootCAs, when set, replaces the system roots.
	RootCAs *x509.CertPool
	// Resolve looks a name up; the default is the system resolver.
	Resolve          func(ctx context.Context, host string) ([]netip.Addr, error)
	Timeout          time.Duration
	MaxResponseBytes int64
	MaxRedirects     int
}

func denied(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrDenied}, args...)...)
}

var (
	// allowable are the ranges an operator may open with Private.
	allowable = []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("fc00::/7"),
	}
	// special are not public, and not allowable either: documentation,
	// benchmarking, reserved, and IPv6 forms that embed an IPv4 address.
	special = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("64:ff9b::/96"),
		netip.MustParsePrefix("64:ff9b:1::/48"),
		netip.MustParsePrefix("2001::/32"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2002::/16"),
	}
	// metadata are cloud instance metadata addresses, never reachable.
	metadata = []netip.Addr{
		netip.MustParseAddr("169.254.169.254"),
		netip.MustParseAddr("169.254.170.2"),
		netip.MustParseAddr("100.100.100.200"),
		netip.MustParseAddr("fd00:ec2::254"),
	}
	metadataNames = []string{"metadata.google.internal", "metadata.goog", "instance-data", "instance-data.ec2.internal"}
)

func inAny(a netip.Addr, prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// CheckPrivate says whether an operator may allow a prefix: it must lie wholly
// within a private range (RFC 1918, shared address space, unique local).
func CheckPrivate(p netip.Prefix) error {
	p = p.Masked()
	ok := false
	for _, a := range allowable {
		if a.Addr().Is4() == p.Addr().Is4() && a.Bits() <= p.Bits() && a.Contains(p.Addr()) {
			ok = true
		}
	}
	if !ok {
		return denied("%s is not wholly within a private network range", p)
	}
	// Metadata addresses inside an allowed prefix stay unreachable: allowed
	// refuses them whatever the policy says.
	return nil
}

// allowed says whether a connection to a may be made.
func (p Policy) allowed(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() {
		return false
	}
	for _, m := range metadata {
		if a == m {
			return false
		}
	}
	if a.IsLoopback() {
		return p.LoopbackFixture
	}
	if a.IsUnspecified() || a.IsMulticast() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() || inAny(a, special) {
		return false
	}
	if a.IsPrivate() || inAny(a, allowable) {
		for _, allowed := range p.Private {
			if CheckPrivate(allowed) == nil && allowed.Contains(a) {
				return true
			}
		}
		return false
	}
	return a.IsGlobalUnicast()
}

// loopbackLiteral says whether a, given as a literal address, is reachable
// through Loopback.
func (p Policy) loopbackLiteral(a netip.Addr) bool {
	return p.Loopback && a.Unmap().IsLoopback()
}

var secretQuery = []string{"token", "access_token", "id_token", "refresh_token", "api_key", "apikey", "key", "secret", "client_secret", "password", "passwd", "auth", "authorization", "sig", "signature", "code", "session", "credential", "credentials"}

// CheckURL checks an address before any connection: the scheme, credentials,
// fragment, secret-bearing query parameters, and a host that is a literal
// address or a name that is never reachable. Names are checked again, by the
// addresses they resolve to, when a connection is made.
func (p Policy) CheckURL(raw string) error {
	if strings.ContainsFunc(raw, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return denied("the address contains spaces or control characters")
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Opaque != "" {
		return denied("not an absolute address")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return denied("the address has no host")
	}
	if u.User != nil {
		return denied("the address carries credentials")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return denied("the address has a fragment")
	}
	for name := range u.Query() {
		lower := strings.ToLower(name)
		for _, s := range secretQuery {
			if lower == s {
				return denied("the address carries a secret in its query (%s)", name)
			}
		}
	}
	addr, parseErr := netip.ParseAddr(strings.Trim(host, "[]"))
	literal := parseErr == nil
	switch u.Scheme {
	case "https":
	case "http":
		if !(p.LoopbackFixture || p.Loopback) || !literal || !addr.IsLoopback() {
			return denied("only https addresses are allowed")
		}
	default:
		return denied("only https addresses are allowed")
	}
	if literal {
		if !p.allowed(addr) && !p.loopbackLiteral(addr) {
			return denied("%s is not a public address", addr)
		}
		return nil
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return denied("localhost is not reachable")
	}
	for _, m := range metadataNames {
		if host == m {
			return denied("cloud metadata is not reachable")
		}
	}
	return nil
}

func (p Policy) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if p.Resolve != nil {
		return p.Resolve(ctx, host)
	}
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// dial resolves the host, refuses the connection if any address is not
// allowed, and connects to an address it checked.
func (p Policy) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	var addrs []netip.Addr
	if a, err := netip.ParseAddr(host); err == nil {
		if p.loopbackLiteral(a) {
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, net.JoinHostPort(a.Unmap().String(), port))
		}
		addrs = []netip.Addr{a}
	} else if addrs, err = p.resolve(ctx, host); err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%s has no address", host)
	}
	for _, a := range addrs {
		if !p.allowed(a) {
			return nil, denied("%s resolves to %s, which is not reachable", host, a.Unmap())
		}
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	var last error
	for _, a := range addrs {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(a.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

func origin(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = map[string]string{"https": "443", "http": "80"}[u.Scheme]
	}
	return u.Scheme + "://" + strings.ToLower(u.Hostname()) + ":" + port
}

// Client makes an HTTP client that applies the policy to every request and
// every redirect.
func (p Policy) Client() *http.Client {
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	max := p.MaxResponseBytes
	if max <= 0 {
		max = DefaultMaxResponseBytes
	}
	redirects := p.MaxRedirects
	if redirects <= 0 {
		redirects = DefaultMaxRedirects
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            p.dial,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: p.RootCAs},
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: 64 << 10,
		ForceAttemptHTTP2:      true,
		IdleConnTimeout:        90 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: checked{policy: p, next: transport, max: max},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= redirects {
				return denied("too many redirects")
			}
			first := via[0]
			if first.Method != http.MethodGet && first.Method != http.MethodHead {
				return denied("a %s request is not redirected", first.Method)
			}
			if origin(req.URL) != origin(first.URL) {
				return denied("a redirect to another origin is not followed")
			}
			return nil
		},
	}
}

// checked checks each request's address and bounds its response.
type checked struct {
	policy Policy
	next   http.RoundTripper
	max    int64
}

func (c checked) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := c.policy.CheckURL(r.URL.String()); err != nil {
		return nil, err
	}
	resp, err := c.next.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if resp.ContentLength > c.max {
		resp.Body.Close()
		return nil, ErrTooLarge
	}
	resp.Body = &bounded{ReadCloser: resp.Body, left: c.max}
	return resp, nil
}

type bounded struct {
	io.ReadCloser
	left int64
}

func (b *bounded) Read(p []byte) (int, error) {
	if b.left < 0 {
		return 0, ErrTooLarge
	}
	if int64(len(p)) > b.left+1 {
		p = p[:b.left+1]
	}
	n, err := b.ReadCloser.Read(p)
	if int64(n) > b.left {
		n, b.left = int(b.left), -1
		return n, ErrTooLarge
	}
	b.left -= int64(n)
	return n, err
}

// Classify turns a request's error into a fixed code: egress_denied,
// too_large, tls or unreachable ("" for nil).
func Classify(err error) string {
	var (
		unknown  x509.UnknownAuthorityError
		invalid  x509.CertificateInvalidError
		hostname x509.HostnameError
		verify   *tls.CertificateVerificationError
		record   tls.RecordHeaderError
		alert    tls.AlertError
	)
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrDenied):
		return "egress_denied"
	case errors.Is(err, ErrTooLarge):
		return "too_large"
	case errors.As(err, &verify), errors.As(err, &unknown), errors.As(err, &invalid), errors.As(err, &hostname), errors.As(err, &record), errors.As(err, &alert):
		return "tls"
	default:
		return "unreachable"
	}
}
