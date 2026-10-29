package attest

import (
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestTheRevocationListIsReadByHexSerialAndKeptADay(t *testing.T) {
	var calls atomic.Int32
	fail := atomic.Bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		_, _ = w.Write([]byte(`{"entries":{"c35747a084470c3135aeefe2b8d40cd6":{"status":"REVOKED","reason":"KEY_COMPROMISE"},"1a2b":{"status":"SUSPENDED"}}}`))
	}))
	defer srv.Close()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	list := &RevocationList{URL: srv.URL, Client: srv.Client(), Now: func() time.Time { return now }}

	revokedSerial, _ := new(big.Int).SetString("c35747a084470c3135aeefe2b8d40cd6", 16)
	if got, err := list.Revoked(revokedSerial); err != nil || !got {
		t.Fatalf("revoked serial: %v, %v", got, err)
	}
	if got, _ := list.Revoked(big.NewInt(0x1a2b)); !got {
		t.Fatal("a suspended serial was accepted")
	}
	if got, _ := list.Revoked(big.NewInt(42)); got {
		t.Fatal("a serial not on the list was refused")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d; the list was not kept", calls.Load())
	}

	// A day later the list is read again; when that fails, the last list
	// still answers.
	fail.Store(true)
	now = now.Add(25 * time.Hour)
	if got, err := list.Revoked(revokedSerial); err != nil || !got {
		t.Fatalf("after a failed refresh: %v, %v", got, err)
	}
}

func TestNoRevocationListMeansNoAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	list := &RevocationList{URL: srv.URL, Client: srv.Client()}
	if _, err := list.Revoked(big.NewInt(42)); err == nil {
		t.Fatal("a chain could be accepted with no revocation list ever read")
	}
}
