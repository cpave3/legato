package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// reviewedTask gives db a task with a running agent in slot, its execution
// (with broker token when one is given), and a handoff's review loop on it
// with one blocking item, f-1. It returns the execution's ID.
func reviewedTask(t *testing.T, db *store.Store, taskID, slot, token string) string {
	t.Helper()
	ctx := context.Background()
	if _, err := db.GetTask(ctx, taskID); err != nil {
		if err := db.CreateTask(ctx, store.Task{ID: taskID, Title: "Fix it", Status: "Doing"}); err != nil {
			t.Fatal(err)
		}
		if err := db.SaveHandoffReview(ctx, store.HandoffReview{TaskID: taskID, Phase: "waiting_for_author", Pass: 1, UpdatedAt: "now"}); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertHandoffReviewItem(ctx, store.HandoffReviewItem{ID: "f-1", TaskID: taskID, Kind: "blocking", Pass: 1, PR: 7, Version: 1, Title: "No bound", Body: "/clear", CreatedAt: "now"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.InsertAgentSession(ctx, store.AgentSession{TaskID: taskID, Slot: slot, TmuxSession: "legato-" + taskID + "_" + slot, Command: "claude", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	session, err := db.GetAgentSessionInSlot(ctx, taskID, slot)
	if err != nil {
		t.Fatal(err)
	}
	execution := "ex-" + taskID + "-" + slot
	if err := db.CreateExecution(ctx, store.Execution{ID: execution, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: taskID,
		ExecutionTaskID: taskID, AgentSessionID: &session.ID, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if token != "" {
		if err := db.SetExecutionBrokerToken(ctx, execution, service.HashBrokerToken(token)); err != nil {
			t.Fatal(err)
		}
	}
	return execution
}

// reviewPassStore is a configuration whose store the command opens.
func reviewPassStore(t *testing.T) (*config.Config, *store.Store) {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte("db:\n  path: "+filepath.Join(root, "legato.db")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	db, err := openStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return cfg, db
}

func answerOf(t *testing.T, db *store.Store, taskID string) store.HandoffReviewItem {
	t.Helper()
	item, err := db.GetHandoffReviewItem(context.Background(), taskID, "f-1")
	if err != nil {
		t.Fatal(err)
	}
	return *item
}

// The host runs a sandboxed session's forwarded command with the session's
// task and execution, from its broker token: the store path reads the loop
// the session authors.
func TestReviewPassCommandsReadAndAnswerTheSessionsFeedback(t *testing.T) {
	_, db := reviewPassStore(t)
	execution := reviewedTask(t, db, "task-1", store.SlotMain, "")
	t.Setenv("LEGATO_TASK_ID", "task-1")
	t.Setenv("LEGATO_EXECUTION_ID", execution)

	out := captureStdout(t, func() int { return runCLI([]string{"review-pass", "feedback", "--json"}) })
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Items []struct {
				ID   string `json:"id"`
				Body string `json:"body"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || !envelope.OK || len(envelope.Data.Items) != 1 || envelope.Data.Items[0].Body != "/clear" {
		t.Fatalf("feedback = %s (%v)", out, err)
	}

	if code := runCLI([]string{"review-pass", "answer", "f-1", "--skipped"}); code == 0 {
		t.Fatal("a blocking finding was skipped")
	}
	if code := runCLI([]string{"review-pass", "answer", "f-1", "--fixed", "--note", "bounded"}); code != 0 {
		t.Fatalf("answer exit %d", code)
	}
	if item := answerOf(t, db, "task-1"); item.Answer != "fixed" || item.AnswerNote != "bounded" || item.Posted {
		t.Fatalf("item = %+v", item)
	}
}

// Inside a session, --task is refused: an agent cannot answer another
// task's loop by naming it (AC 15).
func TestReviewPassRefusesTaskInsideASession(t *testing.T) {
	_, db := reviewPassStore(t)
	reviewedTask(t, db, "task-1", store.SlotMain, "")
	other := reviewedTask(t, db, "task-2", store.SlotMain, "")
	t.Setenv("LEGATO_TASK_ID", "task-2")
	t.Setenv("LEGATO_EXECUTION_ID", other)

	for _, args := range [][]string{
		{"review-pass", "answer", "f-1", "--fixed", "--task", "task-1", "--json"},
		{"review-pass", "feedback", "--task", "task-1", "--json"},
	} {
		out, code := runCLICaptured(t, args...)
		if code != exitForbidden || !strings.Contains(out, `"task_in_session"`) {
			t.Fatalf("%v: exit %d, out %s", args, code, out)
		}
	}
	if item := answerOf(t, db, "task-1"); item.Answer != "" {
		t.Fatalf("another session answered task-1: %+v", item)
	}
}

// A session's broker answers for the session its token names: the author
// answers with no task ID, its reviewer slot is refused, and neither reads
// the configuration or the store itself.
func TestReviewPassGoesThroughTheBrokerAsTheSession(t *testing.T) {
	cfg, db := reviewPassStore(t)
	reviewedTask(t, db, "task-1", store.SlotMain, "lgb_author")
	reviewedTask(t, db, "task-1", service.SlotReviewer, "lgb_reviewer")
	host := startBrokerHost(context.Background(), db, shortDir(t), brokerHostOptions{Deps: hostBrokerDeps(cfg, db, nil, nil, nil, nil, nil, nil)})
	t.Cleanup(host.Stop)
	unloadableConfig(t)
	t.Setenv("LEGATO_BROKER_SOCKET", host.SocketPath())

	t.Setenv("LEGATO_BROKER_TOKEN", "lgb_reviewer")
	out, code := runCLICaptured(t, "review-pass", "answer", "f-1", "--replied", "--note", "It is fine.", "--json")
	if code != exitForbidden || !strings.Contains(out, "reviewer slot") {
		t.Fatalf("reviewer: exit %d, out %s", code, out)
	}
	if item := answerOf(t, db, "task-1"); item.Answer != "" {
		t.Fatalf("the reviewer's answer was kept: %+v", item)
	}

	t.Setenv("LEGATO_BROKER_TOKEN", "lgb_author")
	out, code = runCLICaptured(t, "review-pass", "feedback", "--json")
	if code != 0 || !strings.Contains(out, `"f-1"`) {
		t.Fatalf("feedback: exit %d, out %s", code, out)
	}
	out, code = runCLICaptured(t, "review-pass", "answer", "f-1", "--fixed", "--note", "bounded", "--json")
	if code != 0 || !strings.Contains(out, `"task_id":"task-1"`) {
		t.Fatalf("answer: exit %d, out %s", code, out)
	}
	if item := answerOf(t, db, "task-1"); item.Answer != "fixed" || item.AnswerNote != "bounded" {
		t.Fatalf("item = %+v", item)
	}
}

// In the sandbox, review-pass asks the host for the session's own loop
// instead of being forwarded whole.
func TestASandboxedReviewPassIsNotForwardedWhole(t *testing.T) {
	t.Setenv("LEGATO_SANDBOXED", "1")
	t.Setenv("LEGATO_CONFIG", "")
	for _, verb := range []string{"feedback", "answer"} {
		if forwardsToHost([]string{"review-pass", verb}) {
			t.Fatalf("review-pass %s is forwarded whole", verb)
		}
		// A host without review_pass.* still runs it whole.
		if !forwardable([]string{"review-pass", verb}) {
			t.Fatalf("review-pass %s is not forwardable to an older host", verb)
		}
	}
}

// runCLICaptured runs the command and returns what it wrote (stdout and
// stderr) and its exit code.
func runCLICaptured(t *testing.T, args ...string) (string, int) {
	t.Helper()
	old, oldErr := os.Stdout, os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = w, w
	done := make(chan []byte)
	go func() {
		data, _ := io.ReadAll(r)
		done <- data
	}()
	code := runCLI(args)
	w.Close()
	os.Stdout, os.Stderr = old, oldErr
	out := <-done
	r.Close()
	return string(out), code
}

// Outside any session the operator on the host names the task.
func TestReviewPassOutsideASessionNeedsTask(t *testing.T) {
	_, db := reviewPassStore(t)
	reviewedTask(t, db, "task-1", store.SlotMain, "")
	for _, name := range []string{"LEGATO_TASK_ID", "LEGATO_EXECUTION_ID", "LEGATO_BROKER_SOCKET", "LEGATO_BROKER_TOKEN", "LEGATO_SANDBOXED"} {
		t.Setenv(name, "")
	}
	if code := runCLI([]string{"review-pass", "feedback", "--json"}); code != exitUsage {
		t.Fatalf("feedback without --task: exit %d", code)
	}
	if code := runCLI([]string{"review-pass", "answer", "f-1", "--fixed", "--task", "task-1"}); code != 0 {
		t.Fatalf("answer --task: exit %d", code)
	}
	if item := answerOf(t, db, "task-1"); item.Answer != "fixed" {
		t.Fatalf("item = %+v", item)
	}
}
