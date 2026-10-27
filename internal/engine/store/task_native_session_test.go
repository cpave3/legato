package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func nativeSessionOwner(t *testing.T, s *Store, taskID, kind, executionID string, alter func(*AgentSession, *Execution)) int {
	t.Helper()
	a := AgentSession{TaskID: taskID, TmuxSession: "native-" + executionID, AgentKind: kind, Status: "running"}
	e := executionAt(executionID, taskID, time.Now().UTC())
	e.AgentKind = kind
	if alter != nil {
		alter(&a, &e)
	}
	id, err := s.InsertAgentSessionReturningID(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	e.AgentSessionID = &id
	if err := s.CreateExecution(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTaskNativeSessionPersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "native.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	createTestTask(t, s, "native-task")
	createTestTask(t, s, "other-task")
	if got, err := s.GetTaskNativeSession(ctx, "native-task", "codex"); err != nil || got != nil {
		t.Fatalf("missing record = %#v, %v", got, err)
	}
	if got, err := s.ListTaskNativeSessions(ctx, "native-task"); err != nil || len(got) != 0 {
		t.Fatalf("empty list = %#v, %v", got, err)
	}

	claude := nativeSessionOwner(t, s, "native-task", "claude-code", "claude-1", nil)
	record := func(id int, execution, kind, provider, cwd string, want bool) {
		t.Helper()
		changed, err := s.RecordTaskNativeSession(ctx, "native-task", id, execution, kind, provider, cwd, "/storage")
		if err != nil || changed != want {
			t.Fatalf("record %s = %v, %v; want changed %v", provider, changed, err, want)
		}
	}
	record(claude, "claude-1", "claude-code", "conversation-1", "/repo", true)
	first, err := s.GetTaskNativeSession(ctx, "native-task", "claude-code")
	if err != nil || first == nil || first.UpdatedAt.IsZero() {
		t.Fatalf("saved record = %#v, %v", first, err)
	}
	record(claude, "claude-1", "claude-code", "conversation-1", "/repo", false)
	same, err := s.GetTaskNativeSession(ctx, "native-task", "claude-code")
	if err != nil || *same != *first {
		t.Fatalf("idempotent record = %#v, %v; want %#v", same, err, first)
	}
	record(claude, "claude-1", "claude-code", "conversation-2", "/repo", true)
	record(claude, "claude-1", "claude-code", "conversation-2", "/worktree", true)
	if err := s.UpdateChimeraDiscoveryForSession(ctx, claude, "native-task", "http://native"); err != nil {
		t.Fatal(err)
	}
	if cleared, err := s.ClearChimeraDiscoveryForSession(ctx, claude, "native-task", "http://native"); err != nil || !cleared {
		t.Fatalf("clear live binding = %v, %v", cleared, err)
	}
	if err := s.FinishAgentSession(ctx, claude); err != nil {
		t.Fatal(err)
	}
	codex := nativeSessionOwner(t, s, "native-task", "codex", "codex-1", nil)
	record(codex, "codex-1", "codex", "thread-1", "/repo", true)
	if err := s.DeleteAgentSession(ctx, claude); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ListTaskNativeSessions(ctx, "native-task")
	if err != nil || len(got) != 2 {
		t.Fatalf("durable provider records = %#v, %v", got, err)
	}
	if got[0].TaskID != "native-task" || got[0].AgentKind != "claude-code" || got[0].ProviderSessionID != "conversation-2" || got[0].CWD != "/worktree" || got[0].AgentSessionID != claude || got[0].ExecutionID != "claude-1" {
		t.Fatalf("Claude record = %#v", got[0])
	}
	if got[1].AgentKind != "codex" || got[1].ProviderSessionID != "thread-1" || got[1].AgentSessionID != codex || got[1].ExecutionID != "codex-1" {
		t.Fatalf("Codex record = %#v", got[1])
	}
	if other, err := s.ListTaskNativeSessions(ctx, "other-task"); err != nil || len(other) != 0 {
		t.Fatalf("other task records = %#v, %v", other, err)
	}
	// The same conversation captured by a replacement launch changes ownership.
	replacement := nativeSessionOwner(t, s, "native-task", "codex", "codex-2", nil)
	record(replacement, "codex-2", "codex", "thread-1", "/repo", true)
	record(replacement, "codex-2", "codex", "thread-1", "/repo", false)
}

func TestTaskNativeSessionIsSavedForAnEphemeralAgent(t *testing.T) {
	// So that reloading an ephemeral agent can resume its conversation.
	ctx := context.Background()
	s := newTestStore(t)
	taskID, err := s.CreateEphemeralTask(ctx, "Temporary agent")
	if err != nil {
		t.Fatal(err)
	}
	id := nativeSessionOwner(t, s, taskID, "codex", "exec-1", nil)
	if changed, err := s.RecordTaskNativeSession(ctx, taskID, id, "exec-1", "codex", "thread", "/repo", "/storage"); !changed || err != nil {
		t.Fatalf("record = %v, %v; want it saved", changed, err)
	}
	record, err := s.GetTaskNativeSession(ctx, taskID, "codex")
	if err != nil || record == nil || record.ProviderSessionID != "thread" {
		t.Fatalf("saved = %+v, %v", record, err)
	}
}

func TestTaskNativeSessionRejectsInvalidOwners(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"stale-running", "newer-dead", "dead", "terminal-execution", "wrong-session", "wrong-execution", "wrong-task", "wrong-provider", "execution-provider", "execution-task", "attribution-task", "child-parent", "child-subtask", "worker-role", "worker-execution", "worker-execution-role", "worker-execution-subtask"} {
		t.Run(name, func(t *testing.T) {
			s := newTestStore(t)
			taskID := "native-task"
			createTestTask(t, s, taskID)
			id := nativeSessionOwner(t, s, taskID, "codex", "exec-1", func(a *AgentSession, e *Execution) {
				child := "child"
				switch name {
				case "execution-provider":
					e.AgentKind = "claude-code"
				case "execution-task":
					e.ExecutionTaskID = "other"
				case "attribution-task":
					e.TaskID = "other"
				case "child-parent":
					a.ParentTaskID = &child
				case "child-subtask":
					a.SubtaskID = &child
				case "worker-role":
					a.Role = "worker"
				case "worker-execution":
					e.Kind = ExecutionSwarmWorker
				case "worker-execution-role":
					e.Role = "worker"
				case "worker-execution-subtask":
					e.SwarmSubtaskID = child
				}
			})
			executionID, kind := "exec-1", "codex"
			switch name {
			case "stale-running", "newer-dead":
				newID := nativeSessionOwner(t, s, taskID, "claude-code", "exec-2", nil)
				if name == "newer-dead" {
					if err := s.FinishAgentSession(ctx, newID); err != nil {
						t.Fatal(err)
					}
				}
			case "dead":
				if err := s.FinishAgentSession(ctx, id); err != nil {
					t.Fatal(err)
				}
			case "terminal-execution":
				if err := s.TerminalizeExecution(ctx, executionID, ExecutionSucceeded); err != nil {
					t.Fatal(err)
				}
			case "wrong-session":
				id++
			case "wrong-execution":
				executionID = "another-execution"
			case "wrong-task":
				taskID = "another-task"
			case "wrong-provider":
				kind = "claude-code"
			}
			changed, err := s.RecordTaskNativeSession(ctx, taskID, id, executionID, kind, "thread", "/repo", "/storage")
			if changed || !errors.Is(err, ErrConflict) {
				t.Fatalf("rejected record = %v, %v; want ErrConflict", changed, err)
			}
			if got, err := s.ListTaskNativeSessions(ctx, taskID); err != nil || len(got) != 0 {
				t.Fatalf("rejected write persisted: %#v, %v", got, err)
			}
		})
	}
}

func TestTaskNativeSessionStaleWritePreservesRecord(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "native-task")
	oldID := nativeSessionOwner(t, s, "native-task", "codex", "exec-1", nil)
	newID := nativeSessionOwner(t, s, "native-task", "codex", "exec-2", nil)
	if changed, err := s.RecordTaskNativeSession(ctx, "native-task", newID, "exec-2", "codex", "current-thread", "/repo", "/storage"); err != nil || !changed {
		t.Fatalf("save current = %v, %v", changed, err)
	}
	before, err := s.GetTaskNativeSession(ctx, "native-task", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := s.RecordTaskNativeSession(ctx, "native-task", oldID, "exec-1", "codex", "stale-thread", "/old", "/storage"); changed || !errors.Is(err, ErrConflict) {
		t.Fatalf("stale overwrite = %v, %v", changed, err)
	}
	after, err := s.GetTaskNativeSession(ctx, "native-task", "codex")
	if err != nil || *after != *before {
		t.Fatalf("stale write changed record = %#v, %v", after, err)
	}
}

func TestTaskNativeSessionAllowsTopLevelConductor(t *testing.T) {
	ctx := context.Background()
	for _, promoted := range []bool{false, true} {
		t.Run(fmt.Sprint(promoted), func(t *testing.T) {
			s := newTestStore(t)
			taskID := "native-task"
			createTestTask(t, s, taskID)
			id := nativeSessionOwner(t, s, taskID, "codex", "exec-1", func(a *AgentSession, e *Execution) {
				if !promoted {
					a.Role, a.ParentTaskID = "conductor", &taskID
					e.Kind, e.Role = ExecutionSwarmConductor, "conductor"
				}
			})
			if promoted {
				if err := s.UpdateAgentSessionSwarmRole(ctx, taskID, "conductor", taskID, nil); err != nil {
					t.Fatal(err)
				}
			}
			if changed, err := s.RecordTaskNativeSession(ctx, taskID, id, "exec-1", "codex", "thread", "/repo", "/storage"); err != nil || !changed {
				t.Fatalf("top-level conductor record = %v, %v", changed, err)
			}
			got, err := s.GetTaskNativeSession(ctx, taskID, "codex")
			if err != nil || got == nil || got.AgentSessionID != id || got.ProviderSessionID != "thread" {
				t.Fatalf("conductor conversation = %#v, %v", got, err)
			}
		})
	}
}

func TestTaskNativeSessionConcurrentRecords(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "native-task")
	id := nativeSessionOwner(t, s, "native-task", "codex", "exec-1", nil)
	type result struct {
		changed bool
		err     error
	}
	results := make(chan result, 8)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < cap(results); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			changed, err := s.RecordTaskNativeSession(ctx, "native-task", id, "exec-1", "codex", "thread", "/repo", "/storage")
			results <- result{changed, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	changes := 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.changed {
			changes++
		}
	}
	if changes != 1 {
		t.Fatalf("concurrent identical writes changed %d records, want 1", changes)
	}
	got, err := s.GetTaskNativeSession(ctx, "native-task", "codex")
	if err != nil || got == nil || got.ProviderSessionID != "thread" || got.ExecutionID != "exec-1" {
		t.Fatalf("concurrent record = %#v, %v", got, err)
	}
}

