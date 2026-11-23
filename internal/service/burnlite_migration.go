package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/oauthclient"
	"github.com/cpave3/legato/internal/engine/store"
)

// burnliteMigrationLease bounds how long a migration holds the others off;
// its network calls are bounded well inside it.
const burnliteMigrationLease = 2 * time.Minute

// legacyBurnliteScope is the scope a board connection from before LEG-544
// counts as having: it recorded none, so it counts as the narrowest it can be.
const legacyBurnliteScope = "read:tasks"

// BurnliteMigrationOptions are what joining the two connections may do
// besides the joining.
type BurnliteMigrationOptions struct {
	// Backup copies the database before the first change and says where.
	Backup func(ctx context.Context) (string, error)
	// Log records what was kept and what was revoked; log.Printf when nil.
	Log func(format string, args ...any)
}

// BurnliteMigration is what joining the two connections did.
type BurnliteMigration struct {
	Changed bool   `json:"changed"`
	Backup  string `json:"backup,omitempty"`
	// Kept is whose authorization the shared connection holds now: "mcp"
	// (agents' MCP connection's) or "integration" (the board's own).
	Kept string `json:"kept,omitempty"`
	// Revoked is what Burnlite did with the authorization dropped:
	// revoked, not_supported, failed, or "" when none was dropped.
	Revoked string `json:"revoked,omitempty"`
}

// scopeCovers says whether scope a grants everything b does.
func scopeCovers(a, b string) bool {
	have := map[string]bool{}
	for _, s := range strings.Fields(a) {
		have[s] = true
	}
	for _, s := range strings.Fields(b) {
		if !have[s] {
			return false
		}
	}
	return true
}

