package store

import (
	"context"
	"errors"
	"testing"
)

func TestIntegrationTokensAreFoundByHashUntilRevoked(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	tok := IntegrationToken{ID: "it-1", Name: "argus", Scopes: []string{"owner:read", "delegate"}, TokenHash: "h1"}
	if err := s.InsertIntegrationToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	got, err := s.IntegrationTokenByHash(ctx, "h1")
	if err != nil || got.Name != "argus" || len(got.Scopes) != 2 || got.Scopes[1] != "delegate" {
		t.Fatalf("by hash = %+v, %v", got, err)
	}
	if err := s.RevokeIntegrationToken(ctx, "argus"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IntegrationTokenByHash(ctx, "h1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked token err = %v", err)
	}
	// The name is free again once revoked.
	if err := s.InsertIntegrationToken(ctx, IntegrationToken{ID: "it-2", Name: "argus", Scopes: []string{"delegate"}, TokenHash: "h2"}); err != nil {
		t.Fatalf("reuse name: %v", err)
	}
	all, err := s.ListIntegrationTokens(ctx)
	if err != nil || len(all) != 2 || all[0].RevokedAt == nil || all[1].RevokedAt != nil {
		t.Fatalf("list = %+v, %v", all, err)
	}
}

func TestALiveIntegrationTokenNameIsUnique(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.InsertIntegrationToken(ctx, IntegrationToken{ID: "it-1", Name: "argus", Scopes: []string{"delegate"}, TokenHash: "h1"})
	err := s.InsertIntegrationToken(ctx, IntegrationToken{ID: "it-2", Name: "argus", Scopes: []string{"delegate"}, TokenHash: "h2"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name err = %v", err)
	}
	if err := s.RevokeIntegrationToken(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke unknown err = %v", err)
	}
}

func TestTasksForARepositoryIgnoreCaseAndArchivedTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []string{"t1", "t2", "t3", "t4"} {
		createTestTask(t, s, id)
	}
	_ = s.SetCodeContext(ctx, "t1", &CodeContext{Repo: "acme/widget", Branch: "a"})
	_ = s.SetCodeContext(ctx, "t2", &CodeContext{Repo: "acme/other", Branch: "a"})
	raw, _ := MarshalPRMeta(&PRMeta{Repo: "Acme/Widget", PRNumber: 3})
	_ = s.UpdatePRMeta(ctx, "t3", raw)
	_ = s.SetCodeContext(ctx, "t4", &CodeContext{Repo: "acme/widget"})
	_ = s.ArchiveTask(ctx, "t4")
	tasks, err := s.ListTasksForRepository(ctx, "ACME/widget")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	if len(ids) != 2 || ids[0] != "t1" || ids[1] != "t3" {
		t.Fatalf("ids = %v", ids)
	}
}

func TestTheLatestAgentSessionOfATaskIncludesEndedOnes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "t1")
	if got, err := s.LatestAgentSessionForTask(ctx, "t1"); err != nil || got != nil {
		t.Fatalf("none = %+v, %v", got, err)
	}
	_ = s.InsertAgentSession(ctx, AgentSession{TaskID: "t1", AgentKind: "claude-code", TmuxSession: "a", Status: "running", Role: "conductor"})
	_ = s.UpdateAgentSessionStatus(ctx, "t1", "dead")
	got, err := s.LatestAgentSessionForTask(ctx, "t1")
	if err != nil || got == nil || got.Role != "conductor" || got.Status != "dead" {
		t.Fatalf("latest = %+v, %v", got, err)
	}
}
