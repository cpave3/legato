package attest

import (
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"os"
	"strings"
	"testing"
)

// testdata/pixel-9-pro-strongbox.pem is the chain a Pixel 9 Pro sent when
// the Legato app enrolled it on 2026-10-29: a StrongBox key, through
// Google's remotely provisioned intermediates, to Key Attestation CA1. The
// app was signed with the release key whose digest is releaseDigest.
const releaseDigest = "b52c219d9210f1524b7edc296d562eb5da8b035e343a887c6a2ca28dd3ae49db"

func realChain(t *testing.T) []*x509.Certificate {
	t.Helper()
	raw, err := os.ReadFile("testdata/pixel-9-pro-strongbox.pem")
	if err != nil {
		t.Fatal(err)
	}
	var chain []*x509.Certificate
	for block, rest := pem.Decode(raw); block != nil; block, rest = pem.Decode(rest) {
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		chain = append(chain, cert)
	}
	return chain
}

func realPolicy(t *testing.T, chain []*x509.Certificate) Policy {
	t.Helper()
	roots, n := GoogleRoots()
	if n == 0 {
		t.Fatal("no Google roots")
	}
	var kd KeyDescription
	for _, e := range chain[0].Extensions {
		if e.Id.Equal(keyDescriptionOID) {
			var err error
			if kd, err = ParseKeyDescription(e.Value); err != nil {
				t.Fatal(err)
			}
		}
	}
	digest, _ := hex.DecodeString(releaseDigest)
	return Policy{Roots: roots, Challenge: kd.Challenge, Package: "dev.legato.mobile", Digests: [][]byte{digest},
		Revoked: func(*big.Int) (bool, error) { return false, nil }}
}

func TestARealPhoneChainIsAccepted(t *testing.T) {
	chain := realChain(t)
	if len(chain) != 5 {
		t.Fatalf("chain has %d certificates", len(chain))
	}
	got, err := Verify(chain, realPolicy(t, chain))
	if err != nil {
		t.Fatal(err)
	}
	if got.SecurityLevel != StrongBox {
		t.Fatalf("security level = %v", got.SecurityLevel)
	}
}

func TestARealPhoneChainIsRefusedForAnotherAppOrChallenge(t *testing.T) {
	chain := realChain(t)
	debugKey := realPolicy(t, chain)
	debugKey.Digests = [][]byte{make([]byte, 32)}
	if _, err := Verify(chain, debugKey); err == nil {
		t.Fatal("accepted for an app signed with another key")
	}
	stale := realPolicy(t, chain)
	stale.Challenge = []byte("another challenge")
	if _, err := Verify(chain, stale); err == nil || !strings.Contains(err.Error(), "enrollment") {
		t.Fatalf("another challenge: %v", err)
	}
	revoked := realPolicy(t, chain)
	revoked.Revoked = func(serial *big.Int) (bool, error) { return serial.Cmp(chain[1].SerialNumber) == 0, nil }
	if _, err := Verify(chain, revoked); err == nil {
		t.Fatal("accepted with a revoked intermediate")
	}
}