// Migrate joins a board connection from before LEG-544, which kept its own
// client and tokens (integration-burnlite-refresh), with agents' MCP
// connection to the same Burnlite. It keeps the authorization whose scope
// covers the other's (on a tie, the newer), revokes the other at Burnlite,
// and leaves one: the MCP connection's authorization, which the board uses.
// An MCP connection to Burnlite with no board connects the board. It is
// idempotent, backs up the database before its first change, and changes
// nothing when there is nothing to join.
func (b *BurnliteConnections) Migrate(ctx context.Context, opts BurnliteMigrationOptions) (BurnliteMigration, error) {
	logf := opts.Log
	if logf == nil {
		logf = log.Printf
	}
	// One process at a time, and everything below read under the lease:
	// another may have joined them while this one waited.
	holder := "migrate-" + oauthRandom()[:16]
	for {
		now := time.Now()
		err := b.db.ClaimBurnliteMigration(ctx, holder, now.Unix(), now.Add(burnliteMigrationLease).Unix())
		if err == nil {
			break
		}
		if !errors.Is(err, store.ErrConflict) {
			return BurnliteMigration{}, err
		}
		select {
		case <-ctx.Done():
			return BurnliteMigration{}, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer func() { _ = b.db.ReleaseBurnliteMigration(context.WithoutCancel(ctx), holder) }()
	var result BurnliteMigration
	change := func() error {
		if result.Changed {
			return nil
		}
		result.Changed = true
		if opts.Backup == nil {
			return nil
		}
		path, err := opts.Backup(ctx)
		if err != nil {
			return fmt.Errorf("backing up the database before joining Burnlite's connections: %w", err)
		}
		result.Backup = path
		logf("burnlite: backed up the database to %s before joining the board's and agents' connections (LEG-544)", path)
		return nil
	}

	legacy, _ := b.secrets.Read(ctx, IntegrationRefreshSecret(BurnliteIntegration))
	in, err := b.db.GetIntegration(ctx, BurnliteIntegration)
	if errors.Is(err, store.ErrNotFound) {
		if legacy != "" {
			if err := change(); err != nil {
				return result, err
			}
			b.forgetLegacy(ctx)
			logf("burnlite: removed board tokens that no connection used")
		}
		c, ok, err := b.discover(ctx)
		if err != nil || !ok {
			return result, err
		}
		if err := change(); err != nil {
			return result, err
		}
		if _, err := b.Current(ctx); err != nil {
			return result, err
		}
		result.Kept = "mcp"
		logf("burnlite: kept MCP connection %s's authorization; the board uses it too", c.Name)
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if legacy == "" {
		return result, nil
	}
	if err := change(); err != nil {
		return result, err
	}
	c, linked, err := b.linked(ctx, in)
	if err != nil {
		return result, err
	}
	agents := linked && c.Authorization == "authorized"
	if in.State != store.IntegrationConnected {
		b.forgetLegacy(ctx)
		logf("burnlite: dropped the board's own tokens, which Burnlite had refused")
		if agents {
			result.Kept = "mcp"
			logf("burnlite: kept MCP connection %s's authorization; the board uses it too", c.Name)
		}
		return result, nil
	}

	keepBoard := !agents
	agentsAuth := ""
	if agents {
		scope, at, id, err := b.flows.Authorization(ctx, c.Name)
		agentsAuth = id
		if err != nil {
			return result, err
		}
		agentsCover, boardCovers := scopeCovers(scope, legacyBurnliteScope), scopeCovers(legacyBurnliteScope, scope)
		switch {
		case agentsCover && !boardCovers:
			keepBoard = false
		case boardCovers && !agentsCover:
			keepBoard = true
		default:
			keepBoard = burnliteTime(in.ConnectedAt).Unix() > at
		}
	}

	md := b.legacyMetadata(ctx, in)
	if !keepBoard {
		result.Kept, result.Revoked = "mcp", b.revokeLegacy(ctx, in, md, legacy)
		b.forgetLegacy(ctx)
		logf("burnlite: kept MCP connection %s's authorization, which covers the board's; the board uses it too", c.Name)
		logf("burnlite: the board's own tokens: %s at Burnlite, and forgotten", result.Revoked)
		return result, nil
	}
	if agents {
		if result.Revoked, err = b.flows.RevokeAuthorization(ctx, c.Name, agentsAuth); err != nil {
			return result, err
		}
		logf("burnlite: MCP connection %s's older tokens: %s at Burnlite", c.Name, result.Revoked)
	}
	secret, _ := b.secrets.Read(ctx, IntegrationClientSecret(BurnliteIntegration))
	link, err := b.Adopt(ctx, in.URL, AdoptedAuthorization{Metadata: md, ClientID: in.ClientID, ClientSecret: secret,
		Scope: legacyBurnliteScope, Token: &oauthclient.Token{RefreshToken: legacy}})
	if err != nil {
		return result, fmt.Errorf("moving the board's Burnlite tokens to its MCP connection: %w", err)
	}
	result.Kept = "integration"
	logf("burnlite: kept the board's authorization; MCP connection %s holds it now, for agents too", link.Connection)
	return result, nil
}

// legacyMetadata is the board's Burnlite server metadata: discovered again
// (for its revocation endpoint), else what the board recorded.
func (b *BurnliteConnections) legacyMetadata(ctx context.Context, in *store.Integration) oauthclient.Metadata {
	md := oauthclient.Metadata{Issuer: in.Issuer, TokenEndpoint: in.TokenEndpoint, Resource: in.Resource}
	if md.Resource == "" {
		md.Resource = strings.TrimRight(in.URL, "/") + burnliteMCPPath
	}
	client, err := b.ConnectClient(ctx, in.URL)
	if err != nil {
		return md
	}
	work, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	found, err := oauthclient.Discover(work, client, in.URL)
	if err != nil || found.TokenEndpoint != in.TokenEndpoint {
		return md
	}
	return *found
}

// revokeLegacy asks Burnlite to revoke the board's own refresh token.
func (b *BurnliteConnections) revokeLegacy(ctx context.Context, in *store.Integration, md oauthclient.Metadata, refresh string) string {
	client, err := b.ConnectClient(ctx, in.URL)
	if err != nil {
		return "failed"
	}
	secret, _ := b.secrets.Read(ctx, IntegrationClientSecret(BurnliteIntegration))
	work, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	switch err := oauthclient.Revoke(work, client, &md, in.ClientID, refresh, "refresh_token", oauthclient.WithClientSecret(secret)); {
	case err == nil:
		return "revoked"
	case errors.Is(err, oauthclient.ErrRevocationUnsupported):
		return "not_supported"
	default:
		return "failed"
	}
}
