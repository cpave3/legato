package store

import (
	"context"
	"errors"
	"testing"
)

func newMCPConnection(name string) MCPConnection {
	return MCPConnection{Name: name, URL: "https://mcp.example.test/mcp", Auth: "oauth", InstanceID: "srv-1"}
}

func pendingAttempt(t *testing.T, s *Store, id, connection string, revision int64) {
	t.Helper()
	_, err := s.BeginOAuthAttempt(context.Background(), OAuthAttempt{ID: id, Connection: connection, Owner: "owner", InstanceID: "srv-1", StateHash: "state-" + id, ReceiverHash: "r", ExpiresAt: 1000, ConnectionRevision: revision}, 1)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMCPConnectionsAreNamedAndRevisioned(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	created, err := s.CreateMCPConnection(ctx, newMCPConnection("notion"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if created.Revision != 1 || !created.Enabled || created.CreatedAt != 5 {
		t.Fatalf("created %+v", created)
	}
	if _, err := s.CreateMCPConnection(ctx, newMCPConnection("notion"), 6); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name: %v", err)
	}
	if _, err := s.CreateMCPConnection(ctx, newMCPConnection("github"), 6); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListMCPConnections(ctx)
	if err != nil || len(all) != 2 || all[0].Name != "github" || all[1].Name != "notion" {
		t.Fatalf("list %+v %v", all, err)
	}
	if _, err := s.GetMCPConnection(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestChangingAnMCPResourceFencesAttemptsAndDropsAuthorization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c, _ := s.CreateMCPConnection(ctx, newMCPConnection("notion"), 1)
	pendingAttempt(t, s, "authorized", "notion", c.Revision)
	if err := s.ClaimOAuthAttempt(ctx, "authorized", 2); err != nil {
		t.Fatal(err)
	}
	if err := s.StageOAuthAttempt(ctx, "authorized", 2); err != nil {
		t.Fatal(err)
	}
	if err := s.ActivateOAuthAttempt(ctx, "authorized", "owner", 2); err != nil {
		t.Fatal(err)
	}
	pendingAttempt(t, s, "pending", "notion", c.Revision)

	stale := c
	stale.URL = "https://other.example.test/mcp"
	stale.Revision = 99
	if _, _, err := s.UpdateMCPConnection(ctx, stale, 3); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	c.URL = "https://other.example.test/mcp"
	updated, fenced, err := s.UpdateMCPConnection(ctx, c, 3)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || len(fenced) != 2 {
		t.Fatalf("updated %+v, fenced %d", updated, len(fenced))
	}
	if _, err := s.ActiveOAuthAttempt(ctx, "notion"); !errors.Is(err, ErrNotFound) {
		t.Fatal("authorization for the old resource survived")
	}
	if err := s.ClaimOAuthAttempt(ctx, "pending", 4); !errors.Is(err, ErrConflict) {
		t.Fatal("pending attempt for the old resource could still exchange")
	}
	// An attempt started against the old revision cannot be recorded afterwards.
	_, err = s.BeginOAuthAttempt(ctx, OAuthAttempt{ID: "late", Connection: "notion", Owner: "owner", InstanceID: "srv-1", StateHash: "late", ReceiverHash: "r", ExpiresAt: 1000, ConnectionRevision: updated.Revision - 1}, 4)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("late attempt for old revision: %v", err)
	}
}

func TestDisablingAnMCPConnectionFencesUnfinishedAttemptsButKeepsAuthorization(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c, _ := s.CreateMCPConnection(ctx, newMCPConnection("notion"), 1)
	pendingAttempt(t, s, "authorized", "notion", c.Revision)
	_ = s.ClaimOAuthAttempt(ctx, "authorized", 2)
	_ = s.StageOAuthAttempt(ctx, "authorized", 2)
	if err := s.ActivateOAuthAttempt(ctx, "authorized", "owner", 2); err != nil {
		t.Fatal(err)
	}
	pendingAttempt(t, s, "staged", "notion", c.Revision)
	_ = s.ClaimOAuthAttempt(ctx, "staged", 2)

	c.Enabled = false
	disabled, _, err := s.UpdateMCPConnection(ctx, c, 3)
	if err != nil || disabled.Enabled || disabled.Revision != 2 {
		t.Fatalf("disable: %+v %v", disabled, err)
	}
	if err := s.StageOAuthAttempt(ctx, "staged", 4); !errors.Is(err, ErrConflict) {
		t.Fatal("an exchange in flight could still stage credentials on a disabled connection")
	}
	if _, err := s.ActiveOAuthAttempt(ctx, "notion"); err != nil {
		t.Fatal("disabling discarded the operator's authorization")
	}
	_, err = s.BeginOAuthAttempt(ctx, OAuthAttempt{ID: "while-off", Connection: "notion", Owner: "owner", InstanceID: "srv-1", StateHash: "off", ReceiverHash: "r", ExpiresAt: 1000, ConnectionRevision: disabled.Revision}, 4)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("attempt on a disabled connection: %v", err)
	}
}

func TestRemovingAnMCPConnectionFencesEverything(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c, _ := s.CreateMCPConnection(ctx, newMCPConnection("notion"), 1)
	pendingAttempt(t, s, "pending", "notion", c.Revision)
	fenced, err := s.DeleteMCPConnection(ctx, "notion", 2)
	if err != nil || len(fenced) != 1 {
		t.Fatalf("delete: %d %v", len(fenced), err)
	}
	if err := s.ClaimOAuthAttempt(ctx, "pending", 3); !errors.Is(err, ErrConflict) {
		t.Fatal("attempt survived removal")
	}
	if _, err := s.DeleteMCPConnection(ctx, "notion", 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	// A new connection of the same name starts after the old generation.
	again, _ := s.CreateMCPConnection(ctx, newMCPConnection("notion"), 4)
	if again.Revision <= c.Revision {
		t.Fatalf("recreated connection reused revision %d", again.Revision)
	}
}

func TestMCPContactIsRecordedOnlyForTheCurrentRevision(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c, _ := s.CreateMCPConnection(ctx, newMCPConnection("notion"), 1)
	if err := s.RecordMCPContact(ctx, "notion", c.Revision, "", 5); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetMCPConnection(ctx, "notion")
	if got.LastContactAt != 5 || got.LastError != "" {
		t.Fatalf("contact %+v", got)
	}
	c.URL = "https://other.example.test/mcp"
	if _, _, err := s.UpdateMCPConnection(ctx, c, 6); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordMCPContact(ctx, "notion", c.Revision, "unreachable", 7); !errors.Is(err, ErrConflict) {
		t.Fatal("a result from the old resource was recorded")
	}
	got, _ = s.GetMCPConnection(ctx, "notion")
	if got.LastContactAt != 0 || got.LastError != "" {
		t.Fatalf("contact state of the old resource survived a resource change: %+v", got)
	}
}

func TestALocalMCPServerKeepsItsCommandAndAChangeToItForgetsItsTools(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c, err := s.CreateMCPConnection(ctx, MCPConnection{Name: "design", Kind: "stdio", Auth: "none", InstanceID: "srv-1",
		Command: "/usr/bin/node", ArgsJSON: `["tools/design/mcp.mjs"]`, Dir: "/home/me/repo", EnvJSON: `{"PATH":"/opt/node/bin"}`}, 1)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMCPConnection(ctx, "design")
	if err != nil || got.Kind != "stdio" || got.Command != "/usr/bin/node" || got.ArgsJSON != `["tools/design/mcp.mjs"]` || got.Dir != "/home/me/repo" || got.EnvJSON != `{"PATH":"/opt/node/bin"}` {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := s.RecordMCPTools(ctx, "design", map[string]string{"render": "h1"}); err != nil {
		t.Fatal(err)
	}
	c.ArgsJSON = `["tools/design/other.mjs"]`
	if _, _, err := s.UpdateMCPConnection(ctx, c, 2); err != nil {
		t.Fatal(err)
	}
	// The tools were another program's: none is "changed" from before.
	changed, err := s.RecordMCPTools(ctx, "design", map[string]string{"render": "h2"})
	if err != nil || changed {
		t.Fatalf("changed = %v, %v; a new command's tools are not a change of the old", changed, err)
	}
	// An HTTP connection is kind http unless it says otherwise.
	h, err := s.CreateMCPConnection(ctx, newMCPConnection("notion"), 3)
	if err != nil || h.Kind != "http" {
		t.Fatalf("kind %q, %v", h.Kind, err)
	}
}
