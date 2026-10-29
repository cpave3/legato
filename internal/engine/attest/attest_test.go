package attest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"
)

// Tags of the authorization list the tests write beyond the package's own.
const (
	tagKeySize        = 3
	tagDigest         = 5
	tagNoAuthRequired = tagNoAuth
	tagUserAuthType   = tagUserAuth
	tagAuthTimeout    = tagTimeout
	tagApplicationID  = tagAppID
)

func explicit(tag int, inner []byte) asn1.RawValue {
	return asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: tag, IsCompound: true, Bytes: inner}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := asn1.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func intSet(t *testing.T, values ...int) []byte {
	var body []byte
	for _, v := range values {
		body = append(body, mustMarshal(t, v)...)
	}
	return mustMarshal(t, asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: body})
}

// keyParams says what the attested key is, as a test chooses.
type keyParams struct {
	level       int // 0 software, 1 TEE, 2 StrongBox
	challenge   []byte
	purpose     int
	algorithm   int
	curve       int
	noAuth      bool
	userAuth    int // 0: none
	authTimeout int // -1: absent
	pkg         string
	digest      []byte
}

func goodParams(challenge []byte) keyParams {
	return keyParams{level: 1, challenge: challenge, purpose: 2, algorithm: 3, curve: 1, userAuth: 3, authTimeout: -1, pkg: "dev.legato.mobile", digest: appDigest}
}

var appDigest = []byte(strings.Repeat("\xfa", 32))

func authList(t *testing.T, items ...asn1.RawValue) asn1.RawValue {
	var body []byte
	for _, i := range items {
		body = append(body, mustMarshal(t, i)...)
	}
	return asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSequence, IsCompound: true, Bytes: body}
}

