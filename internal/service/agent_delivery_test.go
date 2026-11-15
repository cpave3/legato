package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	enginetmux "github.com/cpave3/legato/internal/engine/tmux"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestMessageToExitedAgentIsNotTypedAndEndsSession(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "shell-target")
	if err := svc.SpawnAgent(ctx, "shell-target", 0, 0); err != nil {
		t.Fatal(err)
	}
	mt.mu.Lock()
	mt.paneCommands = map[string]string{"legato-shell-target": "zsh"}
	mt.mu.Unlock()
	err := svc.(*agentService).QueueMessage(ctx, "shell-target", "touch /tmp/LEG-192-must-not-run")
	if err == nil || !strings.Contains(err.Error(), "not delivered") {
		t.Fatalf("delivery = %v; want refusal", err)
	}
	if got := mt.sentMultilineFor("legato-shell-target"); len(got) != 0 {
		t.Fatalf("typed into shell: %v", got)
	}
	sessions, err := s.GetAgentSessionByTmuxName(ctx, "legato-shell-target")
	if err != nil {
		t.Fatal(err)
	}
	if sessions.Status == "running" {
		t.Fatalf("sessions = %+v; want ended", sessions)
	}
}

func TestLaunchMessageToShellIsRetained(t *testing.T) {
	svc, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "launch-shell")
	if err := svc.SpawnAgent(ctx, "launch-shell", 0, 0); err != nil {
		t.Fatal(err)
	}
	session, err := s.GetAgentSessionByTaskID(ctx, "launch-shell")
	if err != nil {
		t.Fatal(err)
	}
	a := svc.(*agentService)
	mt.mu.Lock()
	mt.paneCommands = map[string]string{session.TmuxSession: "bash"}
	mt.mu.Unlock()
	kept := []store.LaunchMessage{{ExecutionID: session.ExecutionID, TaskID: session.TaskID, TmuxSession: session.TmuxSession, Owner: a.launchOwner, Message: "echo must-not-run"}}
	if err := s.InsertLaunchMessages(ctx, kept); err != nil {
		t.Fatal(err)
	}
	a.deliverLaunchMessages(ctx, session.ExecutionID, session.TaskID, session.TmuxSession, nil, kept, "")
	pending, err := s.ListLaunchMessages(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %v, %v; want retained message", pending, err)
	}
	if lines := mt.sentLinesFor(session.TmuxSession); len(lines) != 0 {
		t.Fatalf("typed into shell: %v", lines)
	}
}

func TestExitedWorkflowAgentFailsItsWaitingStep(t *testing.T) {
	agents, s, mt := newTestAgentService(t)
	ctx := context.Background()
	workflows := NewWorkflowService(s, []WorkflowExecutor{NewAgentRunExecutor(NewWorkflowAgentRunner(agents, s))})
	def, err := workflowengine.Parse("exit.yaml", []byte(`
schema_version: 1
name: exit
title: Exit
steps:
 - id: run
   title: Run
   type: agent.run
   with: {prompt: Work, agent: claude-code}
`))
	if err != nil {
		t.Fatal(err)
	}
	view, err := workflows.Start(ctx, def, nil)
	if err != nil {
		t.Fatal(err)
	}
	attempt := view.Attempts[0]
	if attempt.Status != "waiting_agent" {
		t.Fatalf("attempt = %+v", attempt)
	}
	// First observe the real workflow-owned session running its tool.
	if err := agents.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}
	mt.mu.Lock()
	mt.paneCommands = map[string]string{attempt.TmuxSession: "zsh"}
	mt.mu.Unlock()
	if err := agents.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := workflows.Show(ctx, view.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Attempts[0].Status != "failed" || after.Run.Status != "failed" {
		t.Fatalf("run = %+v; want failure instead of waiting_agent", after)
	}
	if !mt.sessionAlive(attempt.TmuxSession) {
		t.Fatal("exit reconciliation must leave the shell alone")
	}
}

