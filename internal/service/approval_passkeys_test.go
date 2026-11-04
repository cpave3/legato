package service

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"

	"github.com/cpave3/legato/internal/engine/store"
)

// passkey is a virtual WebAuthn authenticator, as a password manager's
// passkey would be: a P-256 key under one relying party, synced (backup
// eligible), with user verification.
type passkey struct {
	key    *ecdsa.PrivateKey
	credID []byte
	rpID   string
	origin string
	user   []byte
	device string
	// flags the authenticator sets on an assertion; zero is a synced
	// passkey's: presence, verification, backed up.
	assertFlags byte
	// securityKey makes the credential as a hardware key does: not backed up.
	securityKey bool
	// madeAuthData and signedAuthData, when set, are another
	// authenticator's own bytes, sent as they are.
	madeAuthData, signedAuthData []byte
}

func newPasskey(t *testing.T, origin, rpID string) *passkey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return &passkey{key: key, credID: id, rpID: rpID, origin: origin}
}

var b64u = base64.RawURLEncoding

const (
	flagUP = 0x01
	flagUV = 0x04
	flagBE = 0x08
	flagBS = 0x10
	flagAT = 0x40
)

func (p *passkey) authData(flags byte, attested bool) []byte {
	if attested && p.madeAuthData != nil {
		return p.madeAuthData
	}
	if !attested && p.signedAuthData != nil {
		return p.signedAuthData
	}
	rp := sha256.Sum256([]byte(p.rpID))
	out := append([]byte{}, rp[:]...)
	out = append(out, flags, 0, 0, 0, 0)
	if attested {
		out = append(out, make([]byte, 16)...) // AAGUID
		var n [2]byte
		binary.BigEndian.PutUint16(n[:], uint16(len(p.credID)))
		out = append(out, n[:]...)
		out = append(out, p.credID...)
		x, y := p.key.PublicKey.X.FillBytes(make([]byte, 32)), p.key.PublicKey.Y.FillBytes(make([]byte, 32))
		cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
		out = append(out, cose...)
	}
	return out
}

func clientData(kind, challenge, origin string) []byte {
	b, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false})
	return b
}

func (p *passkey) createFlags() byte {
	if p.securityKey {
		return flagUP | flagUV | flagAT
	}
	return flagUP | flagUV | flagBE | flagBS | flagAT
}

