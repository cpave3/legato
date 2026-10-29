package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/cpave3/legato/internal/engine/store"
)

// Desktop passkeys sign approval decisions as a phone does, through WebAuthn
// in the browser. A passkey belongs to the page it was made on (its relying
// party is that page's host name), not to the server that checks it: a page
// of one Legato can enroll a passkey on another, headless one. The server
// keeps the page's origin and accepts the passkey's assertions only from it.
// The WebAuthn challenge of a decision is the SHA-256 of the message a phone
// signs, so an assertion binds the same request, option and challenge.

// PasskeyRequest is what a page needs to ask its passkey for a decision.
type PasskeyRequest struct {
	CredentialID string `json:"credential_id"`
	RPID         string `json:"rp_id"`
	Origin       string `json:"origin"`
}

// passkeyRecord is what a passkey device stores in its key column.
type passkeyRecord struct {
	RPID       string              `json:"rp_id"`
	Credential webauthn.Credential `json:"credential"`
}

// passkeyUser is this Legato, as the account its passkeys are made for.
type passkeyUser struct {
	id          []byte
	name        string
	credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return u.id }
func (u passkeyUser) WebAuthnName() string                       { return u.name }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.name }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// passkeyRP is the relying party of a page: its host name. WebAuthn needs a
// secure context and a domain name, so an http page other than localhost,
// and an IP address, have none.
func passkeyRP(origin string) (string, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("%q is not a page's origin", origin)
	}
	host := u.Hostname()
	if net.ParseIP(host) != nil {
		return "", errors.New("a passkey needs the page opened at a host name, not an IP address")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && host == "localhost") {
		return "", errors.New("a passkey needs the page opened over HTTPS (or on localhost)")
	}
	return host, nil
}

func (d *DeviceService) passkeyUser(ctx context.Context, rpID string) (passkeyUser, error) {
	server, err := d.ServerID(ctx)
	if err != nil {
		return passkeyUser{}, err
	}
	name := d.opts.Name
	if name == "" {
		name, _ = os.Hostname()
	}
	user := passkeyUser{id: []byte(server), name: "Legato on " + name}
	devices, err := d.db.ListApprovalDevices(ctx, "")
	if err != nil {
		return passkeyUser{}, err
	}
	for _, dev := range devices {
		if dev.Kind != "passkey" || dev.Status == store.DeviceRemoved {
			continue
		}
		var rec passkeyRecord
		if json.Unmarshal(dev.PublicKey, &rec) == nil && rec.RPID == rpID {
			user.credentials = append(user.credentials, rec.Credential)
		}
	}
	return user, nil
}

func relyingParty(rpID, origin string) (*webauthn.WebAuthn, error) {
	return webauthn.New(&webauthn.Config{RPID: rpID, RPDisplayName: "Legato", RPOrigins: []string{origin}})
}

// PasskeyOptions starts enrolling a passkey from the page at origin: the
// options for navigator.credentials.create, as JSON.
func (d *DeviceService) PasskeyOptions(ctx context.Context, origin string) ([]byte, error) {
	rpID, err := passkeyRP(origin)
	if err != nil {
		return nil, err
	}
	rp, err := relyingParty(rpID, origin)
	if err != nil {
		return nil, err
	}
	user, err := d.passkeyUser(ctx, rpID)
	if err != nil {
		return nil, err
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(user.credentials))
	for _, c := range user.credentials {
		exclude = append(exclude, c.Descriptor())
	}
	creation, session, err := rp.BeginRegistration(user,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementPreferred, UserVerification: protocol.VerificationRequired}),
		webauthn.WithConveyancePreference(protocol.PreferNoAttestation),
		webauthn.WithExclusions(exclude))
	if err != nil {
		return nil, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(session.Challenge)
	if err != nil {
		return nil, err
	}
	if err := d.db.CreateApprovalChallenge(ctx, store.ApprovalChallenge{Hash: challengeHash(raw), Purpose: "enroll-passkey", ExpiresAt: d.opts.Now().Add(challengeLife)}); err != nil {
		return nil, err
	}
	return json.Marshal(creation)
}

