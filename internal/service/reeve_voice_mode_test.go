package service

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestASpokenTurnIsMarkedForTheReeve(t *testing.T) {
	if got := VoiceMessage("turn the lights off"); got != "[legato voice] turn the lights off" {
		t.Fatalf("got %q", got)
	}
}

func TestTheReevesPromptHasAVoiceSectionOnlyWhileItsVoiceIsOn(t *testing.T) {
	on := ReeveLaunchPrompt("", "## This instance", "", true)
	off := ReeveLaunchPrompt("", "## This instance", "", false)
	for _, want := range []string{"## Voice", "[legato voice]", "speak", "typed"} {
		if !strings.Contains(on, want) {
			t.Errorf("the voice section lacks %q", want)
		}
	}
	if strings.Contains(off, "## Voice") || strings.Contains(off, "[legato voice]") {
		t.Fatal("a voice section with the voice off")
	}
}

func TestASessionStartedWithTheVoiceOnIsToldAboutVoiceMode(t *testing.T) {
	svc, _, agents, _ := newReeveFixture(t)
	svc.opts.Defaults = func() ReeveDefaults { return ReeveDefaults{Agent: "claude-code", Voice: true} }
	if _, err := svc.Start(context.Background(), ReeveStart{StartedBy: "web"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(agents.spawned[0].LaunchPrompt, "[legato voice]") {
		t.Fatal("the launch prompt has no voice section")
	}
}

func TestAFirstSpokenTurnStartsTheReeveWithTheMarkedTextAsItsFirstMessage(t *testing.T) {
	svc, _, agents, _ := newReeveFixture(t)
	svc.opts.Defaults = func() ReeveDefaults { return ReeveDefaults{Agent: "claude-code", Voice: true} }
	if err := svc.Message(context.Background(), VoiceMessage("is the sync fixed?"), "voice:desk"); err != nil {
		t.Fatal(err)
	}
	spawned := agents.spawned[0]
	if spawned.Prompt != "[legato voice] is the sync fixed?" {
		t.Fatalf("first message %q, want the spoken turn itself", spawned.Prompt)
	}
	if strings.Contains(spawned.LaunchPrompt, "is the sync fixed?") {
		t.Fatal("the spoken turn is in the launch prompt's brief too")
	}
}

func TestASpokenTurnThatResumesTheReeveStartsItsFirstMessage(t *testing.T) {
	svc, db, agents, _ := newReeveFixture(t)
	svc.opts.Defaults = func() ReeveDefaults { return ReeveDefaults{Agent: "claude-code", Voice: true} }
	ctx := context.Background()
	session, _ := svc.Start(ctx, ReeveStart{StartedBy: "web"})
	_ = db.SetReeveSessionState(ctx, session.TaskID, store.ReeveSuspended)
	if err := svc.Message(ctx, VoiceMessage("is the sync fixed?"), "voice:desk"); err != nil {
		t.Fatal(err)
	}
	if first := agents.spawned[1].Prompt; !strings.HasPrefix(first, "[legato voice] is the sync fixed?") {
		t.Fatalf("resumed with %q, want the spoken turn first", first)
	}
}

func TestAReeveStartedWithoutItsVoiceIsGivenItWhenNotBusy(t *testing.T) {
	cfg := readyVoiceConfig()
	voice, reeve, db, agents := newReeveVoiceFixture(t, cfg, fakeVoiceProbe{})
	ctx := context.Background()
	reeve.opts.Defaults = func() ReeveDefaults { return ReeveDefaults{Agent: "claude-code", Voice: false} }
	session, err := reeve.Start(ctx, ReeveStart{StartedBy: "web"})
	if err != nil {
		t.Fatal(err)
	}
	// The person turns the voice on while the Reeve runs.
	reeve.opts.Defaults = func() ReeveDefaults { return ReeveDefaults{Agent: "claude-code", Voice: true} }

	got, _ := voice.Status(ctx)
	if got.Ready || !strings.Contains(got.Reason, "getting its voice") {
		t.Fatalf("status %+v, want not ready while the Reeve gets its voice", got)
	}
	// The status takes the lock the restart holds: once Message returns,
	// the restart is done.
	waitUntil(t, func() bool { _ = reeve.Message(ctx, "x", "test"); return len(agents.spawned) == 2 })
	if !strings.Contains(agents.spawned[1].LaunchPrompt, "[legato voice]") || agents.spawned[1].SessionPolicy != "resume" {
		t.Fatalf("restarted with %+v, want the conversation resumed with the voice", agents.spawned[1])
	}
	if current, _ := db.CurrentReeveSession(ctx); current == nil || current.TaskID != session.TaskID || current.State != store.ReeveRunning {
		t.Fatalf("session after = %+v", current)
	}
	if got, _ := voice.Status(ctx); !got.Ready {
		t.Fatalf("status after the restart %+v, want ready", got)
	}
}
