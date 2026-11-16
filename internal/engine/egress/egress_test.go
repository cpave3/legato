package egress

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fixture returns a TLS server on loopback and a policy that may reach it
// only because the test opts into the loopback fixture.
func fixture(t *testing.T, h http.Handler) (*httptest.Server, Policy) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return srv, Policy{RootCAs: srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs, LoopbackFixture: true}
}

func ok(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") }

func TestProductionPolicyRefusesLoopbackConnections(t *testing.T) {
	var reached atomic.Bool
	srv, p := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Store(true) }))
	p.LoopbackFixture = false
	_, err := p.Client().Get(srv.URL)
	if !errors.Is(err, ErrDenied) || reached.Load() {
		t.Fatalf("loopback request: %v, reached %v", err, reached.Load())
	}
	if Classify(err) != "egress_denied" {
		t.Fatalf("classified %q", Classify(err))
	}
}

func TestTheLoopbackFixtureReachesOnlyLoopback(t *testing.T) {
	srv, p := fixture(t, http.HandlerFunc(ok))
	resp, err := p.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// The fixture opens loopback, not private networks or metadata.
	p.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.0.0.5")}, nil
	}
	if _, err := p.Client().Get("https://internal.example.test:" + srv.URL[strings.LastIndex(srv.URL, ":")+1:]); !errors.Is(err, ErrDenied) {
		t.Fatalf("fixture reached a private address: %v", err)
	}
}

// A name is checked by the addresses it resolves to when the connection is
// made, and the connection goes to the address that was checked, so a name
// that rebinds to an internal address is refused.
func TestNamesAreCheckedByTheAddressActuallyDialled(t *testing.T) {
	var reached atomic.Bool
	srv, p := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Store(true) }))
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	p.LoopbackFixture = false
	// The name passed the static check; at connection time it resolves to
	// loopback (a rebinding attack's second answer).
	p.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	if err := p.CheckURL("https://rebind.example.test:" + port + "/mcp"); err != nil {
		t.Fatal(err) // the static check does not resolve
	}
	_, err := p.Client().Get("https://rebind.example.test:" + port + "/mcp")
	if !errors.Is(err, ErrDenied) || reached.Load() {
		t.Fatalf("rebound name: %v, reached %v", err, reached.Load())
	}
	// Any internal address among several refuses the whole name.
	p.Resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.215.14"), netip.MustParseAddr("169.254.169.254")}, nil
	}
	if _, err := p.Client().Get("https://mixed.example.test:" + port); !errors.Is(err, ErrDenied) {
		t.Fatalf("mixed resolution: %v", err)
	}
}

func TestAddressesAreClassified(t *testing.T) {
	for addr, want := range map[string]bool{
		"93.184.215.14":        true,
		"2606:4700::1111":      true,
		"127.0.0.1":            false,
		"::1":                  false,
		"10.1.2.3":             false,
		"172.16.0.1":           false,
		"192.168.1.1":          false,
		"100.100.1.1":          false, // CGNAT and tailnets
		"169.254.169.254":      false, // cloud metadata
		"fd00:ec2::254":        false,
		"fe80::1":              false,
		"0.0.0.0":              false,
		"::":                   false,
		"224.0.0.1":            false,
		"255.255.255.255":      false,
		"::ffff:127.0.0.1":     false,
		"::ffff:93.184.215.14": true,
		"64:ff9b::7f00:1":      false, // NAT64 can embed anything
		"2002:7f00:1::":        false, // 6to4
		"198.18.0.1":           false,
		"192.0.2.1":            false,
	} {
		if got := (Policy{}).allowed(netip.MustParseAddr(addr)); got != want {
			t.Errorf("%s: allowed %v, want %v", addr, got, want)
		}
	}
}

func TestPrivateNetworksNeedAnExplicitNarrowPolicy(t *testing.T) {
	tailnet := netip.MustParsePrefix("100.64.0.0/10")
	p := Policy{Private: []netip.Prefix{tailnet}}
	if !p.allowed(netip.MustParseAddr("100.100.1.1")) || p.allowed(netip.MustParseAddr("10.0.0.1")) {
		t.Fatal("private policy did not apply exactly")
	}
	for _, bad := range []string{"0.0.0.0/0", "127.0.0.0/8", "169.254.0.0/16", "10.0.0.0/7", "::/0", "fe80::/10", "8.8.8.0/24"} {
		if err := CheckPrivate(netip.MustParsePrefix(bad)); err == nil {
			t.Errorf("%s accepted as a private network", bad)
		}
	}
	for _, good := range []string{"10.20.0.0/16", "192.168.1.0/24", "100.64.0.0/10", "fd7a:115c:a1e0::/48"} {
		if err := CheckPrivate(netip.MustParsePrefix(good)); err != nil {
			t.Errorf("%s refused: %v", good, err)
		}
	}
	// Metadata is never reachable, whatever the policy says.
	if (Policy{Private: []netip.Prefix{netip.MustParsePrefix("fd00::/8"), tailnet}}).allowed(netip.MustParseAddr("fd00:ec2::254")) || p.allowed(netip.MustParseAddr("100.100.100.200")) {
		t.Fatal("metadata reachable through a private policy")
	}
}

