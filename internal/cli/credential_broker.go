package cli

import (
	"context"
	"encoding/json"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/service"
)

// CredentialValues is the host's credential broker, as `credential.value`
// uses it.
type CredentialValues interface {
	Value(ctx context.Context, use service.CredentialUse) (credential.Secret, service.UseDecision, error)
}

// CredentialValueResult is what `credential.value` answers: the decision on
// the use, and the value when it was allowed.
type CredentialValueResult struct {
	Allowed    bool   `json:"allowed"`
	Value      []byte `json:"value,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// CredentialBrokerOps are the credential operations the host runs itself,
// with the broker it keeps, so that a session's `legato credential get`
// does not sign in to a store such as 1Password each time. The use is the
// caller's: its session comes from the token, never from the arguments.
func CredentialBrokerOps(values CredentialValues) map[string]BrokerOp {
	return map[string]BrokerOp{
		"credential.value": func(ctx context.Context, caller BrokerCaller, raw json.RawMessage) (any, error) {
			var args struct {
				Credential string `json:"credential"`
				Operation  string `json:"operation"`
				Resource   string `json:"resource"`
			}
			if err := json.Unmarshal(raw, &args); err != nil || args.Credential == "" {
				return nil, refuse("bad_request", "credential is required")
			}
			if args.Operation == "" {
				args.Operation = "read"
			}
			secret, decision, err := values.Value(ctx, service.CredentialUse{TaskID: caller.TaskID, ExecutionID: caller.ExecutionID,
				Credential: args.Credential, Operation: args.Operation, Resource: args.Resource})
			if err != nil {
				return nil, refuse("credential_unavailable", err.Error())
			}
			result := CredentialValueResult{Allowed: decision.Allowed, ApprovalID: decision.ApprovalID, Reason: decision.Reason}
			if decision.Allowed {
				result.Value = secret.Value
			}
			return result, nil
		},
	}
}