// passkeyDevice checks a new passkey: its challenge is one PasskeyOptions
// gave, unused; it was made on the page that sent it; and it verifies.
func (d *DeviceService) passkeyDevice(ctx context.Context, req EnrollRequest) (Device, error) {
	parsed, err := protocol.ParseCredentialCreationResponseBytes(req.Credential)
	if err != nil {
		return Device{}, fmt.Errorf("the passkey does not read: %w", err)
	}
	client := parsed.Response.CollectedClientData
	raw, err := base64.RawURLEncoding.DecodeString(client.Challenge)
	if err != nil {
		return Device{}, errors.New("the passkey's challenge does not read")
	}
	challenge, err := d.db.SpendApprovalChallenge(ctx, challengeHash(raw), d.opts.Now())
	if err != nil || challenge.Purpose != "enroll-passkey" {
		return Device{}, errors.New("the enrollment challenge is unknown, used or expired; ask for a new one")
	}
	if client.Origin != req.Origin {
		return Device{}, fmt.Errorf("the passkey was made at %s, not at the page that sent it", client.Origin)
	}
	rpID, err := passkeyRP(client.Origin)
	if err != nil {
		return Device{}, err
	}
	rp, err := relyingParty(rpID, client.Origin)
	if err != nil {
		return Device{}, err
	}
	user, err := d.passkeyUser(ctx, rpID)
	if err != nil {
		return Device{}, err
	}
	credential, err := rp.CreateCredential(user, webauthn.SessionData{
		Challenge: client.Challenge, RelyingPartyID: rpID, UserID: user.id,
		UserVerification: protocol.VerificationRequired, CredParams: webauthn.CredentialParametersDefault(),
	}, parsed)
	if err != nil {
		return Device{}, fmt.Errorf("the passkey is not accepted: %w", err)
	}
	record, err := json.Marshal(passkeyRecord{RPID: rpID, Credential: *credential})
	if err != nil {
		return Device{}, err
	}
	level := "passkey"
	if credential.Flags.BackupEligible {
		level = "synced passkey"
	}
	return Device{Kind: "passkey", PublicKey: record, SecurityLevel: level, Origin: client.Origin}, nil
}

// passkeyRequest is what the page needs to ask a passkey device.
func passkeyRequest(device *Device) (*PasskeyRequest, error) {
	var rec passkeyRecord
	if err := json.Unmarshal(device.PublicKey, &rec); err != nil {
		return nil, err
	}
	return &PasskeyRequest{CredentialID: base64.RawURLEncoding.EncodeToString(rec.Credential.ID), RPID: rec.RPID, Origin: device.Origin}, nil
}

// verifyPasskey checks an assertion over msg, made at the passkey's own
// page, with user verification, and keeps the credential's new state.
func (d *DeviceService) verifyPasskey(ctx context.Context, device *Device, msg []byte, value string) error {
	var rec passkeyRecord
	if err := json.Unmarshal(device.PublicKey, &rec); err != nil {
		return err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes([]byte(value))
	if err != nil {
		return fmt.Errorf("the passkey's answer does not read: %w", err)
	}
	rp, err := relyingParty(rec.RPID, device.Origin)
	if err != nil {
		return err
	}
	server, err := d.ServerID(ctx)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(msg)
	credential, err := rp.ValidateLogin(passkeyUser{id: []byte(server), credentials: []webauthn.Credential{rec.Credential}}, webauthn.SessionData{
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]), RelyingPartyID: rec.RPID, UserID: []byte(server),
		AllowedCredentialIDs: [][]byte{rec.Credential.ID}, UserVerification: protocol.VerificationRequired,
	}, parsed)
	if err != nil {
		return fmt.Errorf("the passkey's signature is not over this request, option and challenge, from its page: %w", err)
	}
	if credential.Authenticator.CloneWarning {
		return errors.New("the passkey's counter went backwards: it may have been copied")
	}
	rec.Credential = *credential
	updated, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return d.db.SetApprovalDeviceKey(ctx, device.ID, updated)
}
