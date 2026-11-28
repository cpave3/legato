package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestNativeSecurityAttentionSurvivesExecutionEndAndStableDismissal(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "attention.db")
	db, err := store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.CreateExecution(ctx, store.Execution{ID: "execution", TaskID: "task", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, AgentKind: "claude-code", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	attention := NewAttentionService(db, nil)
	if err := attention.RegisterProducer(NewNativeSecurityAttentionAdapter(db)); err != nil {
		t.Fatal(err)
	}
	reconcile := func() []AttentionItem {
		t.Helper()
		if err := attention.Reconcile(ctx, "native_security"); err != nil {
			t.Fatal(err)
		}
		items, err := attention.List(ctx, AttentionFilter{})
		if err != nil {
			t.Fatal(err)
		}
		count, err := attention.Count(ctx)
		if err != nil || count != len(items) {
			t.Fatalf("count = %d, items = %d, %v", count, len(items), err)
		}
		return items
	}
	if items := reconcile(); len(items) != 0 {
		t.Fatalf("before rejection = %+v", items)
	}
	if _, _, err := db.InsertApproval(ctx, store.Approval{
		ID: "approval", Source: "agent_permission", SourceKey: "execution/request",
		TaskID: "task", ExecutionID: "execution", Title: "Run a command",
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordNativeSecurityIncident(ctx, "task", "execution", "generation", "permission"); err != nil {
		t.Fatal(err)
	}
	items := reconcile()
	if len(items) != 1 {
		t.Fatalf("after rejection = %+v", items)
	}
	item := items[0]
	if item.SignalKind != AttentionSignalNativeSecurityIncident || item.SourceRevision != "generation" || item.TaskID != "task" || item.Urgency != "high" || item.Navigation.Kind != "task" || item.Navigation.Target != "task" || !strings.Contains(item.Reason, "refused") {
		t.Fatalf("incident attention = %+v", item)
	}
	rail := RailView(items, config.RailConfig{})
	if len(rail) != 1 || rail[0].RailGroup != "legato" {
		t.Fatalf("rail = %+v", rail)
	}
	if _, err := db.CloseApproval(ctx, "approval", store.ApprovalClose{Status: store.ApprovalApproved, DecidedBy: "web"}); err != nil {
		t.Fatal(err)
	}
	if items := reconcile(); len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("valid approval erased the incident = %+v", items)
	}
	if err := db.TerminalizeExecution(ctx, "execution", store.ExecutionSucceeded); err != nil {
		t.Fatal(err)
	}
	if items := reconcile(); len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("after execution end = %+v", items)
	}
	if err := attention.Dismiss(ctx, item.ID, item.SourceRevision); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordNativeSecurityIncident(ctx, "task", "execution", "generation", "question"); err != nil {
		t.Fatal(err)
	}
	if items := reconcile(); len(items) != 0 {
		t.Fatalf("repeat undid dismissal = %+v", items)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.New(path)
	if err != nil {
		t.Fatal(err)
	}
	attention = NewAttentionService(db, nil)
	if err := attention.RegisterProducer(NewNativeSecurityAttentionAdapter(db)); err != nil {
		t.Fatal(err)
	}
	if items := reconcile(); len(items) != 0 {
		t.Fatalf("restart undid dismissal = %+v", items)
	}
	if err := db.RecordNativeSecurityIncident(ctx, "task", "execution", "next-generation", "control"); err != nil {
		t.Fatal(err)
	}
	items = reconcile()
	if len(items) != 1 || items[0].SourceRevision != "next-generation" || items[0].ID == item.ID {
		t.Fatalf("new generation = %+v", items)
	}
}