// Exercise the raw tmux entry point used by the prompt bar and macros, not
// just the service's SendMessage preflight. This executable is the tmux boundary.
func TestRawKeysUseAgentLifecycleGuard(t *testing.T) {
	_, s, _ := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "raw-target")
	dir := t.TempDir()
	commandFile := filepath.Join(dir, "foreground")
	sentFile := filepath.Join(dir, "sent")
	script := filepath.Join(dir, "tmux")
	if err := os.WriteFile(commandFile, []byte("claude"), 0600); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`#!/bin/sh
case "$1" in
 display-message) printf '%%s ' '%%7'; cat %q ;;
 send-keys) printf '%%s\n' "$*" >> %q ;;
 *) exit 1 ;;
esac
`, commandFile, sentFile)
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	manager, err := enginetmux.New(enginetmux.Options{LookPath: func(string) (string, error) { return script, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: "raw-target", TmuxSession: "legato-raw-target", AgentKind: "claude-code", Status: "running", StartedAt: "2000-01-01 00:00:00"}); err != nil {
		t.Fatal(err)
	}
	_ = NewAgentService(s, manager, dir)
	if err := manager.SendKeysMultiline("legato-raw-target", "one\ntwo"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(sentFile)
	if err != nil || !strings.Contains(string(before), "-t %7 -- [swarm event b64:") {
		t.Fatalf("delivery = %s, %v", before, err)
	}
	// Missing process identity is a refusal, not evidence that the agent exited.
	if err := os.Remove(commandFile); err != nil {
		t.Fatal(err)
	}
	if err := manager.SendKey("legato-raw-target", "Enter"); err == nil {
		t.Fatal("failed foreground probe accepted keys")
	}
	session, err := s.GetAgentSessionByTmuxName(ctx, "legato-raw-target")
	if err != nil || session.Status != "running" {
		t.Fatalf("probe error ended agent: %+v, %v", session, err)
	}
	if err := os.WriteFile(commandFile, []byte("claude"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateChimeraDiscovery(ctx, "raw-target", "native-raw", "http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	if err := manager.SendKey("legato-raw-target", "Enter"); err == nil {
		t.Fatal("bound agent accepted raw keys")
	}
	if _, err := s.ClearChimeraDiscoveryForSession(ctx, session.ID, "native-raw", "http://127.0.0.1:1"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(commandFile, []byte("zsh"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, send := range []func() error{
		func() error { return manager.SendKeys("legato-raw-target", "echo unsafe") },
		func() error { return manager.SendKey("legato-raw-target", "Enter") },
		func() error { return manager.SendKeysLine("legato-raw-target", "echo unsafe") },
		func() error { return manager.SendKeysMultiline("legato-raw-target", "one\ntwo") },
	} {
		if err := send(); err == nil {
			t.Fatal("shell accepted raw keys")
		}
	}
	after, err := os.ReadFile(sentFile)
	if err != nil || string(after) != string(before) {
		t.Fatalf("keys emitted after exit: %s, %v", after, err)
	}
	session, err = s.GetAgentSessionByTmuxName(ctx, "legato-raw-target")
	if err != nil || session.Status != "dead" {
		t.Fatalf("session = %+v, %v", session, err)
	}
}

func TestNativeLaunchFailureRetainsMessageWithoutTyping(t *testing.T) {
	svc, s, mt := newNativeLaunchService(t, t.TempDir())
	ctx := context.Background()
	createTask(t, s, "native-failure")
	if err := svc.SpawnAgent(ctx, "native-failure", 0, 0); err != nil {
		t.Fatal(err)
	}
	session, err := s.GetAgentSessionByTaskID(ctx, "native-failure")
	if err != nil {
		t.Fatal(err)
	}
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer failing.Close()
	if err := s.UpdateChimeraDiscovery(ctx, session.TaskID, "native-failure", failing.URL); err != nil {
		t.Fatal(err)
	}
	a := svc.(*agentService)
	kept := []store.LaunchMessage{{ExecutionID: session.ExecutionID, TaskID: session.TaskID, TmuxSession: session.TmuxSession, Owner: a.launchOwner, Message: "echo must-not-type"}}
	if err := s.InsertLaunchMessages(ctx, kept); err != nil {
		t.Fatal(err)
	}
	before := len(mt.sentLinesFor(session.TmuxSession))
	a.deliverLaunchMessages(ctx, session.ExecutionID, session.TaskID, session.TmuxSession, a.adapter, kept, "")
	pending, err := s.ListLaunchMessages(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending = %v, %v", pending, err)
	}
	if got := mt.sentLinesFor(session.TmuxSession); len(got) != before {
		t.Fatalf("typed native failure: %v", got)
	}
}

func TestExitedBorrowedAgentFailsItsWaitingInterview(t *testing.T) {
	agents, s, mt := newTestAgentService(t)
	ctx := context.Background()
	createTask(t, s, "interview-target")
	if err := agents.SpawnAgent(ctx, "interview-target", 0, 0); err != nil {
		t.Fatal(err)
	}
	interview := NewAgentInterviewExecutor(&fakeReflectionRequestCreator{}, NewWorkflowAgentRunner(agents, s), s)
	workflows := NewWorkflowService(s, []WorkflowExecutor{interview})
	def, err := workflowengine.Parse("interview-exit.yaml", []byte(`
schema_version: 1
name: interview-exit
title: Interview exit
inputs:
  task: {type: task, required: true}
steps:
  - id: interview
    type: agent.interview
    with: {task: "${{ workflow.inputs.task }}"}
    outputs:
      available: {type: boolean}
      reason: {type: string}
      request_id: {type: string}
`))
	if err != nil {
		t.Fatal(err)
	}
	view, err := workflows.Start(ctx, def, map[string]string{"task": "interview-target"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Attempts[0].Status != "waiting_agent" {
		t.Fatalf("attempt = %+v", view.Attempts[0])
	}
	mt.mu.Lock()
	mt.paneCommands = map[string]string{"legato-interview-target": "bash"}
	mt.mu.Unlock()
	if err := agents.ReconcileSessions(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := workflows.Show(ctx, view.Run.ID)
	if err != nil || after.Run.Status != "failed" {
		t.Fatalf("run = %+v, %v; want failed interview", after, err)
	}
}
