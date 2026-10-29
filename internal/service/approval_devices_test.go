package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/attest"
	"github.com/cpave3/legato/internal/engine/store"
)

// phone is a fake enrolled device: a P-256 key, as an Android Keystore key
// would be, whose "attestation" is its public key in DER.
type phone struct {
	key *ecdsa.PrivateKey
	id  string
}

func newPhone(t *testing.T) *phone {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &phone{key: key}
}

func (p *phone) chain(t *testing.T) [][]byte {
	der, err := x509.MarshalPKIXPublicKey(&p.key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return [][]byte{der}
}

// fakeAttest accepts a "chain" of one public key, made with the challenge
// the test expects.
func fakeAttest(chain [][]byte, challenge []byte) (attest.Attested, error) {
	if len(chain) != 1 || len(challenge) == 0 {
		return attest.Attested{}, errors.New("not attested")
	}
	key, err := x509.ParsePKIXPublicKey(chain[0])
	if err != nil {
		return attest.Attested{}, err
	}
	return attest.Attested{PublicKey: key.(*ecdsa.PublicKey), SecurityLevel: attest.StrongBox}, nil
}

type devicesFixture struct {
	approvals *ApprovalService
	devices   *DeviceService
	db        *store.Store
	now       *time.Time
}

func newDevicesFixture(t *testing.T) devicesFixture {
	t.Helper()
	approvals, db, _ := newApprovalFixture(t)
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	devices := NewDeviceService(db, approvals, DeviceServiceOptions{Attest: fakeAttest, Now: func() time.Time { return now }})
	return devicesFixture{approvals: approvals, devices: devices, db: db, now: &now}
}

func (f devicesFixture) enroll(t *testing.T, p *phone, name string) Device {
	t.Helper()
	ctx := context.Background()
	challenge, _, err := f.devices.EnrollChallenge(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d, err := f.devices.Enroll(ctx, EnrollRequest{Name: name, Kind: "android", Chain: p.chain(t), Challenge: challenge})
	if err != nil {
		t.Fatal(err)
	}
	p.id = d.ID
	return d
}

// sign makes a signed decision as the phone app does: it asks for a
// challenge, builds the message from the canonical bytes it was given, and
// signs it.
func (f devicesFixture) sign(t *testing.T, p *phone, approvalID, option string) *ApprovalSignature {
	t.Helper()
	c, err := f.devices.DecisionChallenge(context.Background(), approvalID, p.id)
	if err != nil {
		t.Fatal(err)
	}
	msg := `{"challenge":"` + c.Challenge + `","device":"` + p.id + `","option":"` + option + `","request":` + string(c.Canonical) + `,"v":1}`
	sum := sha256.Sum256([]byte(msg))
	sig, err := ecdsa.SignASN1(rand.Reader, p.key, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return &ApprovalSignature{Device: p.id, Challenge: c.Challenge, Value: base64.StdEncoding.EncodeToString(sig)}
}

func TestTheFirstDeviceIsEnrolledWithTheTokenAlone(t *testing.T) {
	f := newDevicesFixture(t)
	d := f.enroll(t, newPhone(t), "Pixel 9 Pro")
	if d.Status != store.DeviceActive || d.SecurityLevel != "strongbox" || !strings.HasPrefix(d.ID, "dev-") {
		t.Fatalf("device = %+v", d)
	}
	// An enrollment challenge is used once.
	challenge, _, _ := f.devices.EnrollChallenge(context.Background())
	p := newPhone(t)
	_, _ = f.devices.Enroll(context.Background(), EnrollRequest{Name: "A", Kind: "android", Chain: p.chain(t), Challenge: challenge})
	if _, err := f.devices.Enroll(context.Background(), EnrollRequest{Name: "B", Kind: "android", Chain: p.chain(t), Challenge: challenge}); err == nil {
		t.Fatal("a challenge enrolled twice")
	}
}

func TestALaterDeviceNeedsASignedApprovalFromAnEnrolledOne(t *testing.T) {
	f := newDevicesFixture(t)
	ctx := context.Background()
	first := newPhone(t)
	f.enroll(t, first, "Pixel 9 Pro")
	second := newPhone(t)
	d := f.enroll(t, second, "Pixel 4 XL")
	if d.Status != store.DevicePending || d.ApprovalID == "" {
		t.Fatalf("second device = %+v", d)
	}
	approval, _ := f.approvals.Get(ctx, d.ApprovalID)
	if !approval.Signed || approval.Source != ApprovalSourceDevices {
		t.Fatalf("approval = %+v", approval)
	}
	if _, err := f.approvals.Decide(ctx, d.ApprovalID, ApprovalDecision{Decision: ApprovalAllow, Option: "allow", By: "web"}); !errors.Is(err, ErrApprovalNeedsSignature) {
		t.Fatalf("unsigned allow: %v", err)
	}
	// The pending device cannot sign for itself.
	if _, err := f.devices.DecisionChallenge(ctx, d.ApprovalID, second.id); err == nil {
		t.Fatal("a pending device was given a challenge")
	}
	sig := f.sign(t, first, d.ApprovalID, "allow")
	decided, err := f.approvals.Decide(ctx, d.ApprovalID, ApprovalDecision{Decision: ApprovalAllow, Option: "allow", By: "phone", Signature: sig})
	if err != nil || decided.DecidedDevice != first.id {
		t.Fatalf("signed allow: %+v, %v", decided, err)
	}
	got, _ := f.db.GetApprovalDevice(ctx, second.id)
	if got.Status != store.DeviceActive {
		t.Fatalf("second device after approval: %s", got.Status)
	}
}

func TestASignatureIsBoundToItsChallengeRequestAndDevice(t *testing.T) {
	f := newDevicesFixture(t)
	ctx := context.Background()
	first, other := newPhone(t), newPhone(t)
	f.enroll(t, first, "Pixel")
	submit := func(key string) *Approval {
		a, _, err := f.approvals.Submit(ctx, ApprovalInput{Source: ApprovalSourceCredential, SourceKey: key, TaskID: "t", Title: "Use github",
			Options: []ApprovalOption{{ID: "once", Label: "Once", Kind: ApprovalAllow}, {ID: "deny", Label: "Deny", Kind: ApprovalDeny}}, Signed: true})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	f.approvals.RegisterResolver(ApprovalSourceCredential, acceptingResolver{})
	a := submit("k1")
	sig := f.sign(t, first, a.ID, "once")

	// Another device's key over the same message.
	forged := *sig
	other.id = first.id
	forged.Value = f.sign(t, other, a.ID, "once").Value
	if _, err := f.approvals.Decide(ctx, a.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", Signature: &forged}); !errors.Is(err, ErrApprovalBadSignature) {
		t.Fatalf("another key: %v", err)
	}
	// Signed for one option, sent with another.
	if _, err := f.approvals.Decide(ctx, a.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "deny", Signature: f.sign(t, first, a.ID, "once")}); err == nil {
		t.Fatal("a signature for once allowed deny")
	}
	good := f.sign(t, first, a.ID, "once")
	if _, err := f.approvals.Decide(ctx, a.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", Signature: good}); err != nil {
		t.Fatal(err)
	}
	// The challenge is spent: it cannot decide another request.
	b := submit("k2")
	replay := *good
	if _, err := f.approvals.Decide(ctx, b.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", Signature: &replay}); !errors.Is(err, ErrApprovalBadSignature) {
		t.Fatalf("a replayed challenge: %v", err)
	}
	// An expired challenge.
	stale := f.sign(t, first, b.ID, "once")
	*f.now = f.now.Add(6 * time.Minute)
	if _, err := f.approvals.Decide(ctx, b.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", Signature: stale}); !errors.Is(err, ErrApprovalBadSignature) {
		t.Fatalf("an expired challenge: %v", err)
	}
	// A denial needs no signature.
	if _, err := f.approvals.Decide(ctx, b.ID, ApprovalDecision{Decision: ApprovalDeny, By: "tui"}); err != nil {
		t.Fatalf("an unsigned denial: %v", err)
	}
}

type acceptingResolver struct{}

func (acceptingResolver) Resolve(context.Context, Approval, ApprovalDecision) error { return nil }

func TestTheCanonicalFormIsRFC8785(t *testing.T) {
	got, err := canonicalJSON(map[string]any{
		"z": []any{float64(1), "é ", true, nil},
		"a": map[string]any{"b": float64(1e21), "A": float64(-0.5), "c": "line\n\"quote\"\t<tag>&"},
		"€": float64(10),
		"1": "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"1":"x","a":{"A":-0.5,"b":1e+21,"c":"line\n\"quote\"\t<tag>&"},"z":[1,"é` + " " + `",true,null],"€":10}`
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestSignedRulesAndTheirDefaults(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name     string
		rule     config.CredentialRule
		enrolled bool
		signed   bool
	}{
		{"request, no device", config.CredentialRule{Approve: "request"}, false, false},
		{"request, a device", config.CredentialRule{Approve: "request"}, true, true},
		{"request, a device, signed false", config.CredentialRule{Approve: "request", Signed: &no}, true, false},
		{"session, a device", config.CredentialRule{Approve: "session"}, true, false},
		{"session, signed true", config.CredentialRule{Approve: "session", Signed: &yes}, false, true},
	} {
		if got := needsSignature(tc.rule, tc.enrolled); got != tc.signed {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.signed)
		}
	}
}

func TestResetRemovesEveryDevice(t *testing.T) {
	f := newDevicesFixture(t)
	f.enroll(t, newPhone(t), "Pixel")
	n, err := f.devices.Reset(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("reset = %d, %v", n, err)
	}
	if enrolled, _ := f.devices.Enrolled(context.Background()); enrolled {
		t.Fatal("a device is still enrolled")
	}
	// After a reset, the next device is a first device again.
	if d := f.enroll(t, newPhone(t), "Pixel again"); d.Status != store.DeviceActive {
		t.Fatalf("device = %+v", d)
	}
}

func TestTheAuditLogNamesTheDeviceThatSigned(t *testing.T) {
	bf := newBrokerFixture(t, eleven(func() config.CredentialRule { r := rule("request", "read"); yes := true; r.Signed = &yes; return r }()))
	now := time.Now()
	devices := NewDeviceService(bf.store, bf.approvals, DeviceServiceOptions{Attest: fakeAttest, Now: func() time.Time { return now }})
	f := devicesFixture{approvals: bf.approvals, devices: devices, db: bf.store, now: &now}
	p := newPhone(t)
	f.enroll(t, p, "Pixel")
	ctx := context.Background()
	result, _ := bf.broker.Request(ctx, request([]string{"read"}))
	approval, _ := bf.approvals.Get(ctx, result.ApprovalID)
	if !approval.Signed {
		t.Fatal("a signed rule's approval does not need a signature")
	}
	if _, err := bf.approvals.Decide(ctx, approval.ID, ApprovalDecision{Decision: ApprovalAllow, Option: "once", By: "phone", Signature: f.sign(t, p, approval.ID, "once")}); err != nil {
		t.Fatal(err)
	}
	grants, _ := bf.store.ListCredentialAudit(ctx, store.CredentialAuditFilter{Event: "grant"})
	if len(grants) != 1 || !strings.Contains(grants[0].DecidedBy, p.id) {
		t.Fatalf("grant rows = %+v", grants)
	}
	if bad, _ := bf.store.VerifyCredentialAudit(ctx); bad != 0 {
		t.Fatalf("the chain breaks at %d", bad)
	}
}
