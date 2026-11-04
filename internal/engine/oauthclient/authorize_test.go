package oauthclient_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/oauthclient"
)

func startAuthorization(t *testing.T) (*oauthclient.Authorization, url.Values) {
	t.Helper()
	f := newFakeServer(t)
	a, err := oauthclient.StartAuthorization(f.metadata(t), "client-1", "argus:reply")
	if err != nil {
		t.Fatalf("StartAuthorization: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	u, err := url.Parse(a.URL())
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	return a, u.Query()
}

// browse follows the redirect as the browser would. It runs in its own
// goroutine, so it reports a failure as a status of 0.
func browse(a *oauthclient.Authorization, query string) (int, string) {
	resp, err := http.Get(a.RedirectURI() + "?" + query)
	if err != nil {
		return 0, err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func wait(t *testing.T, a *oauthclient.Authorization) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.Wait(ctx)
}

func TestAuthorizationURL(t *testing.T) {
	f := newFakeServer(t)
	md := f.metadata(t)
	a, err := oauthclient.StartAuthorization(md, "client-1", "argus:reply")
	if err != nil {
		t.Fatalf("StartAuthorization: %v", err)
	}
	defer a.Close()

	u, _ := url.Parse(a.URL())
	if got := u.Scheme + "://" + u.Host + u.Path; got != md.AuthorizationEndpoint {
		t.Fatalf("endpoint = %q", got)
	}
	q := u.Query()
	for k, want := range map[string]string{
		"response_type":         "code",
		"client_id":             "client-1",
		"scope":                 "argus:reply",
		"code_challenge_method": "S256",
		"resource":              md.Resource,
		"redirect_uri":          a.RedirectURI(),
	} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
	if !regexp.MustCompile(`^http://127\.0\.0\.1:[1-9][0-9]*/callback$`).MatchString(a.RedirectURI()) {
		t.Errorf("redirect URI = %q, want the bound loopback port", a.RedirectURI())
	}
	if len(q.Get("state")) < 16 {
		t.Errorf("state = %q, want a random value", q.Get("state"))
	}
	sum := sha256.Sum256([]byte(a.Verifier()))
	if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Errorf("code_challenge is not S256 of the verifier")
	}
}

func TestAuthorizationVerifierIsAValidPKCEVerifier(t *testing.T) {
	a, _ := startAuthorization(t)
	if !regexp.MustCompile(`^[A-Za-z0-9\-._~]{43,128}$`).MatchString(a.Verifier()) {
		t.Fatalf("verifier %q is not 43–128 unreserved characters", a.Verifier())
	}
}

func TestAuthorizationStateAndVerifierDifferEachTime(t *testing.T) {
	a1, q1 := startAuthorization(t)
	a2, q2 := startAuthorization(t)
	if q1.Get("state") == q2.Get("state") || a1.Verifier() == a2.Verifier() {
		t.Fatal("state or verifier repeated")
	}
}

func TestAuthorizationKeepsTheEndpointsOwnQuery(t *testing.T) {
	f := newFakeServer(t)
	f.asMD["authorization_endpoint"] = f.URL + "/authorize?tenant=a"
	a, err := oauthclient.StartAuthorization(f.metadata(t), "client-1", "argus:reply")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	u, _ := url.Parse(a.URL())
	if u.Query().Get("tenant") != "a" || u.Query().Get("client_id") != "client-1" {
		t.Fatalf("URL = %s", a.URL())
	}
}

func TestWaitReturnsTheCodeFromTheBrowser(t *testing.T) {
	a, q := startAuthorization(t)
	done := make(chan struct{})
	var status int
	var page string
	go func() {
		defer close(done)
		status, page = browse(a, url.Values{"code": {"the-code"}, "state": {q.Get("state")}}.Encode())
	}()

	code, err := wait(t, a)
	if err != nil || code != "the-code" {
		t.Fatalf("Wait = %q, %v", code, err)
	}
	<-done
	if status != http.StatusOK || !strings.Contains(page, "close this tab") {
		t.Fatalf("page = %d %q", status, page)
	}
}

func TestWaitClosesTheListener(t *testing.T) {
	a, q := startAuthorization(t)
	go browse(a, url.Values{"code": {"c"}, "state": {q.Get("state")}}.Encode())
	if _, err := wait(t, a); err != nil {
		t.Fatal(err)
	}
	if resp, err := http.Get(a.RedirectURI()); err == nil {
		resp.Body.Close()
		t.Fatal("the listener still answers after Wait")
	}
}

func TestWaitRefusesAStateMismatchFromTheBrowser(t *testing.T) {
	a, _ := startAuthorization(t)
	done := make(chan string)
	go func() {
		_, page := browse(a, url.Values{"code": {"the-code"}, "state": {"forged"}}.Encode())
		done <- page
	}()

	code, err := wait(t, a)
	if !errors.Is(err, oauthclient.ErrStateMismatch) || code != "" {
		t.Fatalf("Wait = %q, %v; want ErrStateMismatch and no code", code, err)
	}
	if page := <-done; strings.Contains(page, "the-code") {
		t.Fatalf("page echoes the code: %q", page)
	}
}

func TestWaitSurfacesTheRedirectsError(t *testing.T) {
	a, q := startAuthorization(t)
	go browse(a, url.Values{
		"error": {"access_denied"}, "error_description": {"the person said no"}, "state": {q.Get("state")},
	}.Encode())

	_, err := wait(t, a)
	var oerr *oauthclient.Error
	if !errors.As(err, &oerr) || oerr.Code != "access_denied" || oerr.Description != "the person said no" {
		t.Fatalf("err = %v, want access_denied", err)
	}
}

func TestPasteGivesWaitTheCode(t *testing.T) {
	a, q := startAuthorization(t)
	redirected := a.RedirectURI() + "?" + url.Values{"code": {"pasted"}, "state": {q.Get("state")}}.Encode()
	if err := a.Paste(redirected); err != nil {
		t.Fatalf("Paste: %v", err)
	}
	code, err := wait(t, a)
	if err != nil || code != "pasted" {
		t.Fatalf("Wait = %q, %v", code, err)
	}
}

func TestPasteAcceptsTheAddressOnAnotherPort(t *testing.T) {
	// The browser elsewhere tried 127.0.0.1 on its own machine; the port is
	// what Legato asked for, but nothing requires it to match.
	a, q := startAuthorization(t)
	err := a.Paste("http://127.0.0.1:1/callback?code=c&state=" + url.QueryEscape(q.Get("state")))
	if err != nil {
		t.Fatalf("Paste: %v", err)
	}
	if code, err := wait(t, a); err != nil || code != "c" {
		t.Fatalf("Wait = %q, %v", code, err)
	}
}

func TestPasteRefusesAStateMismatch(t *testing.T) {
	a, _ := startAuthorization(t)
	err := a.Paste(a.RedirectURI() + "?code=c&state=forged")
	if !errors.Is(err, oauthclient.ErrStateMismatch) {
		t.Fatalf("Paste = %v, want ErrStateMismatch", err)
	}
	if code, err := wait(t, a); !errors.Is(err, oauthclient.ErrStateMismatch) || code != "" {
		t.Fatalf("Wait = %q, %v; want ErrStateMismatch and no code", code, err)
	}
}

func TestPasteSurfacesTheRedirectsError(t *testing.T) {
	a, q := startAuthorization(t)
	err := a.Paste(a.RedirectURI() + "?error=access_denied&state=" + url.QueryEscape(q.Get("state")))
	var oerr *oauthclient.Error
	if !errors.As(err, &oerr) || oerr.Code != "access_denied" {
		t.Fatalf("Paste = %v, want access_denied", err)
	}
	if _, err := wait(t, a); !errors.As(err, &oerr) {
		t.Fatalf("Wait = %v, want access_denied", err)
	}
}

func TestPasteOfSomethingElseLeavesTheAuthorizationOpen(t *testing.T) {
	a, q := startAuthorization(t)
	if err := a.Paste("not an address"); err == nil {
		t.Fatal("Paste accepted an address with no code")
	}
	if err := a.Paste(a.RedirectURI() + "?code=late&state=" + url.QueryEscape(q.Get("state"))); err != nil {
		t.Fatalf("second Paste: %v", err)
	}
	if code, err := wait(t, a); err != nil || code != "late" {
		t.Fatalf("Wait = %q, %v", code, err)
	}
}

func TestWaitStopsWithItsContext(t *testing.T) {
	a, _ := startAuthorization(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait = %v, want context.Canceled", err)
	}
	if resp, err := http.Get(a.RedirectURI()); err == nil {
		resp.Body.Close()
		t.Fatal("the listener still answers after Wait gave up")
	}
}

func TestErrorsDoNotCarryTheCodeOrVerifier(t *testing.T) {
	a, _ := startAuthorization(t)
	err := a.Paste(a.RedirectURI() + "?code=secret-code&state=forged")
	if err == nil || strings.Contains(err.Error(), "secret-code") || strings.Contains(err.Error(), a.Verifier()) {
		t.Fatalf("err = %v", err)
	}
}

func TestAnAuthorizationCanReturnToAnAddressOfTheCallers(t *testing.T) {
	f := newFakeServer(t)
	const back = "https://legato.local:3080/oauth/callback"
	a, err := oauthclient.StartRedirectAuthorization(f.metadata(t), "client-1", "read:tasks write:tasks", back)
	if err != nil {
		t.Fatalf("StartRedirectAuthorization: %v", err)
	}
	u, _ := url.Parse(a.URL())
	q := u.Query()
	if q.Get("redirect_uri") != back || a.RedirectURI() != back || q.Get("state") != a.State() || q.Get("scope") != "read:tasks write:tasks" {
		t.Fatalf("query = %v, state %q", q, a.State())
	}
	// The caller's own handler hands it the address the browser came back to.
	if err := a.Paste(back + "?code=c-9&state=" + url.QueryEscape(a.State())); err != nil {
		t.Fatalf("Paste: %v", err)
	}
	if code, err := wait(t, a); err != nil || code != "c-9" {
		t.Fatalf("Wait = %q, %v", code, err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
