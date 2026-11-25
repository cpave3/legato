package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cpave3/legato/internal/engine/slackbot"
	"github.com/cpave3/legato/internal/engine/store"
)

// cameronForwardsAnAlert is what the Slack adapter makes of Cameron
// forwarding an alert to the bot's DM with no note.
func cameronForwardsAnAlert(t *testing.T) ChatEvent {
	t.Helper()
	e, ok := slackChatEvent("slack", slackbot.Identity{UserID: "UBOT", BotID: "BBOT"}, slackbot.Event{
		Type: "message", Channel: "D0BOTDM", ChannelType: "im", User: "UCAMERON", TS: "1724898300.000100",
		Shares: []slackbot.Share{{AuthorID: "UALICE", AuthorName: "alice", ChannelID: "C0ALERTS", ChannelName: "alerts",
			FromURL: "https://example.slack.com/archives/C0ALERTS/p1724898240000200", Text: "checkout is returning 500s"}},
	})
	if !ok {
		t.Fatal("the forward made no event")
	}
	return e
}

// chatEventOf is the event a request keeps.
func chatEventOf(t *testing.T, request store.WorkflowTriggerRequest) map[string]any {
	t.Helper()
	var stored struct {
		Context struct {
			Event map[string]any `json:"event"`
		} `json:"context"`
	}
	if err := json.Unmarshal([]byte(request.EventJSON), &stored); err != nil {
		t.Fatalf("event json %s: %v", request.EventJSON, err)
	}
	return stored.Context.Event
}

func TestAMentionTriggerForOneUserStartsOnTheirForwardToTheBotsDM(t *testing.T) {
	db, coordinator, _ := chatTriggerFixtureWithInputs(t, "  request: {type: string, required: true}\n",
		"  chat.mention:\n    bot: slack\n    user: UCAMERON\n    inputs:\n      request: ${{ event.text }}\n")
	ctx := context.Background()
	if err := coordinator.ChatEvent(ctx, cameronForwardsAnAlert(t)); err != nil {
		t.Fatal(err)
	}
	started := requestsWith(t, db, "started")
	if len(started) != 1 {
		t.Fatalf("started = %+v; waiting for input = %+v", started, requestsWith(t, db, "waiting_input"))
	}
	wantText := "> Forwarded from alice in #alerts:\n> checkout is returning 500s"
	run, err := db.FindWorkflowRunByTriggerRequest(ctx, started[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var inputs map[string]string
	if err := json.Unmarshal([]byte(run.Run.InputsJSON), &inputs); err != nil {
		t.Fatal(err)
	}
	if inputs["request"] != wantText {
		t.Fatalf("request input = %q, want %q", inputs["request"], wantText)
	}
	event := chatEventOf(t, started[0])
	if event["text"] != wantText || event["forwarded"] != true ||
		event["forwarded_url"] != "https://example.slack.com/archives/C0ALERTS/p1724898240000200" {
		t.Fatalf("event = %v", event)
	}
}

func TestAForwardsNoteComesBeforeTheForwardedMessage(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.message:\n    bot: slack\n"+chatInputs)
	e := cameronForwardsAnAlert(t)
	e.Text = "can you look?"
	if err := coordinator.ChatEvent(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	started := requestsWith(t, db, "started")
	if len(started) != 1 {
		t.Fatalf("started = %+v", started)
	}
	if got := chatEventOf(t, started[0])["text"]; got != "can you look?\n\n> Forwarded from alice in #alerts:\n> checkout is returning 500s" {
		t.Fatalf("text = %q", got)
	}
}

func TestAPlainMessageIsNotForwarded(t *testing.T) {
	db, coordinator, _ := chatTriggerFixtureWithInputs(t, "  forwarded: {type: string, required: true}\n",
		"  chat.message:\n    bot: slack\n    inputs:\n      forwarded: ${{ event.forwarded }}\n")
	ctx := context.Background()
	if err := coordinator.ChatEvent(ctx, chatMessage("1", "C1", "hello")); err != nil {
		t.Fatal(err)
	}
	started := requestsWith(t, db, "started")
	if len(started) != 1 {
		t.Fatalf("started = %+v", started)
	}
	event := chatEventOf(t, started[0])
	if event["forwarded"] != false || event["forwarded_url"] != "" {
		t.Fatalf("event = %v", event)
	}
	run, err := db.FindWorkflowRunByTriggerRequest(ctx, started[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(run.Run.InputsJSON, `"forwarded":"false"`) {
		t.Fatalf("inputs = %s", run.Run.InputsJSON)
	}
}

func TestATextFilterMatchesTheForwardedMessage(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.message:\n    bot: slack\n    text: {contains: \"500s\"}\n")
	if err := coordinator.ChatEvent(context.Background(), cameronForwardsAnAlert(t)); err != nil {
		t.Fatal(err)
	}
	if n := len(requestsWith(t, db, "started")); n != 1 {
		t.Fatalf("started = %d, want 1", n)
	}
}

func TestTheCapCutsTheForwardedMessageFirst(t *testing.T) {
	cases := []struct {
		name, note string
		check      func(t *testing.T, text string)
	}{
		{"a short note keeps all of itself", strings.Repeat("n", 1<<10), func(t *testing.T, text string) {
			if !strings.HasPrefix(text, strings.Repeat("n", 1<<10)+"\n\n> Forwarded from alice in #alerts:\n> ") ||
				!strings.HasSuffix(text, "\n> …") || len(text) < MaxChatEventText-8 {
				t.Errorf("text is %d bytes: %.64q … %q", len(text), text, text[max(0, len(text)-16):])
			}
		}},
		{"a long note drops the forward", strings.Repeat("é", 9<<9), func(t *testing.T, text string) {
			if strings.Contains(text, "Forwarded") || !strings.HasPrefix(text, "éé") {
				t.Errorf("text = %.32q", text)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, coordinator, _ := chatTriggerFixture(t, "  chat.message:\n    bot: slack\n")
			e := cameronForwardsAnAlert(t)
			e.Text = tc.note
			e.ForwardedText = "> Forwarded from alice in #alerts:\n> " + strings.Repeat("ü", 5<<10)
			if err := coordinator.ChatEvent(context.Background(), e); err != nil {
				t.Fatal(err)
			}
			started := requestsWith(t, db, "started")
			if len(started) != 1 {
				t.Fatalf("started = %+v", started)
			}
			text, _ := chatEventOf(t, started[0])["text"].(string)
			if len(text) > MaxChatEventText || !utf8.ValidString(text) {
				t.Fatalf("text is %d bytes, valid UTF-8 %v", len(text), utf8.ValidString(text))
			}
			tc.check(t, text)
		})
	}
}
