package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"

	"github.com/cpave3/legato/internal/engine/store"
)

// BrokerSessionEnv gives each agent session the host broker's socket and a
// token of its own, which lets its agent-side processes act for that one
// execution. Only the token's SHA-256 is stored.
type BrokerSessionEnv struct {
	db     *store.Store
	socket string
}

func NewBrokerSessionEnv(db *store.Store, socket string) *BrokerSessionEnv {
	return &BrokerSessionEnv{db: db, socket: socket}
}

// HashBrokerToken is how a token is stored and looked up.
func HashBrokerToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (b *BrokerSessionEnv) SessionEnv(ctx context.Context, _, executionID string) (map[string]string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	token := "lgb_" + base64.RawURLEncoding.EncodeToString(raw)
	if err := b.db.SetExecutionBrokerToken(ctx, executionID, HashBrokerToken(token)); err != nil {
		return nil, err
	}
	return map[string]string{"LEGATO_BROKER_SOCKET": b.socket, "LEGATO_BROKER_TOKEN": token}, nil
}