// create answers creation options as navigator.credentials.create would,
// in the JSON the web page sends.
func (p *passkey) create(t *testing.T, options []byte) []byte {
	t.Helper()
	var o struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RP        struct {
				ID string `json:"id"`
			} `json:"rp"`
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			AuthenticatorSelection struct {
				UserVerification string `json:"userVerification"`
			} `json:"authenticatorSelection"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &o); err != nil {
		t.Fatal(err)
	}
	if o.PublicKey.RP.ID != p.rpID {
		t.Fatalf("options are for %q, the page is %q", o.PublicKey.RP.ID, p.rpID)
	}
	p.user, _ = b64u.DecodeString(o.PublicKey.User.ID)
	att, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": p.authData(p.createFlags(), true)})
	body, _ := json.Marshal(map[string]any{
		"id": b64u.EncodeToString(p.credID), "rawId": b64u.EncodeToString(p.credID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64u.EncodeToString(clientData("webauthn.create", o.PublicKey.Challenge, p.origin)),
			"attestationObject": b64u.EncodeToString(att),
		},
		"clientExtensionResults": map[string]any{},
	})
	return body
}

// assert answers navigator.credentials.get over challenge.
func (p *passkey) assert(t *testing.T, challenge []byte) []byte {
	t.Helper()
	flags := p.assertFlags
	if flags == 0 {
		flags = flagUP | flagUV | flagBE | flagBS
	}
	auth := p.authData(flags, false)
	cd := clientData("webauthn.get", b64u.EncodeToString(challenge), p.origin)
	cdHash := sha256.Sum256(cd)
	sum := sha256.Sum256(append(append([]byte{}, auth...), cdHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, p.key, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"id": b64u.EncodeToString(p.credID), "rawId": b64u.EncodeToString(p.credID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON": b64u.EncodeToString(cd), "authenticatorData": b64u.EncodeToString(auth),
			"signature": b64u.EncodeToString(sig), "userHandle": b64u.EncodeToString(p.user),
		},
		"clientExtensionResults": map[string]any{},
	})
	return body
}

func (f devicesFixture) enrollPasskey(t *testing.T, p *passkey, name string) (Device, error) {
	t.Helper()
	ctx := context.Background()
	options, err := f.devices.PasskeyOptions(ctx, p.origin)
	if err != nil {
		t.Fatal(err)
	}
	d, err := f.devices.Enroll(ctx, EnrollRequest{Name: name, Kind: "passkey", Credential: p.create(t, options), Origin: p.origin})
	if err == nil {
		p.device = d.ID
	}
	return d, err
}

// signWithPasskey decides as the web page does: the WebAuthn challenge is
// the SHA-256 of the same message a phone signs.
func (f devicesFixture) signWithPasskey(t *testing.T, p *passkey, approvalID, option string) *ApprovalSignature {
	t.Helper()
	c, err := f.devices.DecisionChallenge(context.Background(), approvalID, p.device)
	if err != nil {
		t.Fatal(err)
	}
	if c.Passkey == nil || c.Passkey.CredentialID != b64u.EncodeToString(p.credID) || c.Passkey.RPID != p.rpID {
		t.Fatalf("passkey request = %+v", c.Passkey)
	}
	msg := `{"challenge":"` + c.Challenge + `","device":"` + p.device + `","option":"` + option + `","request":` + string(c.Canonical) + `,"v":1}`
	sum := sha256.Sum256([]byte(msg))
	return &ApprovalSignature{Device: p.device, Challenge: c.Challenge, Value: string(p.assert(t, sum[:]))}
}

func TestAPasskeyEnrollsFromThePageItIsMadeOn(t *testing.T) {
	f := newDevicesFixture(t)
	key := newPasskey(t, "https://erebus.example:3080", "erebus.example")
	d, err := f.enrollPasskey(t, key, "Desktop")
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind != "passkey" || d.Status != store.DeviceActive || d.Origin != "https://erebus.example:3080" || d.SecurityLevel != "synced passkey" {
		t.Fatalf("device = %+v", d)
	}
	// The same challenge cannot enroll a second time.
	ctx := context.Background()
	options, _ := f.devices.PasskeyOptions(ctx, key.origin)
	body := key.create(t, options)
	if _, err := f.devices.Enroll(ctx, EnrollRequest{Name: "Desktop", Kind: "passkey", Credential: body, Origin: key.origin}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.devices.Enroll(ctx, EnrollRequest{Name: "Desktop", Kind: "passkey", Credential: body, Origin: key.origin}); err == nil {
		t.Fatal("a spent challenge enrolled again")
	}
}

func TestAPasskeyMadeForAnotherOriginIsRefused(t *testing.T) {
	f := newDevicesFixture(t)
	ctx := context.Background()
	key := newPasskey(t, "https://evil.example", "erebus.example")
	options, _ := f.devices.PasskeyOptions(ctx, "https://erebus.example:3080")
	key.rpID = "erebus.example"
	// The page says one origin; the browser signed another.
	if _, err := f.devices.Enroll(ctx, EnrollRequest{Kind: "passkey", Credential: key.create(t, options), Origin: "https://erebus.example:3080"}); err == nil {
		t.Fatal("a credential made at another origin was enrolled")
	}
	if _, err := f.devices.PasskeyOptions(ctx, "http://192.168.1.10:3080"); err == nil {
		t.Fatal("a passkey was offered for a bare IP address")
	}
}

func TestALaterPasskeyNeedsAnEnrolledDevice(t *testing.T) {
	f := newDevicesFixture(t)
	f.enroll(t, newPhone(t), "Pixel")
	d, err := f.enrollPasskey(t, newPasskey(t, "https://erebus.example:3080", "erebus.example"), "Desktop")
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != store.DevicePending || d.ApprovalID == "" {
		t.Fatalf("device = %+v", d)
	}
	a, _ := f.approvals.Get(context.Background(), d.ApprovalID)
	if !a.Signed || !strings.Contains(a.Summary, "https://erebus.example:3080") {
		t.Fatalf("approval = %+v", a.Approval)
	}
}

func TestAPasskeySignsADecisionOverTheSameMessage(t *testing.T) {
	f := newDevicesFixture(t)
	ctx := context.Background()
	key := newPasskey(t, "https://erebus.example:3080", "erebus.example")
	if _, err := f.enrollPasskey(t, key, "Desktop"); err != nil {
		t.Fatal(err)
	}
	f.approvals.RegisterResolver(ApprovalSourceCredential, acceptingResolver{})
	submit := func(k string) *Approval {
		a, _, err := f.approvals.Submit(ctx, ApprovalInput{Source: ApprovalSourceCredential, SourceKey: k, TaskID: "t", Title: "Use github",
			Options: []ApprovalOption{{ID: "once", Label: "Once", Kind: ApprovalAllow}, {ID: "deny", Label: "Deny", Kind: ApprovalDeny}}, Signed: true})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	a := submit("k1")
	// Signed for one option, sent with another.
	if _, err := f.approvals.Decide(ctx, a.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "deny", Signature: f.signWithPasskey(t, key, a.ID, "once")}); err == nil {
		t.Fatal("a passkey signature for once allowed deny")
	}
	// An assertion made at another origin.
	elsewhere := *key
	elsewhere.origin = "https://evil.example"
	if _, err := f.approvals.Decide(ctx, a.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", Signature: f.signWithPasskey(t, &elsewhere, a.ID, "once")}); !errors.Is(err, ErrApprovalBadSignature) {
		t.Fatalf("another origin: %v", err)
	}
	decided, err := f.approvals.Decide(ctx, a.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", Signature: f.signWithPasskey(t, key, a.ID, "once")})
	if err != nil {
		t.Fatal(err)
	}
	if decided.DecidedDevice != key.device {
		t.Fatalf("decided device = %q", decided.DecidedDevice)
	}
	// It signs again: its stored credential stays usable after a use.
	b := submit("k2")
	if _, err := f.approvals.Decide(ctx, b.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", Signature: f.signWithPasskey(t, key, b.ID, "once")}); err != nil {
		t.Fatalf("a second use: %v", err)
	}
}

func TestAPhoneEnrollmentKeepsItsAttestationChain(t *testing.T) {
	f := newDevicesFixture(t)
	p := newPhone(t)
	d := f.enroll(t, p, "Pixel")
	chain, err := f.devices.AttestationChain(context.Background(), d.ID)
	if err != nil || len(chain) != 1 || string(chain[0]) != string(p.chain(t)[0]) {
		t.Fatalf("chain = %d certificates, %v", len(chain), err)
	}
}

// A security key signs with a touch alone: presence without verification.
// Legato accepts it, and records which it was.
func TestAPasskeySignsWithPresenceAloneAndTheDecisionSaysSo(t *testing.T) {
	f := newDevicesFixture(t)
	ctx := context.Background()
	key := newPasskey(t, "https://erebus.example:3080", "erebus.example")
	if _, err := f.enrollPasskey(t, key, "YubiKey"); err != nil {
		t.Fatal(err)
	}
	f.approvals.RegisterResolver(ApprovalSourceCredential, acceptingResolver{})
	submit := func(k string) *Approval {
		a, _, err := f.approvals.Submit(ctx, ApprovalInput{Source: ApprovalSourceCredential, SourceKey: k, TaskID: "t", Title: "Use github",
			Options: []ApprovalOption{{ID: "once", Label: "Once", Kind: ApprovalAllow}}, Signed: true})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}

	key.assertFlags = flagUP | flagBE | flagBS
	first := submit("touch")
	touched, err := f.approvals.Decide(ctx, first.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", Signature: f.signWithPasskey(t, key, first.ID, "once")})
	if err != nil {
		t.Fatalf("a touch alone: %v", err)
	}
	if got := touched.Decision.Verification; got != "presence" {
		t.Fatalf("a touch alone is recorded as %q, want presence", got)
	}

	key.assertFlags = flagUP | flagUV | flagBE | flagBS
	second := submit("pin")
	verified, err := f.approvals.Decide(ctx, second.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", Signature: f.signWithPasskey(t, key, second.ID, "once")})
	if err != nil {
		t.Fatalf("a touch and a PIN: %v", err)
	}
	if got := verified.Decision.Verification; got != "verified" {
		t.Fatalf("a verified signature is recorded as %q, want verified", got)
	}
}

// Without even a touch, an assertion is no person's decision.
func TestAPasskeyAssertionWithoutPresenceIsRefused(t *testing.T) {
	f := newDevicesFixture(t)
	ctx := context.Background()
	key := newPasskey(t, "https://erebus.example:3080", "erebus.example")
	if _, err := f.enrollPasskey(t, key, "YubiKey"); err != nil {
		t.Fatal(err)
	}
	f.approvals.RegisterResolver(ApprovalSourceCredential, acceptingResolver{})
	a, _, err := f.approvals.Submit(ctx, ApprovalInput{Source: ApprovalSourceCredential, SourceKey: "k", TaskID: "t", Title: "Use github",
		Options: []ApprovalOption{{ID: "once", Label: "Once", Kind: ApprovalAllow}}, Signed: true})
	if err != nil {
		t.Fatal(err)
	}
	key.assertFlags = flagBE | flagBS // no presence, no verification
	if _, err := f.approvals.Decide(ctx, a.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", Signature: f.signWithPasskey(t, key, a.ID, "once")}); !errors.Is(err, ErrApprovalBadSignature) {
		t.Fatalf("no presence: %v", err)
	}
}

// Enrolling asks for verification where the authenticator can give it,
// without requiring it: a security key may then sign with a touch.
func TestPasskeyOptionsPreferVerification(t *testing.T) {
	f := newDevicesFixture(t)
	options, err := f.devices.PasskeyOptions(context.Background(), "https://erebus.example:3080")
	if err != nil {
		t.Fatal(err)
	}
	var o struct {
		PublicKey struct {
			AuthenticatorSelection struct {
				UserVerification string `json:"userVerification"`
			} `json:"authenticatorSelection"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &o); err != nil {
		t.Fatal(err)
	}
	if got := o.PublicKey.AuthenticatorSelection.UserVerification; got != "preferred" {
		t.Fatalf("userVerification = %q, want preferred", got)
	}
}

