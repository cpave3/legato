package credential

import (
	"context"
	"time"
)

// Scope is what a grant covers: operations on resources.
type Scope struct {
	Operations []string
	Resources  []string
}

// Issued is a credential an issuer made for one grant.
type Issued struct {
	Value     []byte
	ExpiresAt time.Time // zero: it lasts as long as the secret
	// ID names it for revocation and the audit log (never the value).
	ID string
}

// Issuer makes a credential from a stored secret for a granted scope.
type Issuer interface {
	// Accepts lists the secret types it can work from.
	Accepts() []string
	Issue(ctx context.Context, secret Secret, scope Scope) (Issued, error)
}

// Revoker is an issuer that can end a credential before it expires.
type Revoker interface {
	Revoke(ctx context.Context, issued Issued) error
}

// Call is one use of a credential as a channel saw it: a request line, an
// SSH login, a helper's question. Its fields depend on the channel.
type Call struct {
	Channel string
	Fields  map[string]string
	Body    []byte
}

// Classified is what an inspector says a call does.
type Classified struct {
	Operation string
	Resource  string
	Amounts   map[string]int64
}

// Inspector says what a call does, in terms a policy reads.
type Inspector interface {
	Classify(ctx context.Context, call Call) (Classified, error)
}

// Execution is the agent session a channel serves.
type Execution struct {
	TaskID      string
	ExecutionID string
	AgentKind   string
}

// Channel gives credentials to an agent session through a protocol its
// tools speak. Open returns the environment the session gets.
type Channel interface {
	Open(ctx context.Context, execution Execution) (map[string]string, error)
	Close(ctx context.Context, execution Execution) error
}

// StaticIssuer gives the secret as it is, for services with long-lived keys.
type StaticIssuer struct{}

func (StaticIssuer) Accepts() []string {
	return []string{TypeValue, TypeUserPass, TypeKeyPair, TypeOAuthClient, TypeOAuthRefresh}
}

func (StaticIssuer) Issue(_ context.Context, secret Secret, _ Scope) (Issued, error) {
	return Issued{Value: secret.Value}, nil
}
