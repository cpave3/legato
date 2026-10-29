package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/attest"
	"github.com/cpave3/legato/internal/engine/store"
)

// ApprovalSourceDevices is the source of approvals that add or remove
// devices: each needs a decision signed by an enrolled device.
const ApprovalSourceDevices = "devices"

// challengeLife is how long a challenge can be used.
const challengeLife = 5 * time.Minute

// AndroidPackage is the app whose keys can sign decisions.
const AndroidPackage = "dev.legato.mobile"

// Device is a device that can sign approval decisions.
type Device = store.ApprovalDevice

// DeviceServiceOptions configures the device service.
type DeviceServiceOptions struct {
	// Attest checks an attestation chain (DER, leaf first) made with a
	// challenge. Nil: Android key attestation against Google's roots, the
	// app's package and AndroidDigests, and Google's revocation list.
	Attest func(chain [][]byte, challenge []byte) (attest.Attested, error)
	// AndroidDigests are the SHA-256 digests of the certificates the app
	// may be signed with.
	AndroidDigests [][]byte
	Now            func() time.Time
}

// DeviceService enrolls the devices that sign approval decisions, gives
// one-time challenges, and checks signed decisions.
type DeviceService struct {
	db        *store.Store
	approvals *ApprovalService
	opts      DeviceServiceOptions
}

// NewDeviceService builds the service and makes the approval service check
// signed decisions with it.
func NewDeviceService(db *store.Store, approvals *ApprovalService, opts DeviceServiceOptions) *DeviceService {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	d := &DeviceService{db: db, approvals: approvals, opts: opts}
	if d.opts.Attest == nil {
		roots, _ := attest.GoogleRoots()
		revocations := &attest.RevocationList{}
		d.opts.Attest = func(chain [][]byte, challenge []byte) (attest.Attested, error) {
			certs := make([]*x509.Certificate, 0, len(chain))
			for _, der := range chain {
				cert, err := x509.ParseCertificate(der)
				if err != nil {
					return attest.Attested{}, fmt.Errorf("a certificate of the chain does not read: %w", err)
				}
				certs = append(certs, cert)
			}
			return attest.Verify(certs, attest.Policy{Roots: roots, Challenge: challenge, Package: AndroidPackage,
				Digests: d.opts.AndroidDigests, Revoked: func(serial *big.Int) (bool, error) { return revocations.Revoked(serial) }})
		}
	}
	approvals.SetSignatureVerifier(d)
	approvals.RegisterResolver(ApprovalSourceDevices, deviceResolver{d: d})
	return d
}

// ServerID is this Legato's ID, made once and kept: what devices sign, so
// a signature for one server is no use to another.
func (d *DeviceService) ServerID(ctx context.Context) (string, error) {
	return d.db.InstanceValue(ctx, "server_id", func() string { return "srv-" + randomHex(8) })
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func challengeHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// EnrollChallenge makes a challenge the phone puts in its new key.
func (d *DeviceService) EnrollChallenge(ctx context.Context) ([]byte, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, time.Time{}, err
	}
	expires := d.opts.Now().Add(challengeLife)
	err := d.db.CreateApprovalChallenge(ctx, store.ApprovalChallenge{Hash: challengeHash(raw), Purpose: "enroll", ExpiresAt: expires})
	return raw, expires, err
}

// EnrollRequest is a phone asking to sign decisions.
type EnrollRequest struct {
	Name      string
	Kind      string
	Chain     [][]byte
	Challenge []byte
}

var deviceName = regexp.MustCompile(`[^\p{L}\p{N} ._'()-]+`)

// Enroll checks the key's attestation and keeps the device. The first
// device is enrolled at once; a later one waits for an enrolled device to
// allow it with a signed decision.
func (d *DeviceService) Enroll(ctx context.Context, req EnrollRequest) (Device, error) {
	if req.Kind != "android" {
		return Device{}, fmt.Errorf("devices of kind %q cannot enroll", req.Kind)
	}
	name := strings.TrimSpace(deviceName.ReplaceAllString(req.Name, ""))
	if name == "" {
		name = "Phone"
	}
	if len(name) > 64 {
		name = name[:64]
	}
	if _, err := d.db.SpendApprovalChallenge(ctx, challengeHash(req.Challenge), d.opts.Now()); err != nil {
		return Device{}, errors.New("the enrollment challenge is unknown, used or expired; ask for a new one")
	}
	attested, err := d.opts.Attest(req.Chain, req.Challenge)
	if err != nil {
		return Device{}, fmt.Errorf("the device's key is not accepted: %w", err)
	}
	public, err := x509.MarshalPKIXPublicKey(attested.PublicKey)
	if err != nil {
		return Device{}, err
	}
	device := Device{ID: "dev-" + randomHex(5), Kind: req.Kind, Name: name, PublicKey: public, SecurityLevel: attested.SecurityLevel.String(), Status: store.DeviceActive}
	enrolled, err := d.Enrolled(ctx)
	if err != nil {
		return Device{}, err
	}
	if enrolled {
		device.Status = store.DevicePending
		approval, _, err := d.approvals.Submit(ctx, ApprovalInput{
			Source: ApprovalSourceDevices, SourceKey: "enroll/" + device.ID,
			Title:   "Let " + name + " sign approvals",
			Summary: "A new " + device.Kind + " device (" + device.SecurityLevel + ") asks to sign approval decisions for this Legato.",
			Details: map[string]string{"action": "enroll", "device_id": device.ID, "device_name": name, "kind": device.Kind, "security_level": device.SecurityLevel},
			Options: []ApprovalOption{{ID: "allow", Label: "Allow this device", Kind: ApprovalAllow}, {ID: "deny", Label: "Deny", Kind: ApprovalDeny}},
			Signed:  true,
		})
		if err != nil {
			return Device{}, err
		}
		device.ApprovalID = approval.ID
	}
	if err := d.db.InsertApprovalDevice(ctx, device); err != nil {
		return Device{}, err
	}
	stored, err := d.db.GetApprovalDevice(ctx, device.ID)
	if err != nil {
		return Device{}, err
	}
	return *stored, nil
}

