package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runningExecution(t *testing.T, s *Store, id, taskID string) {
	t.Helper()
	if err := s.CreateExecution(context.Background(), Execution{ID: id, Kind: ExecutionInteractive, Status: ExecutionRunning, TaskID: taskID, ExecutionTaskID: taskID, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}

func hashOf(placeholder string) string {
	sum := sha256.Sum256([]byte(placeholder))
	return hex.EncodeToString(sum[:])
}

func TestAPlaceholderResolvesOnlyWhileItsExecutionRuns(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	runningExecution(t, s, "ex-1", "t1")
	placeholder := "lgk_EXAMPLEPLACEHOLDER"
	if err := s.CreateCredentialPlaceholder(ctx, hashOf(placeholder), "ex-1", "t1", "elevenlabs"); err != nil {
		t.Fatal(err)
	}

	got, err := s.ResolveCredentialPlaceholder(ctx, hashOf(placeholder))
	if err != nil || got.ExecutionID != "ex-1" || got.TaskID != "t1" || got.Credential != "elevenlabs" {
		t.Fatalf("resolve = %+v, %v", got, err)
	}
	if _, err := s.ResolveCredentialPlaceholder(ctx, hashOf("lgk_other")); err != ErrNotFound {
		t.Fatalf("unknown placeholder err = %v, want ErrNotFound", err)
	}

	if err := s.TerminalizeExecution(ctx, "ex-1", ExecutionSucceeded); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveCredentialPlaceholder(ctx, hashOf(placeholder)); err != ErrNotFound {
		t.Fatalf("after the execution ended err = %v, want ErrNotFound", err)
	}
}

func TestRevokedPlaceholdersResolveToNothing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	runningExecution(t, s, "ex-1", "t1")
	_ = s.CreateCredentialPlaceholder(ctx, hashOf("a"), "ex-1", "t1", "elevenlabs")
	if err := s.RevokeCredentialPlaceholders(ctx, "ex-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveCredentialPlaceholder(ctx, hashOf("a")); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestTheStoreHoldsNoPlaceholderInPlainForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legato.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	runningExecution(t, s, "ex-1", "t1")
	placeholder := "lgk_PLAINFORMMUSTNOTAPPEAR"
	_ = s.CreateCredentialPlaceholder(ctx, hashOf(placeholder), "ex-1", "t1", "elevenlabs")
	var rows []string
	if err := s.db.SelectContext(ctx, &rows, "SELECT hash || execution_id || credential FROM credential_placeholders"); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if strings.Contains(row, placeholder) {
			t.Fatalf("row %q holds the placeholder", row)
		}
	}
}

func TestMeterTotalsSurviveAReopenedStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legato.db")
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, amount := range []int64{1200, 800} {
		if err := s.AddCredentialMeter(ctx, "elevenlabs", "characters", amount, now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	_ = s.AddCredentialMeter(ctx, "elevenlabs", "characters", 5000, now.Add(-30*time.Hour)) // outside the day
	_ = s.AddCredentialMeter(ctx, "other", "characters", 7, now)
	s.Close()

	s, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	total, err := s.CredentialMeterTotal(ctx, "elevenlabs", "characters", now.Add(-24*time.Hour))
	if err != nil || total != 2000 {
		t.Fatalf("total = %d, %v; want 2000", total, err)
	}
}

func TestRunningExecutionsAreListedWithTheirTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	runningExecution(t, s, "ex-1", "t1")
	runningExecution(t, s, "ex-2", "t2")
	_ = s.TerminalizeExecution(ctx, "ex-2", ExecutionCancelled)
	got, err := s.ListRunningExecutions(ctx)
	if err != nil || len(got) != 1 || got[0].ID != "ex-1" || got[0].ExecutionTaskID != "t1" {
		t.Fatalf("running = %+v, %v", got, err)
	}
}
