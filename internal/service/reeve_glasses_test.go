package service

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestAGlassesTurnIsMarkedForTheReeve(t *testing.T) {
	if got := GlassesMessage("start an agent on LEG-212"); got != "[legato glasses] start an agent on LEG-212" {
		t.Fatalf("got %q", got)
	}
}

func TestEveryReevePromptSaysHowToAnswerFromTheGlasses(t *testing.T) {
	for _, voice := range []bool{false, true} {
		prompt := ReeveLaunchPrompt("", "## This instance", "", voice)
		for _, want := range []string{"## Glasses", "[legato glasses]", "short", "plain text"} {
			if !strings.Contains(prompt, want) {
				t.Errorf("voice %v: the prompt lacks %q", voice, want)
			}
		}
	}
}

func TestAFirstGlassesTurnStartsTheReeveWithTheMarkedTextAsItsFirstMessage(t *testing.T) {
	svc, _, agents, _ := newReeveFixture(t)
	if err := svc.Message(context.Background(), GlassesMessage("is the sync fixed?"), "glasses"); err != nil {
		t.Fatal(err)
	}
	spawned := agents.spawned[0]
	if spawned.Prompt != "[legato glasses] is the sync fixed?" {
		t.Fatalf("first message %q, want the glasses turn itself", spawned.Prompt)
	}
	if strings.Contains(spawned.LaunchPrompt, "is the sync fixed?") {
		t.Fatal("the glasses turn is in the launch prompt's brief too")
	}
}

func TestAGlassesTurnThatResumesTheReeveStartsItsFirstMessage(t *testing.T) {
	svc, db, agents, _ := newReeveFixture(t)
	ctx := context.Background()
	session, _ := svc.Start(ctx, ReeveStart{StartedBy: "web"})
	_ = db.SetReeveSessionState(ctx, session.TaskID, store.ReeveSuspended)
	if err := svc.Message(ctx, GlassesMessage("is the sync fixed?"), "glasses"); err != nil {
		t.Fatal(err)
	}
	if first := agents.spawned[1].Prompt; !strings.HasPrefix(first, "[legato glasses] is the sync fixed?") {
		t.Fatalf("resumed with %q, want the glasses turn first", first)
	}
}
