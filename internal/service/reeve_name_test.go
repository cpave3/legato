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

func TestAReeveWithNoNameIsTheReeveWhereverItFallsInASentence(t *testing.T) {
	var n ReeveName
	if n.Label() != "Reeve" || n.Start() != "The Reeve" || n.Mid() != "the Reeve" {
		t.Fatalf("no name: %q %q %q", n.Label(), n.Start(), n.Mid())
	}
}

func TestANamedReeveIsItsNameWhereverItFallsInASentence(t *testing.T) {
	n := ReeveName("Tessitura")
	if n.Label() != "Tessitura" || n.Start() != "Tessitura" || n.Mid() != "Tessitura" {
		t.Fatalf("named: %q %q %q", n.Label(), n.Start(), n.Mid())
	}
	if got := n.Start() + " asks for a secret"; got != "Tessitura asks for a secret" {
		t.Fatal(got)
	}
}

func TestTheReeveNameIsReadFromTheConfiguration(t *testing.T) {
	if got := ReeveNameOf(nil); got != "" {
		t.Fatalf("nil config: %q", got)
	}
	if got := ReeveNameOf(&config.Config{Reeve: config.ReeveConfig{Name: " Tessitura "}}); got != "Tessitura" {
		t.Fatalf("named: %q", got)
	}
	if got := ReeveNameOf(&config.Config{Reeve: config.ReeveConfig{Name: "**x**"}}); got != "" {
		t.Fatalf("invalid: %q", got)
	}
}

func TestTheReevesPromptSaysItsNameOnlyWhenItHasOne(t *testing.T) {
	if got := ReeveNamePrompt(""); got != "" {
		t.Fatalf("no name: %q", got)
	}
	got := ReeveNamePrompt("Tessitura")
	for _, want := range []string{"Tessitura", "Reeve"} {
		if !strings.Contains(got, want) {
			t.Fatalf("named prompt %q lacks %q", got, want)
		}
	}
}

func TestANamedReeveIsToldItsNameAfterItsRoleAndAgainOnResume(t *testing.T) {
	svc, _, agents, _ := newReeveFixture(t)
	name := ReeveName("Tessitura")
	svc.opts.Defaults = func() ReeveDefaults { return ReeveDefaults{Name: name} }
	svc.opts.Snapshot = func(context.Context) string { return "## This instance" }
	ctx := context.Background()
	s, err := svc.Start(ctx, ReeveStart{})
	if err != nil {
		t.Fatal(err)
	}
	prompt := agents.spawned[0].LaunchPrompt
	role, named, snap := strings.Index(prompt, "## You are the Reeve"), strings.Index(prompt, "you go by Tessitura"), strings.Index(prompt, "## This instance")
	if role < 0 || named < role || snap < named {
		t.Fatalf("the name is missing or out of order in:\n%s", prompt)
	}
	svc.End(ctx, s.TaskID)
	name = "Tess"
	svc.Resume(ctx, s.TaskID, ReeveStart{})
	if got := agents.spawned[1].Prompt; !strings.Contains(got, "you go by Tess") {
		t.Fatalf("a resumed session is not told its name now: %q", got)
	}
}

func TestAnUnnamedReeveIsToldNoName(t *testing.T) {
	svc, _, agents, _ := newReeveFixture(t)
	if _, err := svc.Start(context.Background(), ReeveStart{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(agents.spawned[0].LaunchPrompt, "## Your name") {
		t.Fatal("an unnamed Reeve was given a name section")
	}
}

func TestTheVoiceSaysTheReevesNameInItsReasons(t *testing.T) {
	cfg := readyVoiceConfig()
	cfg.Reeve.Name = "Tessitura"
	cfg.Reeve.Voice.Enabled = false
	voice, _, _, _ := newReeveVoiceFixture(t, cfg, fakeVoiceProbe{})
	got, err := voice.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Reeve.Name != "Tessitura" || got.Reason != "Tessitura's voice is off on tessitura" {
		t.Fatalf("name %q, reason %q", got.Reeve.Name, got.Reason)
	}
}

func TestTheReevesEngramItemSaysItsName(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	taskID, _ := s.CreateReeveTask(ctx, "The Reeve")
	if err := s.CreateExecution(ctx, store.Execution{ID: "ex-r", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, TaskID: taskID, ExecutionTaskID: taskID, TaskTitle: "The Reeve", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	_ = s.SetExecutionEngramFailure(ctx, "ex-r", "reeve-1a2b3c4d", "engram was not found")
	adapter := NewEngramAttentionAdapter(s)
	adapter.SetReeveName(func(context.Context) ReeveName { return "Tessitura" })
	items, err := adapter.Snapshot(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %+v %v", items, err)
	}
	if items[0].Title != "Tessitura's engram did not boot" || !strings.HasPrefix(items[0].Reason, "Tessitura started without") || items[0].Actions[0].Label != "Open Tessitura's engram" {
		t.Fatalf("item = %+v", items[0])
	}
}

// The agents list names the Reeve's agent, so the TUI and the web UI show
// its name; other agents are not given it.
func TestTheReevesAgentIsListedUnderItsName(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	svc := NewAgentService(s, newMockTmux(), t.TempDir(), AgentServiceOptions{
		ReeveName: func(context.Context) ReeveName { return "Tessitura" },
	})
	createTask(t, s, "REX-1")
	reeveTask, _ := s.CreateReeveTask(ctx, "The Reeve")
	for _, id := range []string{"REX-1", reeveTask} {
		if err := s.InsertAgentSession(ctx, store.AgentSession{TaskID: id, TmuxSession: "legato-" + id, AgentKind: "claude-code", Status: "running"}); err != nil {
			t.Fatal(err)
		}
	}
	agents, _ := svc.ListAgents(ctx)
	for _, a := range agents {
		switch {
		case a.TaskID == reeveTask && (a.ReeveName != "Tessitura" || a.Title != "Tessitura"):
			t.Fatalf("the Reeve's agent: name %q, title %q", a.ReeveName, a.Title)
		case a.TaskID == "REX-1" && a.ReeveName != "":
			t.Fatalf("another agent was given the name: %+v", a)
		}
	}
}