// The desktop app makes one credential per security key, at an origin of
// its own, and enrolls that same credential on each server it pairs with.
func TestOneCredentialEnrollsAndSignsOnSeveralServers(t *testing.T) {
	ctx := context.Background()
	key := newPasskey(t, "https://legato-desktop.invalid", "legato-desktop.invalid")
	key.securityKey = true
	key.assertFlags = flagUP // a touch, as the app asks
	for _, name := range []string{"erebus", "nyx"} {
		f := newDevicesFixture(t)
		device, err := f.enrollPasskey(t, key, "YubiKey")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if device.Origin != "https://legato-desktop.invalid" {
			t.Fatalf("%s: origin = %q", name, device.Origin)
		}
		f.approvals.RegisterResolver(ApprovalSourceCredential, acceptingResolver{})
		a, _, err := f.approvals.Submit(ctx, ApprovalInput{Source: ApprovalSourceCredential, SourceKey: "k", TaskID: "t", Title: "Use github",
			Options: []ApprovalOption{{ID: "once", Label: "Once", Kind: ApprovalAllow}}, Signed: true})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.approvals.Decide(ctx, a.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", Signature: f.signWithPasskey(t, key, a.ID, "once")}); err != nil {
			t.Fatalf("%s: signing: %v", name, err)
		}
	}
}

