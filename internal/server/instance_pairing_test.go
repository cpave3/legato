package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/pairing"
	"github.com/cpave3/legato/internal/service"
)

// mainOverHTTP pairs as a main instance does: its own SPAKE2 identity, and
// its name and instance ID sealed under LabelMain.
func mainOverHTTP(t *testing.T, srv *Server, words []string, description string) (int, string, service.PairingSecret) {
	t.Helper()
	b64 := base64.StdEncoding
	main, share, err := pairing.Start(pairing.PartyA, pairing.MainProtocol, pairing.PhraseScalar(words), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var exchanged struct{ Session, Share, Error string }
	if status := call(t, srv, http.MethodPost, "/api/pair/exchange", false, map[string]string{"share": b64.EncodeToString(share)}, &exchanged); status != http.StatusOK {
		return status, exchanged.Error, service.PairingSecret{}
	}
	serverShare, _ := b64.DecodeString(exchanged.Share)
	keys, err := main.Finish(serverShare)
	if err != nil {
		t.Fatal(err)
	}
	sealedDescription, _ := pairing.Seal(keys.Key, pairing.LabelMain, []byte(description), rand.Reader)
	var confirmed struct{ Confirmation, Sealed, Error string }
	status := call(t, srv, http.MethodPost, "/api/pair/confirm", false, map[string]string{
		"session":      exchanged.Session,
		"confirmation": b64.EncodeToString(keys.Confirmation),
		"phone":        b64.EncodeToString(sealedDescription),
	}, &confirmed)
	if status != http.StatusOK {
		return status, confirmed.Error, service.PairingSecret{}
	}
	sealed, _ := b64.DecodeString(confirmed.Sealed)
	plain, err := pairing.Open(keys.Key, pairing.LabelServer, sealed)
	if err != nil {
		t.Fatal(err)
	}
	var secret service.PairingSecret
	_ = json.Unmarshal(plain, &secret)
	return status, "", secret
}

// openOn opens a request as the browser at host does, with body as its JSON.
func openOn(t *testing.T, srv *Server, host, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/pairing", strings.NewReader(body))
	req.Host = host
	req.Header.Set("Authorization", "Bearer the-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var view map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	return view
}

func TestAMainRequestShowsItsPurposeAndAddressButNoQRCode(t *testing.T) {
	srv := pairingServer()
	view := openOn(t, srv, "vm-1.lan:3080", `{"purpose":"main"}`)
	if view["purpose"] != "main" || view["pair_uri"] != nil || view["address"] != "http://vm-1.lan:3080" {
		t.Fatalf("view %v", view)
	}
	var now map[string]any
	call(t, srv, http.MethodGet, "/api/pairing", true, nil, &now)
	if now["purpose"] != "main" || now["pair_uri"] != nil {
		t.Fatalf("read again %v", now)
	}
}

func TestARequestWithNoBodyIsStillAPhones(t *testing.T) {
	srv := pairingServer()
	view := openOn(t, srv, "vm-1.lan:3080", "")
	if view["purpose"] != "phone" || view["pair_uri"] == nil {
		t.Fatalf("view %v", view)
	}
	words := []string{}
	for _, w := range view["phrase"].([]any) {
		words = append(words, w.(string))
	}
	status, secret := phoneOverHTTP(t, srv, words)
	if status != http.StatusOK || secret.Token != "the-token" || secret.InstanceID != "" {
		t.Fatalf("a phone: %d %+v", status, secret)
	}
}

func TestARequestForSomethingElseIsABadRequest(t *testing.T) {
	srv := pairingServer()
	if status := call(t, srv, http.MethodPost, "/api/pairing", true, map[string]string{"purpose": "tablet"}, nil); status != http.StatusBadRequest {
		t.Fatalf("status %d", status)
	}
}

func TestAMainPairsOverHTTPAndAPhoneCannotTakeItsRequest(t *testing.T) {
	srv := pairingServer()
	var opened service.PairingStatus
	call(t, srv, http.MethodPost, "/api/pairing", true, map[string]string{"purpose": "main"}, &opened)
	if status, _ := phoneOverHTTP(t, srv, opened.Phrase); status != http.StatusForbidden {
		t.Fatalf("a phone on a main's request: %d", status)
	}
	status, _, secret := mainOverHTTP(t, srv, opened.Phrase, `{"name":"Workstation","instance_id":"srv-0123456789abcdef"}`)
	if status != http.StatusOK || secret.Token != "lgi_main-workstation" || secret.InstanceID != "srv-feedfacefeedface" {
		t.Fatalf("a main: %d %+v", status, secret)
	}
}

func TestAMainThatDoesNotNameItselfIsTold(t *testing.T) {
	srv := pairingServer()
	var opened service.PairingStatus
	call(t, srv, http.MethodPost, "/api/pairing", true, map[string]string{"purpose": "main"}, &opened)
	status, message, _ := mainOverHTTP(t, srv, opened.Phrase, `{"instance_id":"srv-0123456789abcdef"}`)
	if status != http.StatusBadRequest || !strings.Contains(message, "name") {
		t.Fatalf("%d %q", status, message)
	}
}

func TestMetaAdvertisesHandoffPairingAndTheSandbox(t *testing.T) {
	srv := pairingServer()
	srv.SetSandboxAvailable(func() bool { return true })
	var meta map[string]any
	call(t, srv, http.MethodGet, "/api/meta", false, nil, &meta)
	if meta["handoff_pairing"] != float64(1) || meta["handoff"] != float64(service.HandoffProtocolVersion) || meta["sandbox"] != true {
		t.Fatalf("meta %v", meta)
	}

	phonesOnly := New(nil, nil, nil, ":0")
	phonesOnly.SetPairingService(service.NewPairingService(func(context.Context, service.PairingClient) (service.PairingSecret, error) {
		return service.PairingSecret{}, nil
	}))
	meta = nil
	call(t, phonesOnly, http.MethodGet, "/api/meta", false, nil, &meta)
	if meta["handoff_pairing"] != nil || meta["handoff"] != nil || meta["sandbox"] != nil {
		t.Fatalf("a server that pairs phones only: %v", meta)
	}
}

type fakeMains struct {
	mains   []service.PairedMain
	revoked []string
}

func (f *fakeMains) ListMains(context.Context) ([]service.PairedMain, error) { return f.mains, nil }
func (f *fakeMains) RevokeMain(_ context.Context, id string) error {
	for i, m := range f.mains {
		if m.InstanceID == id {
			f.revoked = append(f.revoked, id)
			f.mains = append(f.mains[:i], f.mains[i+1:]...)
			return nil
		}
	}
	return service.ErrMainNotPaired
}

func TestSettingsListAndUnpairMainInstances(t *testing.T) {
	srv := pairingServer()
	mains := &fakeMains{mains: []service.PairedMain{{Name: "workstation", InstanceID: "srv-0123456789abcdef", PairedAt: "2026-11-03T09:00:00Z"}}}
	srv.SetMainInstances(mains)
	var listed struct {
		Mains []service.PairedMain `json:"mains"`
	}
	if status := call(t, srv, http.MethodGet, "/api/settings/mains", true, nil, &listed); status != http.StatusOK || len(listed.Mains) != 1 || listed.Mains[0].Name != "workstation" {
		t.Fatalf("%d %+v", status, listed)
	}
	if status := call(t, srv, http.MethodDelete, "/api/settings/mains/srv-0123456789abcdef", true, nil, nil); status != http.StatusOK || len(mains.revoked) != 1 {
		t.Fatalf("unpair: %d %v", status, mains.revoked)
	}
	if status := call(t, srv, http.MethodDelete, "/api/settings/mains/srv-0123456789abcdef", true, nil, nil); status != http.StatusNotFound {
		t.Fatalf("unpair again: %d", status)
	}
}

func TestHandoffRoutesActOnlyForAPairedMain(t *testing.T) {
	srv := New(&mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}}, nil, nil, ":0")
	srv.SetAuthToken("full-token")
	srv.SetInstanceID(func(context.Context) (string, error) { return "srv-feedfacefeedface", nil })
	srv.SetIntegrationAuth(fakeIntegrationAuth{
		"lgi_main": {Kind: service.IntegrationKindMain, Name: "workstation", InstanceID: "srv-0123456789abcdef", Scopes: []string{service.ScopeHandoff}},
	})
	h := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/integrations/handoff/pairing", nil)
	req.Header.Set("Authorization", "Bearer lgi_main")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var paired map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &paired)
	if rec.Code != http.StatusOK || paired["main_instance_id"] != "srv-0123456789abcdef" || paired["main_name"] != "workstation" || paired["worker_instance_id"] != "srv-feedfacefeedface" {
		t.Fatalf("%d %v", rec.Code, paired)
	}

	// The full web token is not a main.
	if status, body := integrationRequest(t, h, http.MethodGet, "/api/integrations/handoff/pairing", "full-token"); status != http.StatusForbidden || body["error"] != "not_a_main" {
		t.Fatalf("full token: %d %v", status, body)
	}
}