// Enrolled reports whether any device can sign decisions.
func (d *DeviceService) Enrolled(ctx context.Context) (bool, error) {
	active, err := d.db.ListApprovalDevices(ctx, store.DeviceActive)
	return len(active) > 0, err
}

// Devices lists the devices that are enrolled or waiting to be.
func (d *DeviceService) Devices(ctx context.Context) ([]Device, error) {
	all, err := d.db.ListApprovalDevices(ctx, "")
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, device := range all {
		if device.Status != store.DeviceRemoved {
			out = append(out, device)
		}
	}
	return out, nil
}

// Remove asks to remove a device, which needs a signed decision from an
// enrolled device (the removed one included). A device still waiting to be
// enrolled is removed at once. It returns the approval, or "".
func (d *DeviceService) Remove(ctx context.Context, id string) (string, error) {
	device, err := d.db.GetApprovalDevice(ctx, id)
	if err != nil {
		return "", err
	}
	switch device.Status {
	case store.DeviceRemoved:
		return "", store.ErrNotFound
	case store.DevicePending:
		if device.ApprovalID != "" {
			_ = d.approvals.Withdraw(ctx, ApprovalSourceDevices, "enroll/"+device.ID, "the device was removed")
		}
		return "", d.db.SetApprovalDeviceStatus(ctx, id, store.DeviceRemoved)
	}
	approval, _, err := d.approvals.Submit(ctx, ApprovalInput{
		Source: ApprovalSourceDevices, SourceKey: "remove/" + id + "/" + strconv.FormatInt(d.opts.Now().UnixNano(), 36),
		Title:   "Remove " + device.Name,
		Summary: device.Name + " would no longer sign approval decisions for this Legato.",
		Details: map[string]string{"action": "remove", "device_id": id, "device_name": device.Name, "kind": device.Kind},
		Options: []ApprovalOption{{ID: "allow", Label: "Remove it", Kind: ApprovalAllow}, {ID: "deny", Label: "Keep it", Kind: ApprovalDeny}},
		Signed:  true,
	})
	if err != nil {
		return "", err
	}
	return approval.ID, nil
}

// Reset removes every device, for when the only one is lost. It is run at
// the host (legato device reset).
func (d *DeviceService) Reset(ctx context.Context) (int64, error) {
	return d.db.RemoveAllApprovalDevices(ctx)
}

// DecisionChallengeResult is what a device signs over.
type DecisionChallengeResult struct {
	// Canonical is the request in RFC 8785 form, as Legato built it.
	Canonical []byte
	// Challenge is used once, for this approval and device (base64url).
	Challenge string
	ExpiresAt time.Time
}

// DecisionChallenge gives an enrolled device the canonical request and a
// one-time challenge to sign a decision on it.
func (d *DeviceService) DecisionChallenge(ctx context.Context, approvalID, deviceID string) (DecisionChallengeResult, error) {
	device, err := d.db.GetApprovalDevice(ctx, deviceID)
	if err != nil || device.Status != store.DeviceActive {
		return DecisionChallengeResult{}, errors.New("this device is not enrolled to sign decisions")
	}
	approval, err := d.approvals.Get(ctx, approvalID)
	if err != nil {
		return DecisionChallengeResult{}, err
	}
	if approval.Status != store.ApprovalPending {
		return DecisionChallengeResult{}, ErrApprovalDecided
	}
	canonical, err := d.canonical(ctx, *approval)
	if err != nil {
		return DecisionChallengeResult{}, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return DecisionChallengeResult{}, err
	}
	expires := d.opts.Now().Add(challengeLife)
	if err := d.db.CreateApprovalChallenge(ctx, store.ApprovalChallenge{Hash: challengeHash(raw), Purpose: "decide", ApprovalID: approvalID, DeviceID: deviceID, ExpiresAt: expires}); err != nil {
		return DecisionChallengeResult{}, err
	}
	return DecisionChallengeResult{Canonical: canonical, Challenge: base64.RawURLEncoding.EncodeToString(raw), ExpiresAt: expires}, nil
}

