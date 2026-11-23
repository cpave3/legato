package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// burnliteRuntimes holds each database's one Burnlite connection in this
// process: the ticket source, the close step and both web servers share it.
var burnliteRuntimes = struct {
	sync.Mutex
	by map[*store.Store]*service.BurnliteConnections
}{by: map[*store.Store]*service.BurnliteConnections{}}

// burnliteConnections is the connection the Burnlite board shares with
// agents' MCP connection to Burnlite (LEG-544): its tokens are the MCP
// connection's authorization, refreshed by OAuthFlowService under its lease
// across processes, revoked on disconnect, every request through the
// connection's egress policy.
func burnliteConnections(cfg *config.Config, db *store.Store) *service.BurnliteConnections {
	burnliteRuntimes.Lock()
	defer burnliteRuntimes.Unlock()
	if b, ok := burnliteRuntimes.by[db]; ok {
		return b
	}
	b, _ := newBurnliteConnections(cfg, db)
	migrateBurnlite(cfg, db, b)
	burnliteRuntimes.by[db] = b
	return b
}

// migrateBurnlite joins a board connection from before LEG-544 with agents'
// MCP connection, once per process; it does nothing once they are one. A
// copy of the database is written beside it first, when there is something
// to change.
func migrateBurnlite(cfg *config.Config, db *store.Store, b *service.BurnliteConnections) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err := b.Migrate(ctx, service.BurnliteMigrationOptions{Backup: func(ctx context.Context) (string, error) {
		database, err := config.ResolveDatabase(cfg)
		if err != nil {
			return "", err
		}
		path := fmt.Sprintf("%s.%s.leg-544.bak", database.Path, time.Now().UTC().Format("20060102T150405"))
		return path, db.BackupTo(ctx, path)
	}})
	if err != nil {
		log.Printf("burnlite: joining the board's and agents' connections: %v", err)
	}
}

// newBurnliteConnections builds the connection over its own registry and
// OAuth flow service; stop ends the flow service's lease renewal (a command
// that exits calls it).
func newBurnliteConnections(cfg *config.Config, db *store.Store) (*service.BurnliteConnections, func()) {
	secretStore := configuredSecretStore(cfg, db)
	secrets := service.SealedIntegrationSecrets{Store: secretStore}
	conns := service.NewMCPConnectionService(db, service.MCPConnectionOptions{Secrets: secrets, Egress: mcpEgress})
	flows := service.NewOAuthFlowService(db, secrets, service.OAuthFlowOptions{Connections: conns, SecretSource: secretStore.Source})
	conns.SetOAuth(flows)
	b := service.NewBurnliteConnections(db, conns, flows, secrets, service.BurnliteConnectionsOptions{ConfiguredURL: cfg.Burnlite.URL})
	return b, func() { _ = flows.Close() }
}
