package service

import (
	"slices"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/discordbot"
	"github.com/cpave3/legato/internal/engine/slackbot"
)

func TestSlackMessagesBecomeChatEvents(t *testing.T) {
	self := slackbot.Identity{UserID: "UBOT", BotID: "BBOT"}
	cases := []struct {
		name  string
		event slackbot.Event
		ok    bool
		want  ChatEvent
	}{
		{"a message", slackbot.Event{Type: "message", Channel: "C1", ChannelType: "channel", User: "U1", Text: "hi", TS: "10.1"}, true,
			ChatEvent{Bot: "slack", Platform: "slack", Channel: "C1", User: "U1", Text: "hi", MessageID: "10.1", ThreadID: "10.1"}},
		{"in a thread", slackbot.Event{Type: "message", Channel: "C1", User: "U1", Text: "hi", TS: "10.2", ThreadTS: "10.1"}, true,
			ChatEvent{Bot: "slack", Platform: "slack", Channel: "C1", User: "U1", Text: "hi", MessageID: "10.2", ThreadID: "10.1"}},
		{"a mention in text", slackbot.Event{Type: "message", Channel: "C1", User: "U1", Text: "<@UBOT> deploy", TS: "10.3"}, true,
			ChatEvent{Bot: "slack", Platform: "slack", Channel: "C1", User: "U1", Text: "<@UBOT> deploy", MessageID: "10.3", ThreadID: "10.3", Mention: true}},
		{"app_mention", slackbot.Event{Type: "mention", Channel: "C1", User: "U1", Text: "<@UBOT>", TS: "10.4"}, true,
			ChatEvent{Bot: "slack", Platform: "slack", Channel: "C1", User: "U1", Text: "<@UBOT>", MessageID: "10.4", ThreadID: "10.4", Mention: true}},
		{"a direct message", slackbot.Event{Type: "message", Channel: "D1", ChannelType: "im", User: "U1", Text: "hi", TS: "10.5"}, true,
			ChatEvent{Bot: "slack", Platform: "slack", Channel: "D1", User: "U1", Text: "hi", MessageID: "10.5", ThreadID: "10.5", Mention: true}},
		{"another bot", slackbot.Event{Type: "message", Channel: "C1", BotID: "BOTHER", Text: "hi", TS: "10.6"}, true,
			ChatEvent{Bot: "slack", Platform: "slack", Channel: "C1", Text: "hi", MessageID: "10.6", ThreadID: "10.6", FromBot: true}},
		{"its own, by user", slackbot.Event{Type: "message", Channel: "C1", User: "UBOT", Text: "hi", TS: "10.7"}, false, ChatEvent{}},
		{"its own, by bot", slackbot.Event{Type: "message", Channel: "C1", BotID: "BBOT", Text: "hi", TS: "10.8"}, false, ChatEvent{}},
	}
	for _, tc := range cases {
		got, ok := slackChatEvent("slack", self, tc.event)
		got.At = time.Time{}
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: got %+v, %v; want %+v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDiscordMessagesBecomeChatEvents(t *testing.T) {
	at := time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		message discordbot.Message
		ok      bool
		want    ChatEvent
	}{
		{"a message", discordbot.Message{ID: "m1", ChannelID: "c1", GuildID: "g1", AuthorID: "u1", Content: "hi", Timestamp: at}, true,
			ChatEvent{Bot: "discord", Platform: "discord", Channel: "c1", User: "u1", Text: "hi", MessageID: "m1", ThreadID: "m1", At: at}},
		{"in a thread", discordbot.Message{ID: "m2", ChannelID: "t1", GuildID: "g1", AuthorID: "u1", Content: "hi", InThread: true, Timestamp: at}, true,
			ChatEvent{Bot: "discord", Platform: "discord", Channel: "t1", User: "u1", Text: "hi", MessageID: "m2", ThreadID: "t1", At: at}},
		{"a mention", discordbot.Message{ID: "m3", ChannelID: "c1", GuildID: "g1", AuthorID: "u1", Content: "<@self> go", Mentions: []string{"u9", "self"}, Timestamp: at}, true,
			ChatEvent{Bot: "discord", Platform: "discord", Channel: "c1", User: "u1", Text: "<@self> go", MessageID: "m3", ThreadID: "m3", At: at, Mention: true}},
		{"a direct message", discordbot.Message{ID: "m4", ChannelID: "dm", AuthorID: "u1", Content: "hi", Timestamp: at}, true,
			ChatEvent{Bot: "discord", Platform: "discord", Channel: "dm", User: "u1", Text: "hi", MessageID: "m4", ThreadID: "m4", At: at, Mention: true}},
		{"another bot", discordbot.Message{ID: "m5", ChannelID: "c1", GuildID: "g1", AuthorID: "b2", AuthorIsBot: true, Content: "hi", Timestamp: at}, true,
			ChatEvent{Bot: "discord", Platform: "discord", Channel: "c1", User: "b2", Text: "hi", MessageID: "m5", ThreadID: "m5", At: at, FromBot: true}},
		{"its own", discordbot.Message{ID: "m6", ChannelID: "c1", GuildID: "g1", AuthorID: "self", AuthorIsBot: true, Content: "hi"}, false, ChatEvent{}},
	}
	for _, tc := range cases {
		got, ok := discordChatEvent("discord", "self", tc.message)
		if ok != tc.ok || got != tc.want {
			t.Errorf("%s: got %+v, %v; want %+v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestDiscordThreadIDsSayHowToReply(t *testing.T) {
	cases := []struct{ channel, threadID, replyTo string }{
		{"c1", "", ""},     // a new message
		{"t1", "t1", ""},   // into a thread channel
		{"c1", "m1", "m1"}, // a reply to a message
	}
	for _, tc := range cases {
		if got := discordReplyTo(tc.channel, tc.threadID); got != tc.replyTo {
			t.Errorf("discordReplyTo(%q, %q) = %q, want %q", tc.channel, tc.threadID, got, tc.replyTo)
		}
	}
}

func TestChatBotSpecsFromSettings(t *testing.T) {
	specs := ChatBotSpecs(ChatBotSettings{
		Slack:   SlackBotSettings{Enabled: true, AppToken: "xoxb-wrong", BotToken: "xoxb-1", DefaultChannel: "C1"},
		Discord: DiscordBotSettings{Enabled: true, Token: "", Intents: []string{"guilds"}},
	})
	if len(specs) != 2 || specs[0].Name != "discord" || specs[1].Name != "slack" {
		t.Fatalf("specs = %+v", specs)
	}
	discord, slack := specs[0], specs[1]
	if discord.Configured || discord.Problem == "" {
		t.Fatalf("discord without a token = %+v", discord)
	}
	if !slack.Configured || slack.DefaultChannel != "C1" || slack.Problem == "" || slack.Conn == nil {
		t.Fatalf("slack with a bot token for an app token = %+v", slack)
	}
	for _, secret := range []string{"xoxb-wrong", "xoxb-1"} {
		if !slices.Contains(slack.Secrets, secret) {
			t.Fatalf("slack secrets = %v", slack.Secrets)
		}
	}
	ok := ChatBotSpecs(ChatBotSettings{Slack: SlackBotSettings{AppToken: "xapp-1", BotToken: "xoxb-1"}})
	if ok[1].Problem != "" || ok[1].Enabled || !ok[1].Configured {
		t.Fatalf("slack set up but off = %+v", ok[1])
	}
}