func TestURLsAreCheckedBeforeAnyConnection(t *testing.T) {
	p := Policy{}
	for _, bad := range []string{
		"http://mcp.example.test/mcp",
		"ftp://mcp.example.test/",
		"file:///etc/passwd",
		"https://user:pass@mcp.example.test/mcp",
		"https://mcp.example.test/mcp#x",
		"https://mcp.example.test/mcp?access_token=abc",
		"https://mcp.example.test/mcp?API_KEY=abc",
		"https://mcp.example.test/mcp?sig=abc",
		"https://127.0.0.1/mcp",
		"https://[::1]/mcp",
		"https://169.254.169.254/latest",
		"https://localhost/mcp",
		"https://metadata.google.internal/",
		"//mcp.example.test/",
		"https:///nohost",
		"https://mcp.example.test/mcp\n",
	} {
		if err := p.CheckURL(bad); !errors.Is(err, ErrDenied) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	for _, good := range []string{"https://mcp.example.test/mcp", "https://mcp.example.test:8443/mcp?workspace=1", "https://93.184.215.14/mcp"} {
		if err := p.CheckURL(good); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
	// The fixture admits plain HTTP to a loopback literal only.
	f := Policy{LoopbackFixture: true}
	if f.CheckURL("http://127.0.0.1:9/x") != nil || f.CheckURL("http://mcp.example.test/x") == nil {
		t.Fatal("fixture URL rule")
	}
}

func TestRedirectsStayOnTheSameOriginAndOnlyForReads(t *testing.T) {
	var other atomic.Bool
	elsewhere, _ := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { other.Store(true) }))
	mux := http.NewServeMux()
	mux.HandleFunc("/same", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok", http.StatusFound) })
	mux.HandleFunc("/ok", ok)
	mux.HandleFunc("/away", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/x", http.StatusFound)
	})
	mux.HandleFunc("/post", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok", http.StatusTemporaryRedirect) })
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/loop", http.StatusFound) })
	srv, p := fixture(t, mux)
	// Both servers share the test CA.
	p.RootCAs = srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	p.RootCAs.AddCert(elsewhere.Certificate())
	c := p.Client()
	resp, err := c.Get(srv.URL + "/same")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, err := c.Get(srv.URL + "/away"); !errors.Is(err, ErrDenied) || other.Load() {
		t.Fatalf("cross-origin redirect: %v, reached %v", err, other.Load())
	}
	if _, err := c.Post(srv.URL+"/post", "application/x-www-form-urlencoded", strings.NewReader("code=secret")); !errors.Is(err, ErrDenied) {
		t.Fatalf("redirected POST: %v", err)
	}
	if _, err := c.Get(srv.URL + "/loop"); !errors.Is(err, ErrDenied) {
		t.Fatalf("redirect loop: %v", err)
	}
}

func TestResponsesAreBoundedAndTimed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) { w.Write(make([]byte, 2048)) })
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	srv, p := fixture(t, mux)
	p.MaxResponseBytes = 1024
	p.Timeout = 200 * time.Millisecond
	resp, err := p.Client().Get(srv.URL + "/big")
	if err == nil {
		_, err = io.ReadAll(resp.Body)
		resp.Body.Close()
	}
	if !errors.Is(err, ErrTooLarge) || Classify(err) != "too_large" {
		t.Fatalf("oversized body: %v", err)
	}
	if _, err := p.Client().Get(srv.URL + "/slow"); err == nil || Classify(err) != "unreachable" {
		t.Fatalf("slow server: %v (%s)", err, Classify(err))
	}
}

func TestTLSIsVerified(t *testing.T) {
	srv, p := fixture(t, http.HandlerFunc(ok))
	p.RootCAs = nil // the system roots do not trust the test CA
	_, err := p.Client().Get(srv.URL)
	if err == nil || Classify(err) != "tls" {
		t.Fatalf("untrusted certificate: %v (%s)", err, Classify(err))
	}
}

func TestEnvironmentProxiesAreNotUsed(t *testing.T) {
	var proxied atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxied.Store(true) }))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	srv, p := fixture(t, http.HandlerFunc(ok))
	resp, err := p.Client().Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if proxied.Load() {
		t.Fatal("request went through an environment proxy")
	}
}

func TestOnlyTestsUseTheLoopbackFixture(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(here), "..", "..", "..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git" || strings.HasPrefix(d.Name(), ".legato")) {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || filepath.Dir(path) == filepath.Dir(here) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(b), "LoopbackFixture") {
			t.Errorf("%s uses the loopback test fixture outside tests", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
