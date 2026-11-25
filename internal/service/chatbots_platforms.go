package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/discordbot"
	"github.com/cpave3/legato/internal/engine/slackbot"
)

// ChatBotSettings is what the configuration says of the chat bots, with
// the tokens themselves (resolved from their secrets' names in memory).
type ChatBotSettings struct {
	Slack   SlackBotSettings
	Discord DiscordBotSettings
}

// SlackBotSettings is integrations.slack.
type SlackBotSettings struct {
	Enabled            bool
	AppToken, BotToken string
	DefaultChannel     string
}

// DiscordBotSettings is integrations.discord.
type DiscordBotSettings struct {
	Enabled        bool
	Token          string
	Intents        []string
	DefaultChannel string
}

// ChatBotSpecs makes a spec per platform, by name: a bot without its
// tokens, or with a token of the wrong kind, gets a Problem.
func ChatBotSpecs(s ChatBotSettings) []ChatBotSpec {
	slack := ChatBotSpec{Name: "slack", Platform: "slack", Enabled: s.Slack.Enabled, DefaultChannel: s.Slack.DefaultChannel,
		Configured: s.Slack.AppToken != "" && s.Slack.BotToken != "", Secrets: []string{s.Slack.AppToken, s.Slack.BotToken}}
	switch {
	case s.Slack.AppToken == "" || s.Slack.BotToken == "":
		slack.Problem = "the app-level token and the bot token are both needed"
	case !strings.HasPrefix(s.Slack.AppToken, "xapp-"):
		slack.Problem = "the app-level token must start xapp- (Basic Information › App-Level Tokens)"
	case !strings.HasPrefix(s.Slack.BotToken, "xoxb-"):
		slack.Problem = "the bot token must start xoxb- (OAuth & Permissions)"
	}
	slack.Conn = newSlackConnection("slack", &slackbot.Client{AppToken: s.Slack.AppToken, BotToken: s.Slack.BotToken})

	discord := ChatBotSpec{Name: "discord", Platform: "discord", Enabled: s.Discord.Enabled, DefaultChannel: s.Discord.DefaultChannel,
		Configured: s.Discord.Token != "", Secrets: []string{s.Discord.Token}}
	intents, err := discordbot.IntentsFromNames(s.Discord.Intents)
	switch {
	case s.Discord.Token == "":
		discord.Problem = "the bot token is needed"
	case err != nil:
		discord.Problem = err.Error()
	}
	discord.Conn = newDiscordConnection("discord", &discordbot.Client{Token: s.Discord.Token, Intents: intents})
	return []ChatBotSpec{discord, slack}
}

// slackConnection is a Slack bot as a ChatConnection.
type slackConnection struct {
	name   string
	client *slackbot.Client
}

func newSlackConnection(name string, client *slackbot.Client) *slackConnection {
	return &slackConnection{name: name, client: client}
}

func (c *slackConnection) Name() string     { return c.name }
func (c *slackConnection) Platform() string { return "slack" }

func (c *slackConnection) Run(ctx context.Context, emit func(ChatEvent), connected func()) error {
	self, err := c.client.AuthTest(ctx)
	if err != nil {
		return slackError(err)
	}
	err = c.client.Run(ctx, func(e slackbot.Event) {
		if event, ok := slackChatEvent(c.name, self, e); ok {
			emit(event)
		}
	}, connected)
	return slackError(err)
}

func (c *slackConnection) Send(ctx context.Context, channel, text, threadID string) (SentMessage, error) {
	posted, err := c.client.PostMessage(ctx, channel, text, threadID)
	if err != nil {
		return SentMessage{}, slackError(err)
	}
	if posted.Channel != "" {
		channel = posted.Channel
	}
	if threadID == "" {
		threadID = posted.TS
	}
	return SentMessage{MessageID: posted.TS, Channel: channel, ThreadID: threadID}, nil
}

func slackError(err error) error {
	switch {
	case err == nil, errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, slackbot.ErrAuth):
		return fmt.Errorf("%w: %v", ErrChatAuth, err)
	case errors.Is(err, slackbot.ErrReconnect):
		return fmt.Errorf("%w: %v", ErrChatReconnect, err)
	}
	return err
}