// The desktop app on a Mac is its own authenticator: a Secure Enclave key
// that signs after a fingerprint, in authenticator data the app builds.
// desktop/src-tauri/tests/fixtures/touch-id-key.json holds what it builds
// for a known key (rewritten by its test with LEGATO_UPDATE_FIXTURES=1);
// Legato enrolls those bytes and accepts their signature as verified.
func TestADesktopTouchIDKeyEnrollsAndSignsAsVerified(t *testing.T) {
	raw, err := os.ReadFile("../../desktop/src-tauri/tests/fixtures/touch-id-key.json")
	if err != nil {
		t.Fatal(err)
	}
	var fx struct {
		PrivateKey     string `json:"private_key"`
		CredentialID   string `json:"credential_id"`
		MadeAuthData   string `json:"made_auth_data"`
		SignedAuthData string `json:"signed_auth_data"`
	}
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	scalar, err := hex.DecodeString(fx.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := ecdh.P256().NewPrivateKey(scalar)
	if err != nil {
		t.Fatal(err)
	}
	pub := priv.PublicKey().Bytes()
	key := &passkey{
		key: &ecdsa.PrivateKey{
			PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(pub[1:33]), Y: new(big.Int).SetBytes(pub[33:])},
			D:         new(big.Int).SetBytes(scalar),
		},
		rpID: "legato-desktop.invalid", origin: "https://legato-desktop.invalid",
	}
	for _, field := range []struct {
		into *[]byte
		from string
	}{{&key.credID, fx.CredentialID}, {&key.madeAuthData, fx.MadeAuthData}, {&key.signedAuthData, fx.SignedAuthData}} {
		if *field.into, err = b64u.DecodeString(field.from); err != nil {
			t.Fatal(err)
		}
	}

	f := newDevicesFixture(t)
	ctx := context.Background()
	device, err := f.enrollPasskey(t, key, "Touch ID")
	if err != nil {
		t.Fatalf("enrolling: %v", err)
	}
	if device.SecurityLevel != "passkey" {
		t.Fatalf("security level = %q, want passkey: the key never leaves the Mac", device.SecurityLevel)
	}
	f.approvals.RegisterResolver(ApprovalSourceCredential, acceptingResolver{})
	a, _, err := f.approvals.Submit(ctx, ApprovalInput{Source: ApprovalSourceCredential, SourceKey: "k", TaskID: "t", Title: "Use github",
		Options: []ApprovalOption{{ID: "once", Label: "Once", Kind: ApprovalAllow}}, Signed: true})
	if err != nil {
		t.Fatal(err)
	}
	decided, err := f.approvals.Decide(ctx, a.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", Signature: f.signWithPasskey(t, key, a.ID, "once")})
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if got := decided.Decision.Verification; got != "verified" {
		t.Fatalf("a fingerprint is recorded as %q, want verified", got)
	}
}