// canonical is the request a device signs: what Legato knows of it, never
// what a page or an agent supplied, in RFC 8785 form.
func (d *DeviceService) canonical(ctx context.Context, a Approval) ([]byte, error) {
	server, err := d.ServerID(ctx)
	if err != nil {
		return nil, err
	}
	options := make([]any, 0, len(a.Options))
	for _, o := range a.Options {
		options = append(options, map[string]any{"id": o.ID, "label": o.Label, "kind": o.Kind})
	}
	var details any = map[string]any{}
	if len(a.Details) > 0 {
		dec := json.NewDecoder(strings.NewReader(string(a.Details)))
		dec.UseNumber()
		if err := dec.Decode(&details); err != nil {
			return nil, err
		}
	}
	return canonicalJSON(map[string]any{
		"v": float64(1), "server": server, "approval": a.ID, "revision": float64(a.Revision),
		"source": a.Source, "task": a.TaskID, "execution": a.ExecutionID,
		"title": a.Title, "summary": a.Summary, "options": options, "details": details,
	})
}

var signedPart = regexp.MustCompile(`^[A-Za-z0-9_:-]+$`)

// signedMessage is what a device signs: the RFC 8785 form of
// {challenge, device, option, request, v}, which the app writes by joining
// these parts, as none of them needs escaping.
func signedMessage(challenge, device, option string, canonical []byte) ([]byte, error) {
	for _, part := range []string{challenge, device, option} {
		if !signedPart.MatchString(part) {
			return nil, fmt.Errorf("%q cannot be part of a signed decision", part)
		}
	}
	return []byte(`{"challenge":"` + challenge + `","device":"` + device + `","option":"` + option + `","request":` + string(canonical) + `,"v":1}`), nil
}

// VerifyDecision checks a signed decision: the challenge was made for this
// approval and device and is spent now, whatever the outcome; the request
// is what it was when the challenge was given; and the signature verifies
// with the device's key.
func (d *DeviceService) VerifyDecision(ctx context.Context, approval Approval, decision ApprovalDecision) (string, error) {
	sig := decision.Signature
	device, err := d.db.GetApprovalDevice(ctx, sig.Device)
	if err != nil || device.Status != store.DeviceActive {
		return "", errors.New("the signing device is not enrolled")
	}
	raw, err := base64.RawURLEncoding.DecodeString(sig.Challenge)
	if err != nil {
		return "", errors.New("the challenge does not read")
	}
	challenge, err := d.db.SpendApprovalChallenge(ctx, challengeHash(raw), d.opts.Now())
	if err != nil || challenge.Purpose != "decide" || challenge.ApprovalID != approval.ID || challenge.DeviceID != sig.Device {
		return "", errors.New("the challenge is not one given for this approval and device, or it is used or expired")
	}
	canonical, err := d.canonical(ctx, approval)
	if err != nil {
		return "", err
	}
	option := decision.Option
	if option == "" {
		option = decision.Decision
	}
	msg, err := signedMessage(sig.Challenge, sig.Device, option, canonical)
	if err != nil {
		return "", err
	}
	value, err := base64.StdEncoding.DecodeString(sig.Value)
	if err != nil {
		if value, err = base64.RawURLEncoding.DecodeString(sig.Value); err != nil {
			return "", errors.New("the signature does not read")
		}
	}
	parsed, err := x509.ParsePKIXPublicKey(device.PublicKey)
	if err != nil {
		return "", err
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return "", errors.New("the device's key is not an EC key")
	}
	sum := sha256.Sum256(msg)
	if !ecdsa.VerifyASN1(key, sum[:], value) {
		return "", errors.New("the signature is not the device's over this request, option and challenge")
	}
	_ = d.db.TouchApprovalDevice(ctx, sig.Device)
	return sig.Device, nil
}

// deviceResolver applies decisions on enrolling and removing devices.
type deviceResolver struct{ d *DeviceService }

func (r deviceResolver) Resolve(ctx context.Context, approval Approval, decision ApprovalDecision) error {
	var details struct {
		Action   string `json:"action"`
		DeviceID string `json:"device_id"`
	}
	if err := json.Unmarshal(approval.Details, &details); err != nil {
		return err
	}
	allowed := decision.Decision == ApprovalAllow
	switch {
	case details.Action == "enroll" && allowed:
		return r.d.db.SetApprovalDeviceStatus(ctx, details.DeviceID, store.DeviceActive)
	case details.Action == "enroll":
		return r.d.db.SetApprovalDeviceStatus(ctx, details.DeviceID, store.DeviceRemoved)
	case details.Action == "remove" && allowed:
		return r.d.db.SetApprovalDeviceStatus(ctx, details.DeviceID, store.DeviceRemoved)
	}
	return nil
}
