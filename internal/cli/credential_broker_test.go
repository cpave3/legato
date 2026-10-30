package cli_test

import (
	"context"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/service"
)

// fakeValues stands in for the host's credential broker.
type fakeValues struct {
	uses     []service.CredentialUse
	value    string
	decision service.UseDecision
}

func (f *fakeValues) Value(_ context.Context, use service.CredentialUse) (credential.Secret, service.UseDecision, error) {
	f.uses = append(f.uses, use)
	if !f.decision.Allowed {
		return credential.Secret{}, f.decision, nil
	}
	return credential.Secret{Type: credential.TypeValue, Value: []byte(f.value)}, f.decision, nil
}

func TestTheHostGivesACredentialsValueToTheSessionItsTokenNames(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	execID, token := brokerSession(t, s, "abc123")
	values := &fakeValues{value: "sk-eleven", decision: service.UseDecision{Allowed: true}}
	h := cli.NewBrokerHandler(s, cli.BrokerDeps{Ops: cli.CredentialBrokerOps(values)})

	got, err := call(h, token, "credential.value", map[string]any{"credential": "eleven", "operation": "read"})
	if err != nil {
		t.Fatal(err)
	}
	result := got.(cli.CredentialValueResult)
	if !result.Allowed || string(result.Value) != "sk-eleven" {
		t.Fatalf("result = %+v", result)
	}
	if len(values.uses) != 1 || values.uses[0].TaskID != "abc123" || values.uses[0].ExecutionID != execID || values.uses[0].Credential != "eleven" || values.uses[0].Operation != "read" {
		t.Fatalf("uses = %+v; want the token's session", values.uses)
	}
}

func TestTheHostSaysWhyAUseWasRefusedAndGivesNoValue(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	_, token := brokerSession(t, s, "abc123")
	values := &fakeValues{value: "sk-eleven", decision: service.UseDecision{ApprovalID: "ap-1", Reason: "waiting for the person"}}
	h := cli.NewBrokerHandler(s, cli.BrokerDeps{Ops: cli.CredentialBrokerOps(values)})

	got, err := call(h, token, "credential.value", map[string]any{"credential": "eleven"})
	if err != nil {
		t.Fatal(err)
	}
	result := got.(cli.CredentialValueResult)
	if result.Allowed || len(result.Value) != 0 || result.ApprovalID != "ap-1" || result.Reason != "waiting for the person" {
		t.Fatalf("result = %+v", result)
	}
}
