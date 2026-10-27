package main

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

func phraseServer(t *testing.T) (*phrasePairing, *service.PairingService) {
	t.Helper()
	srv := server.New(nil, nil, nil, ":0")
	srv.SetAuthToken("tok")
	svc := service.NewPairingService(func() (service.PairingSecret, error) {
		return service.PairingSecret{Token: "tok"}, nil
	})
	srv.SetPairingService(svc)
	web := httptest.NewServer(srv.Handler())
	t.Cleanup(web.Close)
	return &phrasePairing{base: web.URL, token: "tok", client: web.Client(), poll: 10 * time.Millisecond}, svc
}

func TestPairOpensARequestOnTheRunningServer(t *testing.T) {
	p, svc := phraseServer(t)
	st, err := p.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != service.PairingOpen || len(st.Phrase) != 4 {
		t.Fatalf("opened %+v", st)
	}
	if svc.Status().State != service.PairingOpen {
		t.Fatal("the server has no open request")
	}
}

func TestPairWaitsUntilTheRequestEnds(t *testing.T) {
	p, svc := phraseServer(t)
	if _, err := p.open(context.Background()); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		svc.Cancel()
	}()
	st, err := p.wait(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != service.PairingCancelled {
		t.Fatalf("ended %+v", st)
	}
}

func TestStoppingPairCancelsTheRequest(t *testing.T) {
	p, svc := phraseServer(t)
	if _, err := p.open(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if _, err := p.wait(ctx); err == nil {
		t.Fatal("wait went on after it was stopped")
	}
	p.cancel()
	if svc.Status().State != service.PairingCancelled {
		t.Fatalf("status %+v", svc.Status())
	}
}

func TestPairWithNoServerRunningSaysSo(t *testing.T) {
	p := &phrasePairing{base: "http://127.0.0.1:1", token: "tok", client: httptest.NewServer(nil).Client()}
	if _, err := p.open(context.Background()); err == nil {
		t.Fatal("opened a request with no server")
	}
}

func TestPairsQRCodeCarriesThePhraseNotTheToken(t *testing.T) {
	opened := pairingState{
		PairingStatus: service.PairingStatus{State: service.PairingOpen, Phrase: []string{"acid", "acorn", "acre", "acts"}},
		PairURI:       "legato://pair?phrase=acid-acorn-acre-acts&url=http%3A%2F%2F192.168.1.200%3A3080",
	}
	if got := phraseQR(opened, "", "http://erebus:3080"); got != opened.PairURI {
		t.Fatalf("without --host: %q", got)
	}
	want := "legato://pair?phrase=acid-acorn-acre-acts&url=http%3A%2F%2F10.0.0.9%3A3080"
	if got := phraseQR(opened, "10.0.0.9", "http://10.0.0.9:3080"); got != want {
		t.Fatalf("with --host: %q", got)
	}
	opened.PairURI = ""
	if got := phraseQR(opened, "", "http://erebus:3080"); got != "legato://pair?phrase=acid-acorn-acre-acts&url=http%3A%2F%2Ferebus%3A3080" {
		t.Fatalf("with no address from the server: %q", got)
	}
}