type fakeWorkers struct {
	workers []service.Worker
	err     error
	asked   []service.WorkerPairRequest
}

func (f *fakeWorkers) List(context.Context) ([]service.Worker, error) { return f.workers, nil }
func (f *fakeWorkers) Pair(_ context.Context, req service.WorkerPairRequest) (service.Worker, error) {
	f.asked = append(f.asked, req)
	if f.err != nil {
		return service.Worker{}, f.err
	}
	w := service.Worker{InstanceID: "srv-feedfacefeedface", Address: req.Address, Name: "vm-1", MainName: req.Name}
	f.workers = append(f.workers, w)
	return w, nil
}
func (f *fakeWorkers) Defaults(context.Context) (service.WorkerDefaults, error) {
	return service.WorkerDefaults{InstanceID: "srv-0123456789abcdef", MainName: "workstation"}, nil
}

func TestSettingsAddAWorker(t *testing.T) {
	srv := pairingServer()
	workers := &fakeWorkers{}
	srv.SetWorkers(workers)
	var paired service.Worker
	status := call(t, srv, http.MethodPost, "/api/settings/workers", true, map[string]string{"address": "vm-1.lan:3080", "phrase": "a b c d", "name": "workstation"}, &paired)
	if status != http.StatusOK || paired.Name != "vm-1" || len(workers.asked) != 1 || workers.asked[0].Phrase != "a b c d" {
		t.Fatalf("%d %+v %+v", status, paired, workers.asked)
	}
	var listed struct {
		Workers    []service.Worker `json:"workers"`
		InstanceID string           `json:"instance_id"`
		MainName   string           `json:"main_name"`
	}
	call(t, srv, http.MethodGet, "/api/settings/workers", true, nil, &listed)
	if len(listed.Workers) != 1 || listed.InstanceID != "srv-0123456789abcdef" || listed.MainName != "workstation" {
		t.Fatalf("listed %+v", listed)
	}

	workers.err = errors.Join(service.ErrWorkerTooOld, errors.New("vm-1.lan:3080 runs Legato v0.9.0"))
	var failed struct{ Error string }
	status = call(t, srv, http.MethodPost, "/api/settings/workers", true, map[string]string{"address": "vm-1.lan:3080", "phrase": "a b c d", "name": "workstation"}, &failed)
	if status != http.StatusConflict || !strings.Contains(failed.Error, "update the worker") {
		t.Fatalf("an old worker: %d %q", status, failed.Error)
	}
}
