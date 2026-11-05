package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// phrasePairing asks the Legato web server running on this machine for a
// pairing request, and follows it.
type phrasePairing struct {
	base   string
	token  string
	client *http.Client
	poll   time.Duration
}

// localPhrasePairing reaches the server on this machine. Over HTTPS it trusts
// the generated CA when there is one; otherwise it does not check the
// certificate, which is safe only because the address is the loopback one.
func localPhrasePairing(scheme, port, token, caFile string) *phrasePairing {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if scheme == "https" {
		cfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // loopback only
		if pem, err := os.ReadFile(caFile); err == nil {
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(pem) {
				cfg = &tls.Config{RootCAs: pool, ServerName: "localhost"}
			}
		}
		transport.TLSClientConfig = cfg
	}
	return &phrasePairing{
		base:   scheme + "://localhost:" + port,
		token:  token,
		client: &http.Client{Transport: transport, Timeout: 5 * time.Second},
		poll:   time.Second,
	}
}

// pairingState is a request as the server shows it, with the QR code of it.
type pairingState struct {
	service.PairingStatus
	PairURI string `json:"pair_uri"`
}

// phraseQR is what legato pair's QR code says: the request's phrase and an
// address the phone can use. --host names that address; otherwise the
// server's own choice, else the address legato pair worked out.
func phraseQR(opened pairingState, hostFlag, serverURL string) string {
	if hostFlag == "" && opened.PairURI != "" {
		return opened.PairURI
	}
	return "legato://pair?" + url.Values{"url": {serverURL}, "phrase": {strings.Join(opened.Phrase, "-")}}.Encode()
}

func (p *phrasePairing) request(ctx context.Context, method string) (pairingState, error) {
	return p.requestWith(ctx, method, nil)
}

func (p *phrasePairing) requestWith(ctx context.Context, method string, body []byte) (pairingState, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.base+"/api/pairing", bytes.NewReader(body))
	if err != nil {
		return pairingState{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return pairingState{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var failure struct{ Error string }
		_ = json.NewDecoder(resp.Body).Decode(&failure)
		if failure.Error == "" {
			failure.Error = resp.Status
		}
		return pairingState{}, fmt.Errorf("the server answered: %s", failure.Error)
	}
	var st pairingState
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return pairingState{}, err
	}
	return st, nil
}

// open starts a pairing request for a phone and returns its phrase.
func (p *phrasePairing) open(ctx context.Context) (pairingState, error) {
	return p.request(ctx, http.MethodPost)
}

// openFor starts a pairing request for a phone or a main instance.
func (p *phrasePairing) openFor(ctx context.Context, purpose service.PairingPurpose) (pairingState, error) {
	body, err := json.Marshal(map[string]service.PairingPurpose{"purpose": purpose})
	if err != nil {
		return pairingState{}, err
	}
	return p.requestWith(ctx, http.MethodPost, body)
}

// mainPairingInstructions is what legato pair --main tells the person to
// enter on the main instance.
func mainPairingInstructions(address string, phrase []string) string {
	return fmt.Sprintf("On the main instance, open Settings › Workers › Add a worker and enter\n\n    Address:  %s\n    Phrase:   %s\n\n", address, strings.Join(phrase, " "))
}

// wait follows the request until it is no longer open.
func (p *phrasePairing) wait(ctx context.Context) (pairingState, error) {
	ticker := time.NewTicker(p.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return pairingState{}, ctx.Err()
		case <-ticker.C:
		}
		st, err := p.request(ctx, http.MethodGet)
		if err != nil {
			if ctx.Err() != nil {
				return pairingState{}, ctx.Err()
			}
			continue
		}
		if st.State != service.PairingOpen {
			return st, nil
		}
	}
}

// cancel ends the request, as when the person stops legato pair.
func (p *phrasePairing) cancel() {
	ctx, done := context.WithTimeout(context.Background(), 3*time.Second)
	defer done()
	_, _ = p.request(ctx, http.MethodDelete)
}

// pairingOutcome is what legato pair says when the request ends.
func pairingOutcome(st service.PairingStatus) (string, bool) {
	switch st.State {
	case service.PairingPaired:
		if st.Purpose == service.PairingMain {
			return "Paired with main instance " + st.Device + ". It can now hand tasks to this Legato.", true
		}
		if st.Device != "" {
			return "Paired: " + st.Device, true
		}
		return "Paired.", true
	case service.PairingExpired:
		return "The phrase expired. Run legato pair again.", false
	case service.PairingExhausted:
		return "Too many wrong phrases. Run legato pair again.", false
	case service.PairingCancelled:
		return "Pairing was cancelled.", false
	}
	return "Pairing ended (" + string(st.State) + ").", false
}

func formatPhrase(words []string) string {
	return strings.Join(words, "  ")
}
