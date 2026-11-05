package handofftest

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/pairing"
	"github.com/cpave3/legato/internal/service"
)

func TestAMainPairsWithAWorkerEndToEnd(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Name: "Workstation", Clock: worker.Clock})

	paired := Pair(t, main, worker)

	// The main lists the worker.
	workers, err := main.Workers.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := service.Worker{InstanceID: worker.InstanceID(), Address: worker.URL, Name: "vm-1", MainName: "workstation", Sandbox: true,
		HandoffVersion: service.HandoffProtocolVersion, PairedAt: "2026-11-05T09:00:00Z"}
	if len(workers) != 1 || workers[0] != want || paired != want {
		t.Fatalf("the main lists %+v (paired %+v), want %+v", workers, paired, want)
	}

	// The worker lists the main, by its instance ID, in Settings › Main instances.
	var listed struct {
		Mains []service.PairedMain `json:"mains"`
	}
	decode(t, worker.Do(http.MethodGet, "/api/settings/mains", worker.WebToken(), ""), http.StatusOK, &listed)
	if len(listed.Mains) != 1 || listed.Mains[0].Name != "workstation" || listed.Mains[0].InstanceID != main.InstanceID() || listed.Mains[0].OpenHandoffs != 0 {
		t.Fatalf("the worker lists %+v", listed.Mains)
	}

	// The main reaches the worker's handoff routes over its certificate.
	client, err := main.Workers.Client(ctx, paired.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckPairing(ctx); err != nil {
		t.Fatalf("the main's token on the worker: %v", err)
	}

	// Its token is a handoff token, and nothing more: the worker's board
	// refuses it.
	token := mainToken(t, main, paired.InstanceID)
	if !strings.HasPrefix(token, service.IntegrationTokenPrefix) || token == worker.WebToken() {
		t.Fatalf("the main holds %q", token)
	}
	if resp := worker.Do(http.MethodGet, "/api/board", token, ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("the main's token on the board: %d", resp.StatusCode)
	}

	// The token is sealed: the main's database never holds it in the clear.
	for _, name := range []string{"main.db", "main.db-wal"} {
		raw, err := os.ReadFile(filepath.Join(main.Dir, name))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(token)) {
			t.Fatalf("%s holds the worker's token in the clear", name)
		}
	}
}

func TestPairingTheSameMainAgainReplacesItsToken(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	first := Pair(t, main, worker)
	oldToken := mainToken(t, main, first.InstanceID)

	main.Name = "desk"
	Pair(t, main, worker)

	if resp := worker.Do(http.MethodGet, "/api/integrations/handoff/pairing", oldToken, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the replaced token: %d", resp.StatusCode)
	}
	client, _ := main.Workers.Client(ctx, first.InstanceID)
	if err := client.CheckPairing(ctx); err != nil {
		t.Fatalf("the new token: %v", err)
	}
	mains, _ := worker.Tokens.ListMains(ctx)
	if len(mains) != 1 || mains[0].Name != "desk" {
		t.Fatalf("the worker lists %+v", mains)
	}
	if workers, _ := main.Workers.List(ctx); len(workers) != 1 || workers[0].MainName != "desk" {
		t.Fatalf("the main lists %+v", workers)
	}
}

func TestAnotherMainCannotPairUnderAPairedMainsName(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{})
	Pair(t, NewMain(t, MainOptions{Name: "laptop"}), worker)

	other := NewMain(t, MainOptions{Name: "laptop"})
	phrase := worker.OpenMainPairing()
	_, err := other.Workers.Pair(context.Background(), service.WorkerPairRequest{Address: worker.URL, Phrase: strings.Join(phrase, "-"), Name: "laptop"})
	if !errors.Is(err, service.ErrWorkerRefused) || !strings.Contains(err.Error(), "laptop") {
		t.Fatalf("err %v", err)
	}
	if workers, _ := other.Workers.List(context.Background()); len(workers) != 0 {
		t.Fatalf("the refused main keeps %+v", workers)
	}
}

func TestAWrongPhrasePairsNothing(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{})
	main := NewMain(t, MainOptions{})
	phrase := worker.OpenMainPairing()
	wrong := "acid acid acid acid"
	if strings.Join(phrase, " ") == wrong {
		wrong = "able able able able"
	}
	_, err := main.Workers.Pair(ctx, service.WorkerPairRequest{Address: worker.URL, Phrase: wrong, Name: "workstation"})
	if !errors.Is(err, service.ErrWorkerRefused) || !strings.Contains(err.Error(), "phrase does not match") {
		t.Fatalf("err %v", err)
	}
	if mains, _ := worker.Tokens.ListMains(ctx); len(mains) != 0 {
		t.Fatalf("the worker lists %+v", mains)
	}
}

func TestAMainSaysToUpdateAnOldWorker(t *testing.T) {
	old := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/meta" {
			_, _ = w.Write([]byte(`{"version":"v0.9.0","phrase_pairing":1}`))
			return
		}
		t.Errorf("the main called %s on an old worker", r.URL.Path)
		http.NotFound(w, r)
	}))
	defer old.Close()
	main := NewMain(t, MainOptions{})
	_, err := main.Workers.Pair(context.Background(), service.WorkerPairRequest{Address: old.URL, Phrase: "acid acid acid acid", Name: "workstation"})
	if !errors.Is(err, service.ErrWorkerTooOld) || !strings.Contains(err.Error(), "update the worker") || !strings.Contains(err.Error(), "v0.9.0") {
		t.Fatalf("err %v", err)
	}
}

