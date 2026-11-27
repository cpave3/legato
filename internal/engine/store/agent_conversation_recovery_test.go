package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRecoveryKeepsSameSessionInDifferentStorageRootsSeparate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task-1")
	start := time.Now().Add(-time.Hour)
	end := start.Add(time.Minute)
	for i, root := range []string{"/account-a", "/account-b"} {
		id := []string{"exec-a", "exec-b"}[i]
		if err := s.CreateExecution(ctx, Execution{ID: id, Kind: ExecutionInteractive, Status: ExecutionLost, TaskID: "task-1", AgentKind: "claude-code", StartedAt: start, EndedAt: &end}); err != nil {
			t.Fatal(err)
		}
		ok, err := s.RecordRecoveredConversation(ctx, UnrecordedExecution{ID: id, TaskID: "task-1", Status: ExecutionLost, StartedAt: start, EndedAt: &end}, RecoveredConversation{AgentKind: "claude-code", ProviderSessionID: "shared", StorageDir: root, CWD: "/repo", StartedAt: start})
		if err != nil || !ok {
			t.Fatalf("recover %s: %v, %v", root, ok, err)
		}
	}
	rows := conversationsOf(t, s, "task-1")
	if len(rows) != 2 {
		t.Fatalf("got %d conversations, want two account-local conversations", len(rows))
	}
	for _, c := range rows {
		runs := runsOf(t, s, c.ID)
		want := "exec-a"
		if c.StorageDir == "/account-b" {
			want = "exec-b"
		}
		if len(runs) != 1 || runs[0].ExecutionID != want {
			t.Fatalf("root %s runs = %+v", c.StorageDir, runs)
		}
	}
}

func TestTaskConversationLookupRefusesAmbiguousStorageRoots(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "task-1")
	start := time.Now()
	for _, root := range []string{"/account-a", "/account-b"} {
		if _, err := s.RecordRecoveredConversation(ctx, UnrecordedExecution{ID: root, TaskID: "task-1", Status: ExecutionLost, StartedAt: start, EndedAt: &start}, RecoveredConversation{AgentKind: "claude-code", ProviderSessionID: "shared", StorageDir: root, CWD: "/repo", StartedAt: start}); err != nil {
			t.Fatal(err)
		}
		if root == "/account-a" {
			c, err := s.FindTaskConversation(ctx, "task-1", SlotMain, "claude-code", "shared", nil)
			if err != nil || c == nil || c.StorageDir != root {
				t.Fatalf("unique lookup = %+v, %v", c, err)
			}
		}
	}
	c, err := s.FindTaskConversation(ctx, "task-1", SlotMain, "claude-code", "shared", nil)
	if c != nil || err == nil || !strings.Contains(err.Error(), "ambiguous") || !errors.Is(err, ErrConflict) {
		t.Fatalf("ambiguous lookup = %+v, %v", c, err)
	}
}
