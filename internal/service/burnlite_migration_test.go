package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// migrationRun records what Migrate backed up and logged.
type migrationRun struct {
	backups []string
	logs    []string
}

func (r *migrationRun) options(t *testing.T, db *store.Store) BurnliteMigrationOptions {
	return BurnliteMigrationOptions{
		Backup: func(ctx context.Context) (string, error) {
			path := filepath.Join(t.TempDir(), fmt.Sprintf("before-%d.db", len(r.backups)))
			r.backups = append(r.backups, path)
			return path, db.BackupTo(ctx, path)
		},
		Log: func(format string, args ...any) { r.logs = append(r.logs, fmt.Sprintf(format, args...)) },
	}
}

func (r *migrationRun) logged(words ...string) bool {
	for _, line := range r.logs {
		all := true
		for _, w := range words {
			all = all && strings.Contains(line, w)
		}
		if all {
			return true
		}
	}
	return false
}

// connectLegacyBoard connects the board as Legato did before LEG-544: its own
// client and token set, sealed as integration-burnlite-refresh.
func (f burnliteLinkFixture) connectLegacyBoard(t *testing.T) {
	t.Helper()
	f.provider.mu.Lock()
	f.provider.grantScope = "read:tasks write:tasks"
	f.provider.mu.Unlock()
	connectBurnlite(t, f.provider, f.db, f.secrets)
}

