package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestHistoryRestoresKnownAccountWhenProviderIDsRepeatAcrossRoots(t *testing.T) {
	f := newRestartFixture(t)
	for _, key := range namedAccountAuthVars {
		t.Setenv(key, "")
	}
	ctx := context.Background()
	createTask(t, f.store, "task-1")
	cwd := t.TempDir()
	svc := f.svc.(*agentService)
	for _, name := range []string{"work", "review"} {
		root := t.TempDir()
		data := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"synthetic-only","expiresAt":%d}}`, time.Now().Add(time.Hour).UnixMilli())
		if err := os.WriteFile(filepath.Join(root, ".credentials.json"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		svc.claudeAccounts = append(svc.claudeAccounts, config.ClaudeAccount{Name: name, ConfigDir: root})
		if err := svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{Account: name, WorkingDir: cwd, SessionPolicy: "new"}); err != nil {
			t.Fatal(err)
		}
		f.reportAs(t, "task-1", cwd, "same-provider-id")
		if err := svc.KillAgent(ctx, "task-1"); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := f.store.ListTaskConversations(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want two account conversations: %+v", entries)
	}
	for _, entry := range entries {
		before := len(f.tmux.shellCommandsFor("legato-task-1"))
		if err := restoreErr(f.history(), ctx, entry.ID, RestoreIntoTask); err != nil {
			t.Fatalf("restore known storage %s: %v", entry.StorageDir, err)
		}
		session, err := f.store.GetAgentSessionByTaskID(ctx, "task-1")
		if err != nil {
			t.Fatal(err)
		}
		identity, err := f.store.ExecutionAccount(ctx, session.ExecutionID)
		if err != nil || identity == nil || identity.ConfigDir != entry.StorageDir {
			t.Fatalf("restored wrong account: %+v, %v", identity, err)
		}
		commands := f.tmux.shellCommandsFor("legato-task-1")
		if len(commands) != before+1 || !strings.Contains(commands[before], "CLAUDE_CONFIG_DIR="+shellWord(entry.StorageDir)) || !strings.Contains(commands[before], "--resume same-provider-id") {
			t.Fatalf("known-identity restore did not launch its account/session: %q", commands)
		}
		if err := svc.KillAgent(ctx, "task-1"); err != nil {
			t.Fatal(err)
		}
	}
	before := len(f.tmux.shellCommandsFor("legato-task-1"))
	err = svc.SpawnAgent(ctx, "task-1", 0, 0, AgentSpawnOptions{AgentKind: "claude-code", SessionPolicy: "resume", ResumeSessionID: "same-provider-id"})
	if !errors.Is(err, store.ErrConflict) || !strings.Contains(err.Error(), "ambiguous") || len(f.tmux.shellCommandsFor("legato-task-1")) != before {
		t.Fatalf("unqualified ambiguity did not refuse before launch: %v", err)
	}
}
