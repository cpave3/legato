package service

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/pairing"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func newTestPairing(t *testing.T) (*PairingService, *fakeClock, *[]PairingStatus) {
	t.Helper()
	clock := &fakeClock{now: time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)}
	var changes []PairingStatus
	svc := NewPairingService(func() (PairingSecret, error) {
		return PairingSecret{Token: "the-token", Name: "desktop", CACertificate: "-----BEGIN CERTIFICATE-----\n..."}, nil
	})
	svc.now = clock.Now
	svc.OnChange(func(st PairingStatus) { changes = append(changes, st) })
	return svc, clock, &changes
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
	confirmB, sealed, err := svc.Confirm(session, keys.Confirmation, device)
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
