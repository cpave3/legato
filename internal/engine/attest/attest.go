// Package attest checks Android key attestation: that a public key lives in
// a phone's secure hardware, is used only to sign, and can be used only
// after the person proves who they are, each time. Legato accepts an
// approval device only on such a key.
package attest

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"embed"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
)

// keyDescriptionOID is the extension of the leaf certificate that
// describes the attested key.
var keyDescriptionOID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 11129, 2, 1, 17}

// SecurityLevel is where a key lives.
type SecurityLevel int

const (
	Software           SecurityLevel = 0
	TrustedEnvironment SecurityLevel = 1
	StrongBox          SecurityLevel = 2
)

func (l SecurityLevel) String() string {
	switch l {
	case TrustedEnvironment:
		return "tee"
	case StrongBox:
		return "strongbox"
	}
	return "software"
}

// The values of KeyMint's tags this package checks.
const (
	purposeSign  = 2
	algorithmEC  = 3
	curveP256    = 1
	tagPurpose   = 1
	tagAlgorithm = 2
	tagECCurve   = 10
	tagNoAuth    = 503
	tagUserAuth  = 504
	tagTimeout   = 505
	tagAppID     = 709
)

// AuthorizationList is the part of a key's authorization list this package
// reads.
type AuthorizationList struct {
	tags           map[int]bool
	Purposes       []int
	Algorithm      int
	ECCurve        int
	NoAuthRequired bool
	UserAuthType   int
	HasAuthTimeout bool
	AuthTimeout    int
	applicationID  []byte
}

// Has reports whether the list carries a tag.
func (a AuthorizationList) Has(tag int) bool { return a.tags[tag] }

// KeyDescription is the attestation extension of a key's certificate.
type KeyDescription struct {
	SecurityLevel    SecurityLevel
	Challenge        []byte
	SoftwareEnforced AuthorizationList
	HardwareEnforced AuthorizationList
}

// ApplicationID is the app the key was made for.
type ApplicationID struct {
	Packages []string
	Digests  [][]byte
}

// ApplicationID reads the app's identity, from whichever list holds it.
func (k KeyDescription) ApplicationID() *ApplicationID {
	raw := k.HardwareEnforced.applicationID
	if raw == nil {
		raw = k.SoftwareEnforced.applicationID
	}
	if raw == nil {
		return nil
	}
	var id struct {
		Packages asn1.RawValue
		Digests  asn1.RawValue
	}
	if _, err := asn1.Unmarshal(raw, &id); err != nil {
		return nil
	}
	out := &ApplicationID{}
	for rest := id.Packages.Bytes; len(rest) > 0; {
		var pkg struct {
			Name    []byte
			Version int64
		}
		var err error
		if rest, err = asn1.Unmarshal(rest, &pkg); err != nil {
			return nil
		}
		out.Packages = append(out.Packages, string(pkg.Name))
	}
	for rest := id.Digests.Bytes; len(rest) > 0; {
		var digest []byte
		var err error
		if rest, err = asn1.Unmarshal(rest, &digest); err != nil {
			return nil
		}
		out.Digests = append(out.Digests, digest)
	}
	return out
}

// ParseKeyDescription reads the attestation extension's value.
func ParseKeyDescription(der []byte) (KeyDescription, error) {
	var raw struct {
		AttestationVersion       int64
		AttestationSecurityLevel asn1.Enumerated
		KeyMintVersion           int64
		KeyMintSecurityLevel     asn1.Enumerated
		Challenge                []byte
		UniqueID                 []byte
		SoftwareEnforced         asn1.RawValue
		HardwareEnforced         asn1.RawValue
	}
	if _, err := asn1.Unmarshal(der, &raw); err != nil {
		return KeyDescription{}, fmt.Errorf("the key description does not read: %w", err)
	}
	sw, err := parseAuthorizationList(raw.SoftwareEnforced.Bytes)
	if err != nil {
		return KeyDescription{}, err
	}
	hw, err := parseAuthorizationList(raw.HardwareEnforced.Bytes)
	if err != nil {
		return KeyDescription{}, err
	}
	return KeyDescription{SecurityLevel: SecurityLevel(raw.AttestationSecurityLevel), Challenge: raw.Challenge, SoftwareEnforced: sw, HardwareEnforced: hw}, nil
}

func parseAuthorizationList(body []byte) (AuthorizationList, error) {
	out := AuthorizationList{tags: map[int]bool{}}
	for rest := body; len(rest) > 0; {
		var field asn1.RawValue
		var err error
		if rest, err = asn1.Unmarshal(rest, &field); err != nil {
			return out, fmt.Errorf("the authorization list does not read: %w", err)
		}
		if field.Class != asn1.ClassContextSpecific {
			continue
		}
		out.tags[field.Tag] = true
		inner := field.Bytes
		var bad error
		switch field.Tag {
		case tagPurpose:
			var set asn1.RawValue
			if _, bad = asn1.Unmarshal(inner, &set); bad == nil {
				for r := set.Bytes; len(r) > 0 && bad == nil; {
					var v int
					r, bad = asn1.Unmarshal(r, &v)
					out.Purposes = append(out.Purposes, v)
				}
			}
		case tagAlgorithm:
			_, bad = asn1.Unmarshal(inner, &out.Algorithm)
		case tagECCurve:
			_, bad = asn1.Unmarshal(inner, &out.ECCurve)
		case tagNoAuth:
			out.NoAuthRequired = true
		case tagUserAuth:
			_, bad = asn1.Unmarshal(inner, &out.UserAuthType)
		case tagTimeout:
			out.HasAuthTimeout = true
			_, bad = asn1.Unmarshal(inner, &out.AuthTimeout)
		case tagAppID:
			var octets []byte
			if _, bad = asn1.Unmarshal(inner, &octets); bad == nil {
				out.applicationID = octets
			}
		}
		if bad != nil {
			return out, fmt.Errorf("authorization tag %d does not read: %w", field.Tag, bad)
		}
	}
	return out, nil
}