func TestTaskNativeSessionRejectsInvalidInput(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "native-task")
	id := nativeSessionOwner(t, s, "native-task", "codex", "exec-1", nil)
	for i, args := range [][5]string{
		{"", "exec-1", "codex", "thread", "/repo"},
		{"native-task", "", "codex", "thread", "/repo"},
		{"native-task", "exec-1", "chimera", "thread", "/repo"},
		{"native-task", "exec-1", "codex", "  ", "/repo"},
		{"native-task", "exec-1", "codex", "native-task", "/repo"},
		{"native-task", "exec-1", "codex", "thread", ""},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if changed, err := s.RecordTaskNativeSession(ctx, args[0], id, args[1], args[2], args[3], args[4], "/storage"); changed || err == nil {
				t.Fatalf("invalid input = %v, %v", changed, err)
			}
		})
	}
	if changed, err := s.RecordTaskNativeSession(ctx, "native-task", 0, "exec-1", "codex", "thread", "/repo", "/storage"); changed || err == nil {
		t.Fatalf("missing owner = %v, %v", changed, err)
	}
}

func TestTaskNativeSessionPinsProviderStorageDirectory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	createTestTask(t, s, "native-task")
	id := nativeSessionOwner(t, s, "native-task", "codex", "exec-1", nil)
	for _, directory := range []string{"/provider/one", "/provider/two"} {
		if changed, err := s.RecordTaskNativeSession(ctx, "native-task", id, "exec-1", "codex", "thread", "/repo", directory); err != nil || !changed {
			t.Fatalf("storage update = %v, %v", changed, err)
		}
		got, err := s.GetTaskNativeSession(ctx, "native-task", "codex")
		if err != nil || got == nil || got.StorageDir != directory {
			t.Fatalf("provider storage = %#v, %v", got, err)
		}
	}
}
