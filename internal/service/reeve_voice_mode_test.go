package service

import (
	"context"
	"strings"
	"testing"
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
