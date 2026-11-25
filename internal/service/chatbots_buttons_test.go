package service

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/discordbot"
	"github.com/cpave3/legato/internal/engine/slackbot"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

func TestChatBotsSendPassesButtonsOn(t *testing.T) {
	conn := &fakeChatConnection{name: "slack", platform: "slack"}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Conn: conn}}, nil)
	buttons := []ChatButton{{Label: "🛠 Fix it", Value: "fix:T1", Style: "primary"}, {Label: "No", Value: "no"}}
	if _, err := bots.Send(context.Background(), "slack", "C1", "Findings", "", buttons...); err != nil {
		t.Fatal(err)
	}
	if got := conn.sent[0].Buttons; !reflect.DeepEqual(got, buttons) {
		t.Fatalf("buttons = %+v", got)
	}
}

func TestChatBotsSendRefusesButtonsNoPlatformTakes(t *testing.T) {
	conn := &fakeChatConnection{name: "slack", platform: "slack"}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Conn: conn}}, nil)
	ok := ChatButton{Label: "a", Value: "a"}
	for name, tc := range map[string]struct {
		buttons []ChatButton
		says    string
	}{
		"six":         {[]ChatButton{ok, {Label: "b", Value: "b"}, {Label: "c", Value: "c"}, {Label: "d", Value: "d"}, {Label: "e", Value: "e"}, {Label: "f", Value: "f"}}, "at most 5"},
		"no label":    {[]ChatButton{ok, {Value: "b"}}, "button 2: no label"},
		"long label":  {[]ChatButton{{Label: strings.Repeat("é", 76), Value: "b"}}, "button 1: the label is longer than 75"},
		"no value":    {[]ChatButton{{Label: "a", Value: " "}}, "button 1: no value"},
		"long value":  {[]ChatButton{{Label: "a", Value: strings.Repeat("v", 101)}}, "button 1: the value is longer than 100"},
		"same values": {[]ChatButton{ok, {Label: "b", Value: "a"}}, "button 2: the value \"a\""},
		"bad style":   {[]ChatButton{{Label: "a", Value: "a", Style: "big"}}, "button 1: style"},
	} {
		_, err := bots.Send(context.Background(), "slack", "C1", "Findings", "", tc.buttons...)
		if err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.says)
		}
	}
	if len(conn.sent) != 0 {
		t.Fatalf("sent %d messages", len(conn.sent))
	}
}

