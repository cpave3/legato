package service

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestMQTTProfileKeepsBrokerLoginHostOnly(t *testing.T) {
	f := newBrokerFixture(t, config.CredentialConfig{Name: "mqtt-login", Secret: "env://MQTT_PROFILE_TEST_LOGIN", Profile: "mqtt", Type: "userpass", Policy: []config.CredentialRule{rule("auto", "read")}})
	t.Setenv("MQTT_PROFILE_TEST_LOGIN", `{"username":"legato","password":"mqtt-test-password"}`)
	ctx := context.Background()
	req := request([]string{"read"})
	req.Credential = "mqtt-login"
	result, err := f.broker.Request(ctx, req)
	if err != nil || result.Status != AccessDenied || !strings.Contains(result.Reason, "Legato's own credential") {
		t.Fatalf("request = %+v, %v", result, err)
	}
	u := use("read", "")
	u.Credential = "mqtt-login"
	for _, check := range []func(context.Context, CredentialUse) (UseDecision, error){f.broker.CheckUse, f.broker.Authorize} {
		decision, err := check(ctx, u)
		if err != nil || decision.Allowed || !strings.Contains(decision.Reason, "Legato's own credential") {
			t.Fatalf("decision = %+v, %v", decision, err)
		}
	}
	if secret, decision, err := f.broker.Value(ctx, u); err != nil || decision.Allowed || len(secret.Value) != 0 {
		t.Fatalf("agent read: %v, %+v, %v", secret, decision, err)
	}
	secret, err := f.broker.UseForLegato(ctx, CredentialUse{Credential: "mqtt-login", Operation: "read", Call: "MQTT connection"})
	if err != nil || secret.Type != credential.TypeUserPass || string(secret.Value) != `{"username":"legato","password":"mqtt-test-password"}` {
		t.Fatalf("host read: %v, %v", secret, err)
	}
	entries, err := f.broker.Audit(ctx, store.CredentialAuditFilter{Credential: "mqtt-login"})
	if err != nil || len(entries) == 0 || entries[0].Event != "use" || !strings.Contains(entries[0].Detail, "MQTT connection") {
		t.Fatalf("audit = %+v, %v", entries, err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Detail, "mqtt-test-password") {
			t.Fatal("audit leaked password")
		}
	}
	if pending, err := f.approvals.List(ctx, store.ApprovalFilter{}); err != nil || len(pending) != 0 {
		t.Fatalf("pending = %+v, %v", pending, err)
	}
}