func TestAMainRefusesAWorkerThatSendsTheFullToken(t *testing.T) {
	// A worker that answers a main as it answers a phone.
	worker := StartWorker(t, WorkerOptions{Wire: func(w *Worker) {
		w.Pairing = service.NewPairingService(func(_ context.Context, _ service.PairingClient) (service.PairingSecret, error) {
			return service.PairingSecret{Token: w.WebToken(), Name: "vm-1", InstanceID: "srv-feedfacefeedface"}, nil
		})
		w.Pairing.AllowMains()
		w.Server.SetPairingService(w.Pairing)
	}})
	main := NewMain(t, MainOptions{})
	phrase := worker.OpenMainPairing()
	_, err := main.Workers.Pair(context.Background(), service.WorkerPairRequest{Address: worker.URL, Phrase: strings.Join(phrase, " "), Name: "workstation"})
	if !errors.Is(err, service.ErrWorkerRefused) || !strings.Contains(err.Error(), "not a handoff token") {
		t.Fatalf("err %v", err)
	}
	if workers, _ := main.Workers.List(context.Background()); len(workers) != 0 {
		t.Fatalf("the main kept %+v", workers)
	}
	if secrets, _ := main.Secrets.List(context.Background()); len(secrets) != 0 {
		t.Fatalf("the main kept a secret: %+v", secrets)
	}
}

func TestUnpairingAMainOnTheWorkerRevokesItsToken(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{})
	main := NewMain(t, MainOptions{})
	paired := Pair(t, main, worker)

	if resp := worker.Do(http.MethodDelete, "/api/settings/mains/"+main.InstanceID(), worker.WebToken(), ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("unpair: %d", resp.StatusCode)
	}
	client, _ := main.Workers.Client(ctx, paired.InstanceID)
	if err := client.CheckPairing(ctx); !errors.Is(err, service.ErrWorkerRefused) {
		t.Fatalf("the unpaired main: %v", err)
	}
}

func TestAPairingOutlivesAWorkerRestart(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{})
	main := NewMain(t, MainOptions{})
	paired := Pair(t, main, worker)
	client, _ := main.Workers.Client(ctx, paired.InstanceID)

	worker.Stop()
	if err := client.CheckPairing(ctx); !errors.Is(err, service.ErrWorkerUnreachable) {
		t.Fatalf("a stopped worker: %v", err)
	}
	worker.Restart()
	if err := client.CheckPairing(ctx); err != nil {
		t.Fatalf("after a restart: %v", err)
	}
	if worker.InstanceID() != paired.InstanceID {
		t.Fatalf("the worker came back as %s", worker.InstanceID())
	}
}

func TestAPhonePairingWithTheWorkerStillGetsTheWebToken(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{})
	var opened struct {
		Phrase  []string `json:"phrase"`
		Purpose string   `json:"purpose"`
	}
	decode(t, worker.Do(http.MethodPost, "/api/pairing", worker.WebToken(), ""), http.StatusOK, &opened)
	if opened.Purpose != "phone" {
		t.Fatalf("purpose %q", opened.Purpose)
	}
	secret := phonePairs(t, worker, opened.Phrase)
	if secret.Token != worker.WebToken() || secret.InstanceID != "" || secret.CACertificate == "" {
		t.Fatalf("the phone got %+v", secret)
	}
	if mains, _ := worker.Tokens.ListMains(context.Background()); len(mains) != 0 {
		t.Fatalf("a phone became a main: %+v", mains)
	}
}

// phonePairs pairs with the worker as the Legato app does.
func phonePairs(t *testing.T, worker *Worker, words []string) service.PairingSecret {
	t.Helper()
	b64 := base64.StdEncoding
	phone, share, err := pairing.Start(pairing.PartyA, pairing.Protocol, pairing.PhraseScalar(words), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var exchanged struct{ Session, Share string }
	body, _ := json.Marshal(map[string]string{"share": b64.EncodeToString(share)})
	decode(t, worker.Do(http.MethodPost, "/api/pair/exchange", "", string(body)), http.StatusOK, &exchanged)
	workerShare, _ := b64.DecodeString(exchanged.Share)
	keys, err := phone.Finish(workerShare)
	if err != nil {
		t.Fatal(err)
	}
	device, _ := pairing.Seal(keys.Key, pairing.LabelPhone, []byte(`{"device":"Pixel"}`), rand.Reader)
	body, _ = json.Marshal(map[string]string{"session": exchanged.Session, "confirmation": b64.EncodeToString(keys.Confirmation), "phone": b64.EncodeToString(device)})
	var confirmed struct{ Confirmation, Sealed string }
	decode(t, worker.Do(http.MethodPost, "/api/pair/confirm", "", string(body)), http.StatusOK, &confirmed)
	sealed, _ := b64.DecodeString(confirmed.Sealed)
	plain, err := pairing.Open(keys.Key, pairing.LabelServer, sealed)
	if err != nil {
		t.Fatal(err)
	}
	var secret service.PairingSecret
	if err := json.Unmarshal(plain, &secret); err != nil {
		t.Fatal(err)
	}
	return secret
}

// mainToken is the worker's token as the main keeps it, sealed.
func mainToken(t *testing.T, main *Main, workerID string) string {
	t.Helper()
	secret, err := main.Secrets.Get(context.Background(), credential.Ref{Scheme: "legato", Path: "handoff-worker-" + workerID})
	if err != nil {
		t.Fatal(err)
	}
	return string(secret.Value)
}

func decode(t *testing.T, resp *http.Response, status int, into any) {
	t.Helper()
	if resp.StatusCode != status {
		var body bytes.Buffer
		_, _ = body.ReadFrom(resp.Body)
		t.Fatalf("status %d, want %d: %s", resp.StatusCode, status, body.String())
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatal(err)
	}
}