func TestChatSendPostsTheStepsButtons(t *testing.T) {
	conn := &fakeChatConnection{name: "slack", platform: "slack"}
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Configured: true, Conn: conn}}, nil)
	executor := NewChatSendExecutor(bots)
	_, err := executor.Start(context.Background(), chatSendStep(map[string]any{"bot": "slack", "channel": "C1", "text": "Findings",
		"buttons": []any{map[string]any{"label": "Fix", "value": "fix:T1", "style": "primary"}, map[string]any{"label": "No", "value": "no"}}}))
	if err != nil {
		t.Fatal(err)
	}
	want := []ChatButton{{Label: "Fix", Value: "fix:T1", Style: "primary"}, {Label: "No", Value: "no"}}
	if !reflect.DeepEqual(conn.sent[0].Buttons, want) {
		t.Fatalf("buttons = %+v", conn.sent[0].Buttons)
	}
	for name, buttons := range map[string]any{
		"a string":      "fix",
		"a list of one": []any{"fix"},
		"a number":      []any{map[string]any{"label": "a", "value": 3}},
	} {
		_, err := executor.Start(context.Background(), chatSendStep(map[string]any{"bot": "slack", "channel": "C1", "text": "x", "buttons": buttons}))
		if err == nil || !strings.Contains(err.Error(), "buttons") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestASlackClickIsAChatEvent(t *testing.T) {
	click := slackbot.Click{Channel: "C1", User: "U7", TS: "5.5", ThreadTS: "5.0", Value: "fix:T1", Label: "Fix", InteractionID: "trig-1"}
	e := slackClickEvent("slack", click, nil)
	if e.Click == nil || e.Click.Value != "fix:T1" || e.Click.Label != "Fix" || e.Click.InteractionID != "trig-1" {
		t.Fatalf("click = %+v", e.Click)
	}
	if e.Bot != "slack" || e.Platform != "slack" || e.Channel != "C1" || e.User != "U7" || e.MessageID != "5.5" || e.ThreadID != "5.0" || e.At.IsZero() {
		t.Fatalf("event = %+v", e)
	}
	click.ThreadTS = ""
	if e := slackClickEvent("slack", click, nil); e.ThreadID != "5.5" {
		t.Fatalf("thread of a message in none = %q", e.ThreadID)
	}
}

func TestADiscordClickIsAChatEvent(t *testing.T) {
	i := discordbot.Interaction{ID: "int-1", ChannelID: "c1", UserID: "u7", MessageID: "m9", Value: "fix:T1", Label: "Fix"}
	e := discordClickEvent("discord", i, nil)
	if e.Click == nil || e.Click.Value != "fix:T1" || e.Click.InteractionID != "int-1" || e.MessageID != "m9" || e.ThreadID != "m9" || e.User != "u7" {
		t.Fatalf("event = %+v, click %+v", e, e.Click)
	}
	i.InThread = true
	if e := discordClickEvent("discord", i, nil); e.ThreadID != "c1" {
		t.Fatalf("thread = %q", e.ThreadID)
	}
}

func TestSettlingAClickGoesThroughItsBotAndHidesTokens(t *testing.T) {
	secret := "xoxb-super-secret"
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Conn: &fakeChatConnection{}, Secrets: []string{secret}}}, nil)
	var lines []string
	e := ChatEvent{Bot: "slack", Click: &ChatClick{Value: "fix", settle: func(ctx context.Context, line string) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("settle has no deadline")
		}
		lines = append(lines, line)
		return nil
	}}}
	if err := bots.SettleClick(context.Background(), e, "Fix — chosen by <@U7>"); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "Fix — chosen by <@U7>" {
		t.Fatalf("lines = %v", lines)
	}
	e.Click.settle = func(context.Context, string) error { return errors.New("refused " + secret) }
	if err := bots.SettleClick(context.Background(), e, "x"); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("err = %v", err)
	}
	if err := bots.SettleClick(context.Background(), ChatEvent{Bot: "slack"}, "x"); err == nil {
		t.Fatal("settled a message event")
	}
}