// connectAgents authorizes agents' MCP connection from Settings › MCP.
func (f burnliteLinkFixture) connectAgents(t *testing.T, scope string) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.conns.Get(ctx, "burnlite"); err != nil {
		if _, err := f.conns.Add(ctx, MCPConnectionInput{Name: "burnlite", URL: f.provider.srv.URL + "/api/llm/mcp", Loopback: true}); err != nil {
			t.Fatal(err)
		}
	}
	f.provider.mu.Lock()
	f.provider.grantScope = scope
	f.provider.mu.Unlock()
	begun, err := f.flows.Begin(ctx, OAuthFlowRequest{Connection: "burnlite", Owner: "operator", Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.flows.Deliver(ctx, begun.ID, "operator", begun.ReturnKey, oauthAppResponse(mustQuery(t, begun.AuthorizeURL))); err != nil {
		t.Fatal(err)
	}
	if err := f.flows.Confirm(ctx, begun.ID, "operator"); err != nil {
		t.Fatal(err)
	}
}

func (f burnliteLinkFixture) revokedTokens() []string {
	f.provider.mu.Lock()
	defer f.provider.mu.Unlock()
	var out []string
	for _, form := range f.provider.revoked {
		out = append(out, form.Get("token"))
	}
	return out
}

func (f burnliteLinkFixture) currentRefresh() string {
	f.provider.mu.Lock()
	defer f.provider.mu.Unlock()
	return f.provider.refresh
}

// assertOneConnection checks the board and agents share one authorization
// and the board's old token set is gone.
func (f burnliteLinkFixture) assertOneConnection(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	link, err := f.links.Current(ctx)
	if err != nil || link.State != store.IntegrationConnected || link.Connection != "burnlite" || link.URL != f.provider.srv.URL {
		t.Fatalf("board = %+v, %v", link, err)
	}
	if conn, _ := f.conns.Get(ctx, "burnlite"); conn.Authorization != "authorized" || conn.SharedWith != BurnliteIntegration {
		t.Fatalf("MCP connection = %+v", conn)
	}
	for _, name := range []string{IntegrationRefreshSecret(BurnliteIntegration), IntegrationClientSecret(BurnliteIntegration)} {
		if v, _ := f.secrets.Read(ctx, name); v != "" {
			t.Errorf("%s is still kept", name)
		}
	}
	board, err := f.links.Tokens().Token(ctx, false)
	agents, err2 := f.flows.AccessToken(ctx, "burnlite")
	if err != nil || err2 != nil || board != agents {
		t.Fatalf("board token %q (%v), agents' %q (%v)", board, err, agents, err2)
	}
}

func TestMigratingKeepsTheWiderMCPAuthorizationAndRevokesTheBoards(t *testing.T) {
	f := newBurnliteLinkFixture(t, nil)
	ctx := context.Background()
	f.connectLegacyBoard(t)
	legacy, _ := f.secrets.Read(ctx, IntegrationRefreshSecret(BurnliteIntegration))
	f.connectAgents(t, BurnliteScope)
	run := &migrationRun{}

	got, err := f.links.Migrate(ctx, run.options(t, f.db))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Changed || got.Kept != "mcp" || got.Revoked != "revoked" || len(run.backups) != 1 {
		t.Fatalf("migration = %+v, backups %v", got, run.backups)
	}
	if revoked := f.revokedTokens(); len(revoked) != 1 || revoked[0] != legacy {
		t.Errorf("revoked %v, want the board's %q", revoked, legacy)
	}
	if !run.logged("kept", "burnlite") || !run.logged("revoked") {
		t.Errorf("logs = %q", run.logs)
	}
	f.assertOneConnection(t)

	again, err := f.links.Migrate(ctx, run.options(t, f.db))
	if err != nil || again.Changed || len(run.backups) != 1 {
		t.Fatalf("a second run = %+v, %v, backups %v", again, err, run.backups)
	}
}

func TestMigratingKeepsTheNewerBoardAuthorizationAndRevokesAgents(t *testing.T) {
	f := newBurnliteLinkFixture(t, nil)
	ctx := context.Background()
	// Agents' connection was authorized a day before the board's, with no
	// wider scope: the board's is newer and is kept.
	f.clock.Add(-24 * time.Hour)
	f.connectAgents(t, "read:tasks")
	agentsRefresh := f.currentRefresh()
	f.clock.Add(24 * time.Hour)
	f.connectLegacyBoard(t)
	run := &migrationRun{}

	got, err := f.links.Migrate(ctx, run.options(t, f.db))
	if err != nil {
		t.Fatal(err)
	}
	if got.Kept != "integration" || got.Revoked != "revoked" || len(run.backups) != 1 {
		t.Fatalf("migration = %+v", got)
	}
	if revoked := f.revokedTokens(); len(revoked) != 1 || revoked[0] != agentsRefresh {
		t.Errorf("revoked %v, want agents' %q", revoked, agentsRefresh)
	}
	if !run.logged("kept", "board") {
		t.Errorf("logs = %q", run.logs)
	}
	f.assertOneConnection(t)
}

func TestMigratingABoardWithNoMCPConnectionMakesOne(t *testing.T) {
	f := newBurnliteLinkFixture(t, nil)
	ctx := context.Background()
	f.connectLegacyBoard(t)
	run := &migrationRun{}
	got, err := f.links.Migrate(ctx, run.options(t, f.db))
	if err != nil || got.Kept != "integration" || got.Revoked != "" || len(run.backups) != 1 {
		t.Fatalf("migration = %+v, %v", got, err)
	}
	f.assertOneConnection(t)
	if conn, _ := f.conns.Get(ctx, "burnlite"); len(conn.Rules) != 0 {
		t.Errorf("the new connection has rules: %+v", conn.Rules)
	}
	if len(f.revokedTokens()) != 0 {
		t.Errorf("revoked %v", f.revokedTokens())
	}
}

func TestMigratingAnMCPConnectionWithNoBoardConnectsTheBoard(t *testing.T) {
	f := newBurnliteLinkFixture(t, nil)
	ctx := context.Background()
	f.connectAgents(t, BurnliteScope)
	run := &migrationRun{}
	got, err := f.links.Migrate(ctx, run.options(t, f.db))
	if err != nil || got.Kept != "mcp" || len(run.backups) != 1 {
		t.Fatalf("migration = %+v, %v", got, err)
	}
	f.assertOneConnection(t)
	if again, _ := f.links.Migrate(ctx, run.options(t, f.db)); again.Changed || len(run.backups) != 1 {
		t.Fatalf("a second run = %+v", again)
	}
}

func TestMigratingNothingChangesNothing(t *testing.T) {
	f := newBurnliteLinkFixture(t, nil)
	run := &migrationRun{}
	got, err := f.links.Migrate(context.Background(), run.options(t, f.db))
	if err != nil || got.Changed || len(run.backups) != 0 || len(run.logs) != 0 {
		t.Fatalf("migration = %+v, %v, %v", got, err, run)
	}
}

// fakeBurnliteTools serves get_task and close_task on the fake Burnlite's
// MCP endpoint, to whoever holds its current access token.
type fakeBurnliteTools struct {
	closed []string
}

func (f burnliteLinkFixture) serveTools(t *testing.T) *fakeBurnliteTools {
	t.Helper()
	tools := &fakeBurnliteTools{}
	srv := mcp.NewServer(&mcp.Implementation{Name: "burnlite", Version: "test"}, nil)
	result := func(v any) *mcp.CallToolResult {
		b, _ := json.Marshal(v)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		return &mcp.CallToolResult{StructuredContent: out, Content: []mcp.Content{&mcp.TextContent{Text: "ok"}}}
	}
	srv.AddTool(&mcp.Tool{Name: "get_task", InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return result(map[string]any{"task": map[string]any{"key": "LEG-1", "url": f.provider.srv.URL + "/LEG-1", "taskName": "Join the connections",
			"kind": "work", "state": "open", "updatedAt": "2026-11-24T10:00:00Z"}}), nil
	})
	srv.AddTool(&mcp.Tool{Name: "close_task", InputSchema: map[string]any{"type": "object"}}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args map[string]any
		_ = json.Unmarshal(req.Params.Arguments, &args)
		f.provider.mu.Lock()
		tools.closed = append(tools.closed, fmt.Sprint(args["taskId"]))
		f.provider.mu.Unlock()
		return result(map[string]any{"ok": true}), nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	f.provider.mu.Lock()
	f.provider.mcp = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.provider.mu.Lock()
		want := fmt.Sprintf("Bearer access-%d", f.provider.issued)
		f.provider.mu.Unlock()
		if r.Header.Get("Authorization") != want {
			w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	})
	f.provider.mu.Unlock()
	return tools
}

// The live instance on 2026-11-24: the board's connection from 2026-11-05,
// reconnected with write:tasks, and agents' MCP authorization from 13:37 with
// all three scopes. Agents' is kept, the board's revoked, and the board can
// import and close tickets straight after.
func TestMigratingTheLiveInstancesPair(t *testing.T) {
	f := newBurnliteLinkFixture(t, nil)
	ctx := context.Background()
	f.connectLegacyBoard(t)
	legacy, _ := f.secrets.Read(ctx, IntegrationRefreshSecret(BurnliteIntegration))
	f.connectAgents(t, BurnliteScope)
	run := &migrationRun{}
	got, err := f.links.Migrate(ctx, run.options(t, f.db))
	if err != nil || got.Kept != "mcp" || len(run.backups) != 1 {
		t.Fatalf("migration = %+v, %v", got, err)
	}
	if info, err := os.Stat(run.backups[0]); err != nil || info.Size() == 0 {
		t.Fatalf("backup: %v", err)
	}
	if revoked := f.revokedTokens(); len(revoked) != 1 || revoked[0] != legacy {
		t.Errorf("revoked %v", revoked)
	}
	f.assertOneConnection(t)

	tools := f.serveTools(t)
	for i, c := range []string{"Ready", "Backlog", "Doing", "Done"} {
		if err := f.db.CreateColumnMapping(ctx, store.ColumnMapping{ColumnName: c, RemoteStatuses: "[]", SortOrder: i}); err != nil {
			t.Fatal(err)
		}
	}
	source := NewBurnliteSource(f.links, BurnliteSourceOptions{Interval: time.Minute,
		Columns: BurnliteColumns{Ready: "Ready", Blocked: "Backlog", Claimed: "Doing", Closed: "Done"}})
	card, err := NewTicketSync(f.db, events.New(), nil, source).ImportRemoteTask(ctx, BurnliteIntegration, "LEG-1", ImportRemoteTaskOptions{})
	if err != nil || card.Title != "Join the connections" {
		t.Fatalf("import after migrating: %+v, %v", card, err)
	}
	if err := (ConnectedBurnlite{Connection: f.links}).CloseTask(ctx, "LEG-1", "completed", "Merged"); err != nil {
		t.Fatalf("close after migrating: %v", err)
	}
	if len(tools.closed) != 1 || tools.closed[0] != "LEG-1" {
		t.Fatalf("closed %v", tools.closed)
	}
}
