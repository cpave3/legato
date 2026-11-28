package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/cpave3/legato/internal/engine/prompt"
)

// Screens a real Claude Code 2.1.332 session drew (LEG-688).
func claudeScreen(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "engine", "prompt", "testdata", "claude-2.1.332", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const idleClaudeScreen = "● Done.\n\n────────\n❯ \n────────\n"

type terminalAnswerFixture struct {
	server  *Server
	tmux    *mockTmuxManager
	client  *wsClient
	answers []WSMessage
}

// newTerminalAnswerFixture is a native agent, whose plain keys Legato
// refuses, showing the given screens one capture after another.
func newTerminalAnswerFixture(t *testing.T, screens ...string) *terminalAnswerFixture {
	t.Helper()
	previous := terminalAnswerSettle
	terminalAnswerSettle = 0
	t.Cleanup(func() { terminalAnswerSettle = previous })
	fixture := &terminalAnswerFixture{tmux: &mockTmuxManager{screens: screens}}
	agents := &promptAgentService{chimeraEndpoint: "http://127.0.0.1:1", chimeraSessionID: "native"}
	fixture.server = New(nil, agents, fixture.tmux, ":0")
	fixture.client = &wsClient{ctx: context.Background(), sendFn: func(m WSMessage) { fixture.answers = append(fixture.answers, m) }}
	return fixture
}

func (f *terminalAnswerFixture) answer(keys, text string) {
	f.server.handleWSMessage(f.client, WSMessage{Type: MsgAnswerTerminalPrompt, AgentID: "native-agent", RequestID: "terminal-answer:1", Keys: keys, Content: text})
}

func (f *terminalAnswerFixture) last(t *testing.T) WSMessage {
	t.Helper()
	if len(f.answers) == 0 {
		t.Fatal("no answer sent")
	}
	return f.answers[len(f.answers)-1]
}

func TestATerminalPromptOnANativeAgentIsAnsweredWithTheKeysOnScreen(t *testing.T) {
	f := newTerminalAnswerFixture(t, claudeScreen(t, "single-question"), idleClaudeScreen)

	f.answer("Down Enter", "")

	if want := []string{"Down", "Enter"}; !reflect.DeepEqual(f.tmux.sentKeys, want) {
		t.Fatalf("sent keys = %v, want %v", f.tmux.sentKeys, want)
	}
	answered := f.last(t)
	if answered.Type != MsgTerminalPromptAnswered || answered.AgentID != "native-agent" || answered.RequestID != "terminal-answer:1" {
		t.Fatalf("answer = %+v", answered)
	}
	// What the screen shows after the keys, so the card knows to close.
	if answered.Prompt == nil || answered.Prompt.Type == prompt.Menu {
		t.Fatalf("prompt after answering = %+v", answered.Prompt)
	}
}

func TestKeysThatAreNotAnAnswerOnScreenAreRefused(t *testing.T) {
	for name, screen := range map[string]string{
		"keys the prompt does not offer": claudeScreen(t, "single-question"),
		"no prompt on screen":            idleClaudeScreen,
	} {
		t.Run(name, func(t *testing.T) {
			f := newTerminalAnswerFixture(t, screen)

			f.answer("Down Down Down Down Down Down Down Enter", "")

			if len(f.tmux.sentKeys) != 0 || len(f.tmux.typed) != 0 {
				t.Fatalf("typed %v %v", f.tmux.sentKeys, f.tmux.typed)
			}
			refusal := f.last(t)
			if refusal.Type != MsgError || refusal.Code != "prompt_changed" || refusal.AgentID != "native-agent" || refusal.RequestID != "terminal-answer:1" {
				t.Fatalf("refusal = %+v", refusal)
			}
			if refusal.Prompt == nil {
				t.Fatal("the refusal does not say what the screen shows now")
			}
		})
	}
}

func TestATextRowIsAnsweredByTypingOnIt(t *testing.T) {
	f := newTerminalAnswerFixture(t, claudeScreen(t, "single-question"), idleClaudeScreen)

	f.answer("Down Down Down Enter", "Purple")

	// The cursor moves to the row, and the text is typed and submitted.
	if want := []string{"Down", "Down", "Down"}; !reflect.DeepEqual(f.tmux.sentKeys, want) {
		t.Fatalf("sent keys = %v", f.tmux.sentKeys)
	}
	if want := []string{"Purple⏎"}; !reflect.DeepEqual(f.tmux.typed, want) {
		t.Fatalf("typed = %v", f.tmux.typed)
	}
	if f.last(t).Type != MsgTerminalPromptAnswered {
		t.Fatalf("answer = %+v", f.last(t))
	}
}

func TestATextRowOfAMultiSelectIsTickedNotSubmitted(t *testing.T) {
	f := newTerminalAnswerFixture(t, claudeScreen(t, "multi-select"), claudeScreen(t, "multi-select-typed"))

	f.answer("Down Down Down Enter", "Hamster")

	if want := []string{"Down", "Down", "Down"}; !reflect.DeepEqual(f.tmux.sentKeys, want) {
		t.Fatalf("sent keys = %v", f.tmux.sentKeys)
	}
	if want := []string{"Hamster"}; !reflect.DeepEqual(f.tmux.typed, want) {
		t.Fatalf("typed = %v", f.tmux.typed)
	}
}

func TestTextIsOnlyTypedWhereAPromptTakesIt(t *testing.T) {
	cases := map[string]struct{ keys, text string }{
		"text for a row that takes none": {"Down Enter", "Purple"},
		"a text row with no text":        {"Down Down Down Enter", ""},
		"text over more than one line":   {"Down Down Down Enter", "one\ntwo"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newTerminalAnswerFixture(t, claudeScreen(t, "single-question"))

			f.answer(c.keys, c.text)

			if len(f.tmux.sentKeys) != 0 || len(f.tmux.typed) != 0 {
				t.Fatalf("typed %v %v", f.tmux.sentKeys, f.tmux.typed)
			}
			if refusal := f.last(t); refusal.Type != MsgError || refusal.RequestID != "terminal-answer:1" || refusal.AgentID != "native-agent" {
				t.Fatalf("refusal = %+v", refusal)
			}
		})
	}
}

func TestAFailedTerminalAnswerSaysSoToTheCard(t *testing.T) {
	f := newTerminalAnswerFixture(t, claudeScreen(t, "permission"))
	f.tmux.sendKeyErr = errors.New("pane is gone")

	f.answer("Enter", "")

	refusal := f.last(t)
	if refusal.Type != MsgError || refusal.AgentID != "native-agent" || refusal.RequestID != "terminal-answer:1" || refusal.Code != "send_failed" {
		t.Fatalf("refusal = %+v", refusal)
	}
}

func TestSendKeysErrorsNameTheAgent(t *testing.T) {
	f := newTerminalAnswerFixture(t)

	f.server.handleWSMessage(f.client, WSMessage{Type: MsgSendKeys, AgentID: "native-agent", RequestID: "keys:1", Keys: "Down Enter"})

	refusal := f.last(t)
	if refusal.Type != MsgError || refusal.AgentID != "native-agent" || refusal.RequestID != "keys:1" {
		t.Fatalf("refusal = %+v", refusal)
	}
}
