package oauthclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/oauthclient"
)

func TestLoopbackReceiverRegistersItsBoundRedirectAndCleansTheBrowserURL(t *testing.T) {
	f := newFakeServer(t)
	var registered string
	f.mux.HandleFunc("POST /register", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Redirects []string `json:"redirect_uris"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Redirects) != 1 {
			t.Error("invalid registration")
			http.Error(w, "bad registration", 400)
			return
		}
		registered = req.Redirects[0]
		writeJSON(w, 201, map[string]string{"client_id": "client"})
	})
	var received oauthclient.AuthorizationResponse
	receiver, err := oauthclient.ListenLoopback(context.Background(), func(_ context.Context, response oauthclient.AuthorizationResponse) error {
		received = response
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	redirect, _ := url.Parse(receiver.RedirectURI())
	if redirect.Scheme != "http" || redirect.Hostname() != "127.0.0.1" || redirect.Port() == "" {
		t.Fatal("receiver did not bind an IP loopback address")
	}
	_, err = oauthclient.Register(context.Background(), f.Client(), f.metadata(t), oauthclient.RegistrationRequest{ClientName: "Legato", RedirectURI: receiver.RedirectURI()})
	if err != nil {
		t.Fatal(err)
	}
	if registered != receiver.RedirectURI() {
		t.Fatal("registration differs from bound receiver")
	}
	resp, err := http.Get(receiver.RedirectURI() + "?" + url.Values{"code": {"sentinel-code"}, "state": {"expected-state"}, "iss": {f.URL}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || resp.Request.URL.Path != "/complete" || resp.Request.URL.RawQuery != "" {
		t.Fatal("browser did not reach a clean completion URL")
	}
	if received.Code != "sentinel-code" || received.State != "expected-state" || received.Issuer != f.URL {
		t.Fatal("callback was not delivered to its owner")
	}
	if strings.Contains(string(body), "sentinel-code") || strings.Contains(string(body), "expected-state") {
		t.Fatal("completion page leaked authorization material")
	}
}

func TestLoopbackReceiverRefusesMalformedUnrelatedAndRepeatedCallbacks(t *testing.T) {
	calls := 0
	receiver, err := oauthclient.ListenLoopback(context.Background(), func(_ context.Context, r oauthclient.AuthorizationResponse) error {
		calls++
		if r.State != "expected" {
			return errors.New("sensitive-provider-description")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	browser := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, query := range []string{"code=sentinel&state=expected&state=forged", "code=sentinel&error=access_denied&state=expected", "code=sentinel&state=%zz", "code=sentinel&state=forged"} {
		resp, err := browser.Get(receiver.RedirectURI() + "?" + query)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || strings.Contains(string(body), "sentinel") || strings.Contains(string(body), "sensitive-provider-description") {
			t.Fatal("invalid callback accepted or leaked response")
		}
	}
	if calls != 1 {
		t.Fatal("malformed callbacks reached transaction owner")
	}
	req, _ := http.NewRequest("GET", receiver.RedirectURI()+"?code=sentinel&state=expected", nil)
	req.Host = "localhost.attacker.invalid"
	resp, err := browser.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 || calls != 1 {
		t.Fatal("unrelated Host reached transaction owner")
	}
	resp, err = browser.Get(receiver.RedirectURI() + "?code=sentinel&state=expected")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 303 || calls != 2 {
		t.Fatal("invalid callback consumed legitimate response")
	}
	resp, err = browser.Get(receiver.RedirectURI() + "?code=replay&state=expected")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 409 || calls != 2 {
		t.Fatal("replay reached transaction owner")
	}
}

func TestLoopbackReceiverClosesWithItsAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	receiver, err := oauthclient.ListenLoopback(ctx, func(context.Context, oauthclient.AuthorizationResponse) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	cancel()
	browser := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(time.Second)
	for {
		resp, err := browser.Get(receiver.RedirectURI())
		if err != nil {
			break
		}
		resp.Body.Close()
		if time.Now().After(deadline) {
			t.Fatal("cancelled attempt still owns receiver")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLoopbackReceiverRefusesConcurrentDeliveryWithoutWaitingForExchange(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	receiver, err := oauthclient.ListenLoopback(context.Background(), func(context.Context, oauthclient.AuthorizationResponse) error { close(entered); <-release; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	browser := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	first := make(chan error, 1)
	go func() {
		response, err := browser.Get(receiver.RedirectURI() + "?code=one&state=expected")
		if err == nil {
			response.Body.Close()
		}
		first <- err
	}()
	<-entered
	response, err := browser.Get(receiver.RedirectURI() + "?code=two&state=expected")
	close(release)
	if err != nil {
		t.Fatalf("concurrent callback waited for the exchange: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatal("concurrent response reached the exchange")
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}