// Policy is what an approval device's key must be.
type Policy struct {
	Roots *x509.CertPool
	// Challenge is the server's enrollment challenge, put in the key.
	Challenge []byte
	// Package and Digests name the app: its package and the digests of the
	// certificates it may be signed with.
	Package string
	Digests [][]byte
	// Revoked says whether a certificate serial is revoked. An error (no
	// list could be read) refuses the chain.
	Revoked func(serial *big.Int) (bool, error)
}

// Attested is an accepted key.
type Attested struct {
	PublicKey     *ecdsa.PublicKey
	SecurityLevel SecurityLevel
}

// Verify checks an attestation chain, leaf first, against the policy: it
// leads to one of the roots, none of it is revoked, and the leaf's key is
// a P-256 signing key in secure hardware, made with the challenge for the
// app, that needs the person's biometric or PIN for each use.
func Verify(chain []*x509.Certificate, p Policy) (Attested, error) {
	if len(chain) < 2 {
		return Attested{}, errors.New("an attestation chain has at least a key and a root")
	}
	for i := 0; i+1 < len(chain); i++ {
		if err := chain[i].CheckSignatureFrom(chain[i+1]); err != nil {
			return Attested{}, fmt.Errorf("certificate %d is not signed by the next: %w", i, err)
		}
	}
	last := chain[len(chain)-1]
	if p.Roots == nil {
		return Attested{}, errors.New("no attestation roots")
	}
	if _, err := last.Verify(x509.VerifyOptions{Roots: p.Roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		return Attested{}, fmt.Errorf("the chain does not lead to a known attestation root: %w", err)
	}
	if p.Revoked != nil {
		for i, cert := range chain {
			revoked, err := p.Revoked(cert.SerialNumber)
			if err != nil {
				return Attested{}, fmt.Errorf("the revocation list cannot be read: %w", err)
			}
			if revoked {
				return Attested{}, fmt.Errorf("certificate %d of the chain is revoked", i)
			}
		}
	}
	leaf := chain[0]
	var ext []byte
	for _, e := range leaf.Extensions {
		if e.Id.Equal(keyDescriptionOID) {
			ext = e.Value
		}
	}
	if ext == nil {
		return Attested{}, errors.New("the key's certificate has no attestation")
	}
	kd, err := ParseKeyDescription(ext)
	if err != nil {
		return Attested{}, err
	}
	if err := checkKey(kd, p); err != nil {
		return Attested{}, err
	}
	key, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return Attested{}, errors.New("the attested key is not a P-256 key")
	}
	return Attested{PublicKey: key, SecurityLevel: kd.SecurityLevel}, nil
}

func checkKey(kd KeyDescription, p Policy) error {
	if kd.SecurityLevel != TrustedEnvironment && kd.SecurityLevel != StrongBox {
		return errors.New("the key is not in secure hardware")
	}
	if len(p.Challenge) == 0 || !bytes.Equal(kd.Challenge, p.Challenge) {
		return errors.New("the key was not made for this enrollment")
	}
	hw := kd.HardwareEnforced
	sign := false
	for _, purpose := range hw.Purposes {
		sign = sign || purpose == purposeSign
	}
	switch {
	case !sign:
		return errors.New("the key is not a signing key")
	case hw.Algorithm != algorithmEC || hw.ECCurve != curveP256:
		return errors.New("the key is not an EC P-256 key")
	case hw.NoAuthRequired || hw.UserAuthType == 0:
		return errors.New("the key can be used without the person")
	case hw.HasAuthTimeout && hw.AuthTimeout != 0:
		return errors.New("the key stays unlocked after the person proves who they are; it must ask each time")
	}
	app := kd.ApplicationID()
	if app == nil {
		return errors.New("the key names no app")
	}
	okPackage := false
	for _, pkg := range app.Packages {
		okPackage = okPackage || pkg == p.Package
	}
	okDigest := false
	for _, d := range app.Digests {
		for _, want := range p.Digests {
			okDigest = okDigest || bytes.Equal(d, want)
		}
	}
	if !okPackage || !okDigest {
		return errors.New("the key was made for another app, or for this app signed by someone else")
	}
	return nil
}

//go:embed roots/*.pem
var rootFiles embed.FS

// GoogleRoots are Google's hardware attestation roots, shipped in the
// binary.
func GoogleRoots() (*x509.CertPool, int) {
	pool := x509.NewCertPool()
	entries, _ := rootFiles.ReadDir("roots")
	n := 0
	for _, e := range entries {
		data, err := rootFiles.ReadFile("roots/" + e.Name())
		if err == nil && pool.AppendCertsFromPEM(data) {
			n++
		}
	}
	return pool, n
}