func TestAClickReachesTheEventHandler(t *testing.T) {
	click := ChatEvent{Bot: "slack", Platform: "slack", Channel: "C1", MessageID: "5.5", Click: &ChatClick{Value: "fix", InteractionID: "i1"}}
	conn := &fakeChatConnection{name: "slack", platform: "slack", runs: []func(context.Context, func(ChatEvent), func()) error{
		func(ctx context.Context, emit func(ChatEvent), connected func()) error {
			connected()
			emit(click)
			<-ctx.Done()
			return ctx.Err()
		},
	}}
	got := make(chan ChatEvent, 1)
	bots, _ := testChatBots(t, []ChatBotSpec{{Name: "slack", Platform: "slack", Enabled: true, Conn: conn}}, func(_ context.Context, e ChatEvent) { got <- e })
	bots.Start(context.Background())
	t.Cleanup(bots.Stop)
	select {
	case e := <-got:
		if e.Click == nil || e.Click.InteractionID != "i1" {
			t.Fatalf("event = %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no click")
	}
}

const buttonInputs = "    inputs:\n      text: ${{ event.value }}\n      thread: ${{ event.match.task }}\n"

func chatClick(interaction, message, user, value string) ChatEvent {
	return ChatEvent{Bot: "slack", Platform: "slack", Channel: "C1", User: user, MessageID: message, ThreadID: "5.0",
		Click: &ChatClick{Value: value, Label: "🛠 Fix it", InteractionID: interaction}}
}

func TestAChatButtonStartsTheWorkflowWithTheEvent(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.button:\n    bot: slack\n    user: U7\n    value: {regex: \"^fix:(?P<task>.+)$\"}\n"+buttonInputs)
	ctx := context.Background()
	outcome, err := coordinator.ChatButton(ctx, chatClick("i1", "5.5", "U7", "fix:LEG-1"))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != ChatClickAccepted || !outcome.Started || outcome.Line != "🛠 Fix it — chosen by <@U7>" {
		t.Fatalf("outcome = %+v", outcome)
	}
	started := requestsOfKind(t, db, "started", workflowengine.TriggerChatButton)
	if len(started) != 1 || started[0].Item != "slack:C1/5.5" {
		t.Fatalf("started = %+v", started)
	}
	run, err := db.FindWorkflowRunByTriggerRequest(ctx, started[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"text":"fix:LEG-1"`, `"thread":"LEG-1"`} {
		if !strings.Contains(run.Run.InputsJSON, want) {
			t.Fatalf("inputs %s lack %s", run.Run.InputsJSON, want)
		}
	}
	for _, want := range []string{`"platform":"slack"`, `"message_id":"5.5"`, `"thread_id":"5.0"`, `"user":"U7"`, `"label":"🛠 Fix it"`, `"value":"fix:LEG-1"`, `"bot":"slack"`, `"channel":"C1"`} {
		if !strings.Contains(started[0].EventJSON, want) {
			t.Fatalf("event %s lacks %s", started[0].EventJSON, want)
		}
	}
}

func TestAClickByAnotherUserStartsNothing(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.button:\n    bot: slack\n    user: U7\n")
	outcome, err := coordinator.ChatButton(context.Background(), chatClick("i1", "5.5", "U8", "fix:LEG-1"))
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status != ChatClickRefused || outcome.Line != "" {
		t.Fatalf("outcome = %+v", outcome)
	}
	if n := len(requestsWith(t, db, "started")); n != 0 {
		t.Fatalf("started = %d", n)
	}
}

func TestAChatButtonClickFiresOnce(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.button:\n    bot: slack\n")
	ctx := context.Background()
	if outcome, _ := coordinator.ChatButton(ctx, chatClick("i1", "5.5", "U7", "fix")); outcome.Status != ChatClickAccepted {
		t.Fatalf("first = %+v", outcome)
	}
	if outcome, _ := coordinator.ChatButton(ctx, chatClick("i1", "5.5", "U7", "fix")); outcome.Status != ChatClickDuplicate || outcome.Line != "" {
		t.Fatalf("again = %+v", outcome)
	}
	if n := len(requestsWith(t, db, "started")); n != 1 {
		t.Fatalf("started = %d", n)
	}
}

func TestTheLoopStopCountsButtonRunsPerMessage(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.button:\n    bot: slack\n    max_runs_per_day: 1\n")
	ctx := context.Background()
	coordinator.ChatButton(ctx, chatClick("i1", "5.5", "U7", "fix"))
	held, err := coordinator.ChatButton(ctx, chatClick("i2", "5.5", "U7", "fix"))
	if err != nil {
		t.Fatal(err)
	}
	if held.Status != ChatClickAccepted || held.Started || !strings.HasSuffix(held.Line, " · waiting in Legato") {
		t.Fatalf("held = %+v", held)
	}
	coordinator.ChatButton(ctx, chatClick("i3", "6.6", "U7", "fix"))
	if started, held := len(requestsWith(t, db, "started")), len(requestsWith(t, db, "held")); started != 2 || held != 1 {
		t.Fatalf("started %d, held %d", started, held)
	}
}

func TestAPausedOrUnmatchedButtonTriggerTakesNoClick(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.button:\n    bot: slack\n    enabled: false\n")
	if outcome, err := coordinator.ChatButton(context.Background(), chatClick("i1", "5.5", "U7", "fix")); err != nil || outcome.Status != ChatClickUnmatched {
		t.Fatalf("paused: %+v, %v", outcome, err)
	}
	_, coordinator, _ = chatTriggerFixture(t, "  chat.button:\n    bot: slack\n    value: {contains: dismiss}\n")
	if outcome, err := coordinator.ChatButton(context.Background(), chatClick("i1", "5.5", "U7", "fix")); err != nil || outcome.Status != ChatClickUnmatched {
		t.Fatalf("unmatched: %+v, %v", outcome, err)
	}
	if n := len(requestsWith(t, db, "started")); n != 0 {
		t.Fatalf("started = %d", n)
	}
}

func TestAClickIsNotAMessage(t *testing.T) {
	db, coordinator, _ := chatTriggerFixture(t, "  chat.message:\n    bot: slack\n")
	if err := coordinator.ChatEvent(context.Background(), chatClick("i1", "5.5", "U7", "fix")); err != nil {
		t.Fatal(err)
	}
	if n := len(requestsWith(t, db, "started")); n != 0 {
		t.Fatalf("a click started %d message runs", n)
	}
}
