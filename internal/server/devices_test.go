package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/engine/attest"
	"github.com/cpave3/legato/internal/service"
)

func plainKeyAttest(chain [][]byte, challenge []byte) (attest.Attested, error) {
	if len(chain) != 1 {
		return attest.Attested{}, errors.New("not attested")
	}
	key, err := x509.ParsePKIXPublicKey(chain[0])
	if err != nil {
		return attest.Attested{}, err
	}
	return attest.Attested{PublicKey: key.(*ecdsa.PublicKey), SecurityLevel: attest.TrustedEnvironment}, nil
}

func decode(t *testing.T, body []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
}

func TestADeviceEnrollsAndSignsADecisionOverTheAPI(t *testing.T) {
	srv, approvals, resolver := newApprovalsServer(t)
	devices := service.NewDeviceService(approvals.Store(), approvals, service.DeviceServiceOptions{Attest: plainKeyAttest})
	srv.SetDeviceService(devices)

	rec := serve(srv, "GET", "/api/devices", "")
	var listed struct {
		Devices  []service.Device `json:"devices"`
		Enrolled bool             `json:"enrolled"`
		ServerID string           `json:"server_id"`
	}
	decode(t, rec.Body.Bytes(), &listed)
	if rec.Code != 200 || listed.Enrolled || listed.ServerID == "" {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}

	rec = serve(srv, "POST", "/api/devices/challenge", "")
	var challenge struct {
		Challenge string `json:"challenge"`
	}
	decode(t, rec.Body.Bytes(), &challenge)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	body, _ := json.Marshal(map[string]any{"name": "Pixel 9 Pro", "kind": "android", "chain": []string{base64.StdEncoding.EncodeToString(pub)}, "challenge": challenge.Challenge})
	rec = serve(srv, "POST", "/api/devices", string(body))
	var device service.Device
	decode(t, rec.Body.Bytes(), &device)
	if rec.Code != http.StatusCreated || device.Status != "active" {
		t.Fatalf("enroll: %d %s", rec.Code, rec.Body)
	}

	approval, _, _ := approvals.Submit(context.Background(), service.ApprovalInput{Source: "test", SourceKey: "signed", Title: "Push",
		Options: []service.ApprovalOption{{ID: "once", Label: "Once", Kind: service.ApprovalAllow}, {ID: "deny", Label: "Deny", Kind: service.ApprovalDeny}}, Signed: true})
	rec = serve(srv, "POST", "/api/approvals/"+approval.ID+"/decision", `{"decision":"allow","option":"once"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unsigned: %d %s", rec.Code, rec.Body)
	}

	rec = serve(srv, "POST", "/api/approvals/"+approval.ID+"/challenge", `{"device":"`+device.ID+`"}`)
	var dc struct {
		Canonical string `json:"canonical"`
		Challenge string `json:"challenge"`
	}
	decode(t, rec.Body.Bytes(), &dc)
	canonical, _ := base64.StdEncoding.DecodeString(dc.Canonical)
	msg := `{"challenge":"` + dc.Challenge + `","device":"` + device.ID + `","option":"once","request":` + string(canonical) + `,"v":1}`
	sum := sha256.Sum256([]byte(msg))
	sig, _ := ecdsa.SignASN1(rand.Reader, key, sum[:])
	decision, _ := json.Marshal(map[string]any{"decision": "allow", "option": "once", "by": "phone",
		"signature": map[string]string{"device": device.ID, "challenge": dc.Challenge, "value": base64.StdEncoding.EncodeToString(sig)}})
	rec = serve(srv, "POST", "/api/approvals/"+approval.ID+"/decision", string(decision))
	if rec.Code != 200 || len(resolver.decisions) != 1 || resolver.decisions[0].Device != device.ID {
		t.Fatalf("signed: %d %s %+v", rec.Code, rec.Body, resolver.decisions)
	}

	// Removing the only device is itself a signed approval.
	rec = serve(srv, "DELETE", "/api/devices/"+device.ID, "")
	var removal struct {
		ApprovalID string `json:"approval_id"`
	}
	decode(t, rec.Body.Bytes(), &removal)
	if rec.Code != http.StatusAccepted || removal.ApprovalID == "" {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body)
	}
}
