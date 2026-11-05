package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/pairing"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

// pairedClients records who the secret callback was asked to pair.
var pairedClients []PairingClient

func newTestPairing(t *testing.T) (*PairingService, *fakeClock, *[]PairingStatus) {
	t.Helper()
	clock := &fakeClock{now: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)}
	var changes []PairingStatus
	pairedClients = nil
	svc := NewPairingService(func(_ context.Context, client PairingClient) (PairingSecret, error) {
		pairedClients = append(pairedClients, client)
		if client.Purpose == PairingMain {
			return PairingSecret{Token: "lgi_for-" + client.Name, Name: "vm-1", CACertificate: "-----BEGIN CERTIFICATE-----\n...", InstanceID: "srv-feedfacefeedface"}, nil
		}
		return PairingSecret{Token: "the-token", Name: "desktop", CACertificate: "-----BEGIN CERTIFICATE-----\n..."}, nil
	})
	svc.SetClock(clock.Now)
	svc.AllowMains()
	svc.OnChange(func(st PairingStatus) { changes = append(changes, st) })
	return svc, clock, &changes
}

// mainPairs runs a main instance's side of the exchange: the main's SPAKE2
// identity, and its description sealed under LabelMain.
func mainPairs(t *testing.T, svc *PairingService, words []string, description string) (PairingSecret, error) {
	t.Helper()
	main, share, err := pairing.Start(pairing.PartyA, pairing.MainProtocol, pairing.PhraseScalar(words), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	session, serverShare, err := svc.Exchange(share)
	if err != nil {
		return PairingSecret{}, err
	}
	keys, err := main.Finish(serverShare)
	if err != nil {
		t.Fatal(err)
	}
	sealedDescription, _ := pairing.Seal(keys.Key, pairing.LabelMain, []byte(description), rand.Reader)
	if description == "" {
		sealedDescription = nil
	}
	confirmB, sealed, err := svc.Confirm(context.Background(), session, keys.Confirmation, sealedDescription)
	if err != nil {
		return PairingSecret{}, err
	}
	if !keys.Confirms(confirmB) {
		t.Fatal("the worker's confirmation does not check")
	}
	plain, err := pairing.Open(keys.Key, pairing.LabelServer, sealed)
	if err != nil {
		t.Fatal(err)
	}
	var secret PairingSecret
	if err := json.Unmarshal(plain, &secret); err != nil {
		t.Fatal(err)
	}
	return secret, nil
}

const workstation = `{"name":"Workstation.local","instance_id":"srv-0123456789abcdef"}`

func TestARequestWithNoPurposePairsAPhone(t *testing.T) {
	svc, _, _ := newTestPairing(t)
	opened, _ := svc.Open()
	if opened.Purpose != PairingPhone {
		t.Fatalf("purpose %q", opened.Purpose)
	}
	secret, err := phonePairs(t, svc, opened.Phrase)
	if err != nil {
		t.Fatal(err)
	}
	if secret.Token != "the-token" || len(pairedClients) != 1 || pairedClients[0] != (PairingClient{Purpose: PairingPhone, Name: "Pixel 4 XL"}) {
		t.Fatalf("secret %+v, asked for %+v", secret, pairedClients)
	}
}

func TestAPhoneCannotCompleteAMainRequest(t *testing.T) {
	svc, _, _ := newTestPairing(t)
	opened, err := svc.OpenFor(PairingMain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := phonePairs(t, svc, opened.Phrase); !errors.Is(err, ErrPairingMismatch) {
		t.Fatalf("a phone with the phrase: %v", err)
	}
	if len(pairedClients) != 0 {
		t.Fatalf("the callback was asked for %+v", pairedClients)
	}
	if st := svc.Status(); st.State != PairingOpen || st.AttemptsLeft != 4 {
		t.Fatalf("status %+v", st)
	}
}

func TestAMainCannotCompleteAPhoneRequest(t *testing.T) {
	svc, _, _ := newTestPairing(t)
	opened, _ := svc.Open()
	if _, err := mainPairs(t, svc, opened.Phrase, workstation); !errors.Is(err, ErrPairingMismatch) {
		t.Fatalf("a main with the phrase: %v", err)
	}
	if len(pairedClients) != 0 {
		t.Fatalf("the callback was asked for %+v", pairedClients)
	}
}

func TestAMainGetsWhatTheCallbackMakesForItsNameAndInstance(t *testing.T) {
	svc, _, _ := newTestPairing(t)
	opened, _ := svc.OpenFor(PairingMain)
	if opened.Purpose != PairingMain {
		t.Fatalf("purpose %q", opened.Purpose)
	}
	secret, err := mainPairs(t, svc, opened.Phrase, workstation)
	if err != nil {
		t.Fatal(err)
	}
	want := PairingClient{Purpose: PairingMain, Name: "workstation-local", InstanceID: "srv-0123456789abcdef"}
	if len(pairedClients) != 1 || pairedClients[0] != want {
		t.Fatalf("asked for %+v, want %+v", pairedClients, want)
	}
	if secret.Token != "lgi_for-workstation-local" || secret.InstanceID != "srv-feedfacefeedface" || secret.Name != "vm-1" {
		t.Fatalf("secret %+v", secret)
	}
	if st := svc.Status(); st.State != PairingPaired || st.Purpose != PairingMain || st.Device != "workstation-local" {
		t.Fatalf("status %+v", st)
	}
}

func TestAMainMustNameItselfAndItsInstance(t *testing.T) {
	for name, description := range map[string]string{
		"nothing sealed":     "",
		"no name":            `{"instance_id":"srv-0123456789abcdef"}`,
		"no instance":        `{"name":"workstation"}`,
		"not an instance ID": `{"name":"workstation","instance_id":"workstation"}`,
		"nothing left":       `{"name":"..!!","instance_id":"srv-0123456789abcdef"}`,
		"longer than 40":     `{"name":"` + strings.Repeat("a", 41) + `","instance_id":"srv-0123456789abcdef"}`,
		"not JSON":           `workstation`,
	} {
		t.Run(name, func(t *testing.T) {
			svc, _, _ := newTestPairing(t)
			opened, _ := svc.OpenFor(PairingMain)
			if _, err := mainPairs(t, svc, opened.Phrase, description); !errors.Is(err, ErrPairingClient) {
				t.Fatalf("err %v", err)
			}
			if len(pairedClients) != 0 {
				t.Fatalf("the callback was asked for %+v", pairedClients)
			}
			if st := svc.Status(); st.State == PairingPaired {
				t.Fatalf("status %+v", st)
			}
		})
	}
}

func TestMainNamesAreNormalised(t *testing.T) {
	for in, want := range map[string]string{
		"workstation":       "workstation",
		"Erebus":            "erebus",
		"Cam's MacBook Pro": "cam-s-macbook-pro",
		"vm_1.example.net":  "vm-1-example-net",
		"--edge--":          "edge",
	} {
		if got, err := NormaliseMainName(in); err != nil || got != want {
			t.Errorf("NormaliseMainName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestAServerThatDoesNotPairMainsRefusesAMainRequest(t *testing.T) {
	svc := NewPairingService(func(context.Context, PairingClient) (PairingSecret, error) { return PairingSecret{}, nil })
	if _, err := svc.OpenFor(PairingMain); !errors.Is(err, ErrPairingPurpose) {
		t.Fatalf("err %v", err)
	}
	if _, err := svc.OpenFor("tablet"); !errors.Is(err, ErrPairingPurpose) {
		t.Fatalf("err %v", err)
	}
}

// phonePairs runs the phone's side of the exchange with words, as the app does.
func phonePairs(t *testing.T, svc *PairingService, words []string) (PairingSecret, error) {
	t.Helper()
	phone, share, err := pairing.Start(pairing.PartyA, pairing.Protocol, pairing.PhraseScalar(words), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	session, serverShare, err := svc.Exchange(share)
	if err != nil {
		return PairingSecret{}, err
	}
	keys, err := phone.Finish(serverShare)
	if err != nil {
		t.Fatal(err)
	}
	device, _ := pairing.Seal(keys.Key, pairing.LabelPhone, []byte(`{"device":"Pixel 4 XL"}`), rand.Reader)
	confirmB, sealed, err := svc.Confirm(context.Background(), session, keys.Confirmation, device)
	if err != nil {
		return PairingSecret{}, err
	}
	if !keys.Confirms(confirmB) {
		t.Fatal("the server's confirmation does not check")
	}
	plain, err := pairing.Open(keys.Key, pairing.LabelServer, sealed)
	if err != nil {
		t.Fatal(err)
	}
	var secret PairingSecret
	if err := json.Unmarshal(plain, &secret); err != nil {
		t.Fatal(err)
	}
	return secret, nil
}

func TestAPhoneWithThePhraseGetsTheToken(t *testing.T) {
	svc, _, changes := newTestPairing(t)
	opened, err := svc.Open()
	if err != nil {
		t.Fatal(err)
	}
	if opened.State != PairingOpen || len(opened.Phrase) != 4 || opened.AttemptsLeft != 5 {
		t.Fatalf("opened %+v", opened)
	}
	if !opened.ExpiresAt.Equal(time.Date(2026, 10, 27, 9, 5, 0, 0, time.UTC)) {
		t.Fatalf("expires %v", opened.ExpiresAt)
	}

	secret, err := phonePairs(t, svc, opened.Phrase)
	if err != nil {
		t.Fatal(err)
	}
	if secret.Token != "the-token" || secret.Name != "desktop" || secret.CACertificate == "" {
		t.Fatalf("secret %+v", secret)
	}
	st := svc.Status()
	if st.State != PairingPaired || st.Device != "Pixel 4 XL" || len(st.Phrase) != 0 {
		t.Fatalf("status %+v", st)
	}
	if last := (*changes)[len(*changes)-1]; last.State != PairingPaired {
		t.Fatalf("last change %+v", last)
	}
	if _, err := phonePairs(t, svc, opened.Phrase); !errors.Is(err, ErrPairingClosed) {
		t.Fatalf("a used phrase paired again: %v", err)
	}
}

func TestAWrongPhraseUsesAnAttempt(t *testing.T) {
	svc, _, _ := newTestPairing(t)
	opened, _ := svc.Open()
	wrong := append([]string{}, opened.Phrase...)
	wrong[3] = "zucchini"
	if wrong[3] == opened.Phrase[3] {
		wrong[3] = "aardvark"
	}
	if _, err := phonePairs(t, svc, wrong); !errors.Is(err, ErrPairingMismatch) {
		t.Fatalf("err %v", err)
	}
	if st := svc.Status(); st.State != PairingOpen || st.AttemptsLeft != 4 {
		t.Fatalf("status %+v", st)
	}
	if _, err := phonePairs(t, svc, opened.Phrase); err != nil {
		t.Fatalf("the right phrase failed after a wrong one: %v", err)
	}
}

func TestFiveAttemptsCloseTheRequest(t *testing.T) {
	svc, _, _ := newTestPairing(t)
	opened, _ := svc.Open()
	for range 5 {
		_, _ = phonePairs(t, svc, []string{"acid", "acid", "acid", "acid"})
	}
	if st := svc.Status(); st.State != PairingExhausted {
		t.Fatalf("status %+v", st)
	}
	if _, err := phonePairs(t, svc, opened.Phrase); !errors.Is(err, ErrPairingClosed) {
		t.Fatalf("err %v", err)
	}
}

func TestARequestExpiresAfterFiveMinutes(t *testing.T) {
	svc, clock, _ := newTestPairing(t)
	opened, _ := svc.Open()
	clock.now = clock.now.Add(5*time.Minute + time.Second)
	if st := svc.Status(); st.State != PairingExpired || len(st.Phrase) != 0 {
		t.Fatalf("status %+v", st)
	}
	if _, err := phonePairs(t, svc, opened.Phrase); !errors.Is(err, ErrPairingClosed) {
		t.Fatalf("err %v", err)
	}
}

func TestCancellingClosesTheRequest(t *testing.T) {
	svc, _, _ := newTestPairing(t)
	opened, _ := svc.Open()
	svc.Cancel()
	if st := svc.Status(); st.State != PairingCancelled {
		t.Fatalf("status %+v", st)
	}
	if _, err := phonePairs(t, svc, opened.Phrase); !errors.Is(err, ErrPairingClosed) {
		t.Fatalf("err %v", err)
	}
}

func TestOpeningAgainReplacesThePhrase(t *testing.T) {
	svc, _, _ := newTestPairing(t)
	first, _ := svc.Open()
	second, _ := svc.Open()
	if _, err := phonePairs(t, svc, first.Phrase); !errors.Is(err, ErrPairingMismatch) {
		t.Fatalf("the replaced phrase: %v", err)
	}
	if _, err := phonePairs(t, svc, second.Phrase); err != nil {
		t.Fatalf("the new phrase failed: %v", err)
	}
}

func TestNothingToPairWithBeforeARequestIsOpened(t *testing.T) {
	svc, _, _ := newTestPairing(t)
	if st := svc.Status(); st.State != PairingNone {
		t.Fatalf("status %+v", st)
	}
	if _, err := phonePairs(t, svc, []string{"acid", "acid", "acid", "acid"}); !errors.Is(err, ErrPairingClosed) {
		t.Fatalf("err %v", err)
	}
}
