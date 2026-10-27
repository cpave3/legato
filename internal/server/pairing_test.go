package server

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/pairing"
	"github.com/cpave3/legato/internal/service"
)

func pairingServer() *Server {
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("the-token")
	srv.SetPairingService(service.NewPairingService(func() (service.PairingSecret, error) {
		return service.PairingSecret{Token: "the-token", Name: "desktop"}, nil
	}))
	return srv
}

// call sends body as JSON, with the token when authed, and decodes the reply.
func call(t *testing.T, srv *Server, method, path string, authed bool, body, into any) int {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	if authed {
		req.Header.Set("Authorization", "Bearer the-token")
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if into != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
			t.Fatalf("%s %s: %v in %q", method, path, err, rec.Body.String())
		}
	}
	return rec.Code
}

// phoneOverHTTP pairs as the app does, without the token. It returns the HTTP
// status of the step that ended the attempt and, on success, the secret.
func phoneOverHTTP(t *testing.T, srv *Server, words []string) (int, service.PairingSecret) {
	t.Helper()
	b64 := base64.StdEncoding
	phone, share, err := pairing.Start(pairing.PartyA, pairing.Protocol, pairing.PhraseScalar(words), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var exchanged struct {
		Session string `json:"session"`
		Share   string `json:"share"`
		Error   string `json:"error"`
	}
	if status := call(t, srv, http.MethodPost, "/api/pair/exchange", false, map[string]string{"share": b64.EncodeToString(share)}, &exchanged); status != http.StatusOK {
		return status, service.PairingSecret{}
	}
	serverShare, _ := b64.DecodeString(exchanged.Share)
	keys, err := phone.Finish(serverShare)
	if err != nil {
		t.Fatal(err)
	}
	device, _ := pairing.Seal(keys.Key, pairing.LabelPhone, []byte(`{"device":"Pixel"}`), rand.Reader)
	var confirmed struct {
		Confirmation string `json:"confirmation"`
		Sealed       string `json:"sealed"`
	}
	status := call(t, srv, http.MethodPost, "/api/pair/confirm", false, map[string]string{
		"session":      exchanged.Session,
		"confirmation": b64.EncodeToString(keys.Confirmation),
		"phone":        b64.EncodeToString(device),
	}, &confirmed)
	if status != http.StatusOK {
		return status, service.PairingSecret{}
	}
	cB, _ := b64.DecodeString(confirmed.Confirmation)
	if !keys.Confirms(cB) {
		t.Fatal("the server's confirmation does not check")
	}
	sealed, _ := b64.DecodeString(confirmed.Sealed)
	plain, err := pairing.Open(keys.Key, pairing.LabelServer, sealed)
	if err != nil {
		t.Fatal(err)
	}
	var secret service.PairingSecret
	_ = json.Unmarshal(plain, &secret)
	return status, secret
}

func TestAPhonePairsOverHTTPWithoutTheToken(t *testing.T) {
	srv := pairingServer()
	var opened service.PairingStatus
	if status := call(t, srv, http.MethodPost, "/api/pairing", true, nil, &opened); status != http.StatusOK || len(opened.Phrase) != 4 {
		t.Fatalf("open: %d %+v", status, opened)
	}

	status, secret := phoneOverHTTP(t, srv, opened.Phrase)
	if status != http.StatusOK || secret.Token != "the-token" || secret.Name != "desktop" {
		t.Fatalf("pair: %d %+v", status, secret)
	}

	var now service.PairingStatus
	call(t, srv, http.MethodGet, "/api/pairing", true, nil, &now)
	if now.State != service.PairingPaired || now.Device != "Pixel" {
		t.Fatalf("status %+v", now)
	}
}

func TestOpeningOrReadingARequestNeedsTheToken(t *testing.T) {
	srv := pairingServer()
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		if status := call(t, srv, method, "/api/pairing", false, nil, nil); status != http.StatusUnauthorized {
			t.Fatalf("%s without the token: %d", method, status)
		}
	}
}

func TestAWrongPhraseIsForbiddenAndAClosedRequestIsGone(t *testing.T) {
	srv := pairingServer()
	if status, _ := phoneOverHTTP(t, srv, []string{"acid", "acid", "acid", "acid"}); status != http.StatusGone {
		t.Fatalf("with no request: %d", status)
	}
	var opened service.PairingStatus
	call(t, srv, http.MethodPost, "/api/pairing", true, nil, &opened)
	wrong := []string{"acid", "acid", "acid", "acid"}
	if status, _ := phoneOverHTTP(t, srv, wrong); status != http.StatusForbidden {
		t.Fatalf("wrong phrase: %d", status)
	}
	call(t, srv, http.MethodDelete, "/api/pairing", true, nil, nil)
	if status, _ := phoneOverHTTP(t, srv, opened.Phrase); status != http.StatusGone {
		t.Fatalf("after cancelling: %d", status)
	}
}

func TestAShareThatIsNotBase64IsABadRequest(t *testing.T) {
	srv := pairingServer()
	call(t, srv, http.MethodPost, "/api/pairing", true, nil, nil)
	if status := call(t, srv, http.MethodPost, "/api/pair/exchange", false, map[string]string{"share": "%%%"}, nil); status != http.StatusBadRequest {
		t.Fatalf("status %d", status)
	}
}

func TestMetaSaysTheServerPairsByPhrase(t *testing.T) {
	var meta MobileMetaResponse
	call(t, pairingServer(), http.MethodGet, "/api/meta", false, nil, &meta)
	if meta.PhrasePairing != 1 {
		t.Fatalf("meta %+v", meta)
	}
}

func pairURIFor(t *testing.T, srv *Server, host string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/pairing", nil)
	req.Host = host
	req.Header.Set("Authorization", "Bearer the-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var body struct {
		Phrase  []string `json:"phrase"`
		PairURI string   `json:"pair_uri"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.PairURI == "" {
		return ""
	}
	return strings.Replace(body.PairURI, strings.Join(body.Phrase, "-"), "PHRASE", 1)
}

func TestTheQRCodeOfARequestNamesTheAddressTheBrowserUsed(t *testing.T) {
	srv := pairingServer()
	got := pairURIFor(t, srv, "192.168.1.5:3080")
	if want := "legato://pair?phrase=PHRASE&url=http%3A%2F%2F192.168.1.5%3A3080"; got != want {
		t.Fatalf("pair_uri = %q, want %q", got, want)
	}
}

func TestOnLocalhostTheQRCodeNamesTheMachinesNetworkAddress(t *testing.T) {
	srv := pairingServer()
	srv.SetPairingAddresses(func() []string { return []string{"http://192.168.1.200:3080"} })
	got := pairURIFor(t, srv, "localhost:3080")
	if want := "legato://pair?phrase=PHRASE&url=http%3A%2F%2F192.168.1.200%3A3080"; got != want {
		t.Fatalf("pair_uri = %q, want %q", got, want)
	}
}

func TestWithNoAddressAPhoneCouldUseThereIsNoQRCode(t *testing.T) {
	if got := pairURIFor(t, pairingServer(), "127.0.0.1:3080"); got != "" {
		t.Fatalf("pair_uri = %q", got)
	}
}