// slackChatEvent is a Slack event as a ChatEvent; ok is false for the
// bot's own messages. A message in a thread replies there; one that starts
// none gets a thread under it.
func slackChatEvent(name string, self slackbot.Identity, e slackbot.Event) (ChatEvent, bool) {
	if (self.UserID != "" && e.User == self.UserID) || (self.BotID != "" && e.BotID == self.BotID) {
		return ChatEvent{}, false
	}
	thread := e.ThreadTS
	if thread == "" {
		thread = e.TS
	}
	mention := e.Type == "mention" || e.ChannelType == "im" || (self.UserID != "" && strings.Contains(e.Text, "<@"+self.UserID+">"))
	return ChatEvent{Bot: name, Platform: "slack", Channel: e.Channel, User: e.User, Text: e.Text, MessageID: e.TS,
		ThreadID: thread, At: slackTime(e.TS), Mention: mention, FromBot: e.BotID != ""}.withForwards(slackForwards(e.Shares)), true
}

// slackTime reads a Slack ts (seconds.micros); zero when it cannot.
func slackTime(ts string) time.Time {
	var sec, micro int64
	if _, err := fmt.Sscanf(ts, "%d.%d", &sec, &micro); err != nil {
		return time.Time{}
	}
	return time.Unix(sec, micro*1000).UTC()
}

// discordConnection is a Discord bot as a ChatConnection.
type discordConnection struct {
	name   string
	client *discordbot.Client
	mu     sync.Mutex // one session at a time
}

func newDiscordConnection(name string, client *discordbot.Client) *discordConnection {
	return &discordConnection{name: name, client: client}
}

func (c *discordConnection) Name() string     { return c.name }
func (c *discordConnection) Platform() string { return "discord" }

func (c *discordConnection) Run(ctx context.Context, emit func(ChatEvent), connected func()) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.client.Run(ctx, func(m discordbot.Message) {
		if event, ok := discordChatEvent(c.name, c.client.SelfID(), m); ok {
			emit(event)
		}
	}, func(string) { connected() })
	switch {
	case err == nil, errors.Is(err, context.Canceled):
		return err
	case errors.Is(err, discordbot.ErrAuth), errors.Is(err, discordbot.ErrIntents), errors.Is(err, discordbot.ErrConfig):
		return fmt.Errorf("%w: %v", ErrChatAuth, err)
	case errors.Is(err, discordbot.ErrReconnect):
		return fmt.Errorf("%w: %v", ErrChatReconnect, err)
	}
	return err
}

func (c *discordConnection) Send(ctx context.Context, channel, text, threadID string) (SentMessage, error) {
	replyTo := discordReplyTo(channel, threadID)
	id, err := c.client.CreateMessage(ctx, channel, text, replyTo)
	if err != nil {
		return SentMessage{}, err
	}
	switch {
	case threadID == "":
		threadID = id
	case replyTo == "":
		threadID = channel
	}
	return SentMessage{MessageID: id, Channel: channel, ThreadID: threadID}, nil
}

// discordReplyTo is the message a send replies to: a thread_id that is
// the channel itself (a thread channel) is none.
func discordReplyTo(channel, threadID string) string {
	if threadID == channel {
		return ""
	}
	return threadID
}

// discordChatEvent is a Discord message as a ChatEvent; ok is false for the
// bot's own. A message in a thread replies there; any other is replied to.
func discordChatEvent(name, self string, m discordbot.Message) (ChatEvent, bool) {
	if self != "" && m.AuthorID == self {
		return ChatEvent{}, false
	}
	thread := m.ID
	if m.InThread {
		thread = m.ChannelID
	}
	mention := m.GuildID == "" || (self != "" && slices.Contains(m.Mentions, self))
	return ChatEvent{Bot: name, Platform: "discord", Channel: m.ChannelID, User: m.AuthorID, Text: m.Content, MessageID: m.ID,
		ThreadID: thread, At: m.Timestamp, Mention: mention, FromBot: m.AuthorIsBot}.withForwards(discordForwards(m.Forwards)), true
}