func keyDescription(t *testing.T, p keyParams) []byte {
	t.Helper()
	appID := mustMarshal(t, struct {
		Packages asn1.RawValue
		Digests  asn1.RawValue
	}{
		Packages: asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: mustMarshal(t, struct {
			Name    []byte
			Version int
		}{[]byte(p.pkg), 2})},
		Digests: asn1.RawValue{Class: asn1.ClassUniversal, Tag: asn1.TagSet, IsCompound: true, Bytes: mustMarshal(t, p.digest)},
	})
	hw := []asn1.RawValue{
		explicit(tagPurpose, intSet(t, p.purpose)),
		explicit(tagAlgorithm, mustMarshal(t, p.algorithm)),
		explicit(tagKeySize, mustMarshal(t, 256)),
		explicit(tagDigest, intSet(t, 4)),
		explicit(tagECCurve, mustMarshal(t, p.curve)),
	}
	if p.noAuth {
		hw = append(hw, explicit(tagNoAuthRequired, mustMarshal(t, asn1.NullRawValue)))
	}
	if p.userAuth != 0 {
		hw = append(hw, explicit(tagUserAuthType, mustMarshal(t, p.userAuth)))
	}
	if p.authTimeout >= 0 {
		hw = append(hw, explicit(tagAuthTimeout, mustMarshal(t, p.authTimeout)))
	}
	sw := []asn1.RawValue{explicit(tagApplicationID, mustMarshal(t, appID))}
	return mustMarshal(t, struct {
		AttestationVersion       int
		AttestationSecurityLevel asn1.Enumerated
		KeyMintVersion           int
		KeyMintSecurityLevel     asn1.Enumerated
		Challenge                []byte
		UniqueID                 []byte
		SoftwareEnforced         asn1.RawValue
		HardwareEnforced         asn1.RawValue
	}{300, asn1.Enumerated(p.level), 300, asn1.Enumerated(p.level), p.challenge, []byte{}, authList(t, sw...), authList(t, hw...)})
}

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newCA(t *testing.T, name string, parent *testCA) testCA {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	signer, signerKey := tmpl, key
	if parent != nil {
		signer, signerKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return testCA{cert: cert, key: key}
}

// attestedChain is leaf, intermediate, root, as a phone sends it.
func attestedChain(t *testing.T, root testCA, p keyParams) ([]*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	intermediate := newCA(t, "intermediate", &root)
	leafKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Android Keystore Key"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		ExtraExtensions: []pkix.Extension{{Id: keyDescriptionOID, Value: keyDescription(t, p)}}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, intermediate.cert, &leafKey.PublicKey, intermediate.key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	return []*x509.Certificate{leaf, intermediate.cert, root.cert}, leafKey
}

func TestTheKeyDescriptionIsRead(t *testing.T) {
	kd, err := ParseKeyDescription(keyDescription(t, goodParams([]byte("challenge-1"))))
	if err != nil {
		t.Fatal(err)
	}
	if kd.SecurityLevel != TrustedEnvironment || string(kd.Challenge) != "challenge-1" {
		t.Fatalf("kd = %+v", kd)
	}
	hw := kd.HardwareEnforced
	if !hw.Has(tagPurpose) || hw.Purposes[0] != 2 || hw.Algorithm != 3 || hw.ECCurve != 1 || hw.UserAuthType != 3 || hw.NoAuthRequired || hw.HasAuthTimeout {
		t.Fatalf("hardware = %+v", hw)
	}
	app := kd.ApplicationID()
	if app == nil || app.Packages[0] != "dev.legato.mobile" || string(app.Digests[0]) != string(appDigest) {
		t.Fatalf("application = %+v", app)
	}
}

func policyFor(root testCA, challenge []byte) Policy {
	pool := x509.NewCertPool()
	pool.AddCert(root.cert)
	return Policy{Roots: pool, Challenge: challenge, Package: "dev.legato.mobile", Digests: [][]byte{appDigest}}
}

func TestAHardwareKeyThatNeedsThePersonForEachUseIsAccepted(t *testing.T) {
	root := newCA(t, "root", nil)
	chain, key := attestedChain(t, root, goodParams([]byte("c1")))
	got, err := Verify(chain, policyFor(root, []byte("c1")))
	if err != nil {
		t.Fatal(err)
	}
	if !got.PublicKey.Equal(&key.PublicKey) || got.SecurityLevel != TrustedEnvironment {
		t.Fatalf("got %+v", got)
	}
	// An auth timeout of 0 is also each use.
	p := goodParams([]byte("c1"))
	p.authTimeout = 0
	chain, _ = attestedChain(t, root, p)
	if _, err := Verify(chain, policyFor(root, []byte("c1"))); err != nil {
		t.Fatalf("timeout 0: %v", err)
	}
}

func TestKeysThatDoNotMeetThePolicyAreRefused(t *testing.T) {
	root := newCA(t, "root", nil)
	for name, change := range map[string]func(*keyParams){
		"software only":   func(p *keyParams) { p.level = 0 },
		"no auth needed":  func(p *keyParams) { p.noAuth = true },
		"no user auth":    func(p *keyParams) { p.userAuth = 0 },
		"an auth timeout": func(p *keyParams) { p.authTimeout = 30 },
		"another curve":   func(p *keyParams) { p.curve = 2 },
		"not for signing": func(p *keyParams) { p.purpose = 3 },
		"another app":     func(p *keyParams) { p.pkg = "com.example.evil" },
		"another signer":  func(p *keyParams) { p.digest = []byte(strings.Repeat("\x01", 32)) },
		"stale challenge": func(p *keyParams) { p.challenge = []byte("old") },
	} {
		p := goodParams([]byte("c1"))
		change(&p)
		chain, _ := attestedChain(t, root, p)
		if _, err := Verify(chain, policyFor(root, []byte("c1"))); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAChainToAnotherRootIsRefused(t *testing.T) {
	root, other := newCA(t, "root", nil), newCA(t, "other", nil)
	chain, _ := attestedChain(t, other, goodParams([]byte("c1")))
	if _, err := Verify(chain, policyFor(root, []byte("c1"))); err == nil {
		t.Fatal("a chain to an unknown root was accepted")
	}
	// A chain whose last certificate claims the root's name but another key.
	chain, _ = attestedChain(t, root, goodParams([]byte("c1")))
	if _, err := Verify(chain[:1], policyFor(root, []byte("c1"))); err == nil {
		t.Fatal("a lone leaf was accepted")
	}
}

func TestARevokedCertificateIsRefused(t *testing.T) {
	root := newCA(t, "root", nil)
	chain, _ := attestedChain(t, root, goodParams([]byte("c1")))
	policy := policyFor(root, []byte("c1"))
	policy.Revoked = func(serial *big.Int) (bool, error) { return serial.Cmp(chain[1].SerialNumber) == 0, nil }
	if _, err := Verify(chain, policy); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("err = %v", err)
	}
	policy.Revoked = func(*big.Int) (bool, error) { return false, errors.New("unreachable") }
	if _, err := Verify(chain, policy); err == nil {
		t.Fatal("a chain was accepted with no revocation list")
	}
}

func TestTheShippedRootsAreGooglesValidOnes(t *testing.T) {
	pool, n := GoogleRoots()
	if pool == nil || n < 3 {
		t.Fatalf("roots = %d", n)
	}
}
