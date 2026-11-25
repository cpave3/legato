package service

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	err = c.client.Serve(ctx, slackbot.Handlers{
		Event: func(e slackbot.Event) {
			if event, ok := slackChatEvent(c.name, self, e); ok {
				emit(event)
			}
		},
		// Serve has acknowledged the click already, within Slack's 3s.
		Click:     func(click slackbot.Click) { emit(slackClickEvent(c.name, click, c.client)) },
		Connected: connected,
	})
	return slackError(err)
}

// slackClickEvent is a click as a ChatEvent: it replies in the message's
// thread, or starts one under it.
func slackClickEvent(name string, click slackbot.Click, client *slackbot.Client) ChatEvent {
	thread := click.ThreadTS
	if thread == "" {
		thread = click.TS
	}
	e := ChatEvent{Bot: name, Platform: "slack", Channel: click.Channel, User: click.User, MessageID: click.TS,
		ThreadID: thread, At: time.Now().UTC(),
		Click: &ChatClick{Value: click.Value, Label: click.Label, InteractionID: click.InteractionID, MessageText: click.Text}}
	if client != nil {
		e.Click.settle = func(ctx context.Context, line string) error { return slackError(client.Settle(ctx, click, line)) }
	}
	return e
}

func (c *slackConnection) Send(ctx context.Context, channel, text, threadID string, buttons ...ChatButton) (SentMessage, error) {
	posted, err := c.client.PostMessage(ctx, channel, text, threadID, slackButtons(buttons)...)
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

func slackButtons(buttons []ChatButton) []slackbot.Button {
	out := make([]slackbot.Button, len(buttons))
	for i, b := range buttons {
		out[i] = slackbot.Button{Label: b.Label, Value: b.Value, Style: b.Style}
	}
	return out
}

func (c *slackConnection) React(ctx context.Context, channel, messageID, emoji string, remove bool) error {
	name, err := slackReactionName(emoji)
	if err != nil {
		return err
	}
	if remove {
		err = c.client.RemoveReaction(ctx, channel, messageID, name)
	} else {
		err = c.client.AddReaction(ctx, channel, messageID, name)
	}
	var apiErr *slackbot.APIError
	if errors.As(err, &apiErr) && apiErr.Code == "missing_scope" {
		scope := apiErr.Needed
		if scope == "" {
			scope = "reactions:write"
		}
		return fmt.Errorf("slack refused: missing_scope (needs %s). Add %s under OAuth & Permissions › Bot Token Scopes and reinstall the app", scope, scope)
	}
	return slackError(err)
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
	err := c.client.Serve(ctx, discordbot.Handlers{
		Message: func(m discordbot.Message) {
			if event, ok := discordChatEvent(c.name, c.client.SelfID(), m); ok {
				emit(event)
			}
		},
		// Serve has started the deferred response already, within
		// Discord's 3s, so the queue may take its time.
		Interaction: func(i discordbot.Interaction) { emit(discordClickEvent(c.name, i, c.client)) },
		InteractionError: func(err error) {
			log.Printf("chat bots: %s: answering a button click: %v", c.name, err)
		},
		Ready: func(string) { connected() },
	})
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

// discordClickEvent is a click as a ChatEvent: in a thread it replies
// there, elsewhere to the message with the buttons.
func discordClickEvent(name string, i discordbot.Interaction, client *discordbot.Client) ChatEvent {
	thread := i.MessageID
	if i.InThread {
		thread = i.ChannelID
	}
	e := ChatEvent{Bot: name, Platform: "discord", Channel: i.ChannelID, User: i.UserID, MessageID: i.MessageID,
		ThreadID: thread, At: time.Now().UTC(),
		Click: &ChatClick{Value: i.Value, Label: i.Label, InteractionID: i.ID, MessageText: i.Content}}
	if client != nil {
		e.Click.settle = func(ctx context.Context, line string) error { return client.Settle(ctx, i, line) }
	}
	return e
}

// Send posts text with the :names: Legato knows as their emoji, since
// Discord shows a bot's :eyes: as it is.
func (c *discordConnection) Send(ctx context.Context, channel, text, threadID string, buttons ...ChatButton) (SentMessage, error) {
	replyTo := discordReplyTo(channel, threadID)
	discordButtons := make([]discordbot.Button, len(buttons))
	for i, b := range buttons {
		discordButtons[i] = discordbot.Button{Label: b.Label, Value: b.Value, Style: b.Style}
	}
	id, err := c.client.CreateMessage(ctx, channel, expandEmojiShortcodes(text), replyTo, discordButtons...)
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

func (c *discordConnection) React(ctx context.Context, channel, messageID, emoji string, remove bool) error {
	e, err := discordReactionEmoji(emoji)
	if err != nil {
		return err
	}
	if remove {
		err = c.client.RemoveReaction(ctx, channel, messageID, e)
	} else {
		err = c.client.AddReaction(ctx, channel, messageID, e)
	}
	var apiErr *discordbot.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case discordbot.CodeMissingPermissions:
			return fmt.Errorf("%v: the bot lacks a permission in this channel: it needs Add Reactions and Read Message History", err)
		case discordbot.CodeMissingAccess:
			return fmt.Errorf("%v: the bot cannot see this channel: it needs View Channel", err)
		case discordbot.CodeUnknownMessage:
			return fmt.Errorf("%v: there is no message %s in channel %s", err, messageID, channel)
		case discordbot.CodeUnknownEmoji:
			return fmt.Errorf("%v: Discord does not know the emoji %s", err, e)
		}
	}
	return err
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

// ReadThread reads a Slack thread with conversations.replies. Authors are
// named with users.info, once per user; a bot without users:read (or any
// failure to name one) leaves the rest of the read's authors unnamed
// rather than failing it.
func (c *slackConnection) ReadThread(ctx context.Context, channel, threadID string, max int) ([]ChatThreadMessage, error) {
	replies, err := c.client.Replies(ctx, channel, threadID, max)
	if err != nil {
		return nil, slackError(err)
	}
	names := map[string]string{}
	naming := true
	name := func(user string) string {
		if n, ok := names[user]; ok || !naming {
			return n
		}
		n, err := c.client.UserName(ctx, user)
		if err != nil {
			naming = false
			return ""
		}
		names[user] = n
		return n
	}
	out := make([]ChatThreadMessage, 0, len(replies))
	for i, m := range replies {
		msg := ChatThreadMessage{ID: m.TS, AuthorID: m.User, At: slackTime(m.TS), Text: m.Text, Parent: i == 0, FromBot: m.BotID != ""}
		switch {
		case m.User == "":
			msg.AuthorID, msg.AuthorName = m.BotID, m.Username
		case m.BotID != "" && m.Username != "":
			msg.AuthorName = m.Username
		default:
			msg.AuthorName = name(m.User)
		}
		out = append(out, msg.withForwards(slackForwards(m.Shares)))
	}
	return out, nil
}

// discordReplyChainMax is the most messages a reply chain is followed up
// through, one request each.
const discordReplyChainMax = 50

// ReadThread reads a Discord thread (threadID is the channel, a thread
// channel) or, for any other threadID (a message), the chain of replies
// that message ends.
func (c *discordConnection) ReadThread(ctx context.Context, channel, threadID string, max int) ([]ChatThreadMessage, error) {
	var msgs []discordbot.Message
	var err error
	if threadID == channel {
		msgs, err = c.readThreadChannel(ctx, channel, max)
	} else {
		msgs, err = c.readReplyChain(ctx, channel, threadID, min(max, discordReplyChainMax))
	}
	if err != nil {
		return nil, err
	}
	out := make([]ChatThreadMessage, 0, len(msgs))
	for i, m := range msgs {
		out = append(out, ChatThreadMessage{ID: m.ID, AuthorID: m.AuthorID, AuthorName: m.AuthorName, At: m.Timestamp, Text: m.Content,
			Parent: i == 0, FromBot: m.AuthorIsBot}.withForwards(discordForwards(m.Forwards)))
	}
	return out, nil
}

// readThreadChannel is a thread's starter, then its messages oldest first.
// A thread started on a message has that message's ID, and the message
// stays in the parent channel; a forum post's starter is in the thread
// itself. Failing both, the thread's oldest message starts it. System
// messages (the starter's stub in the thread, joins, pins) are left out;
// see discordAuthored.
func (c *discordConnection) readThreadChannel(ctx context.Context, thread string, max int) ([]discordbot.Message, error) {
	var newest []discordbot.Message
	before := ""
	for len(newest) < max {
		ask := min(100, max-len(newest))
		page, err := c.client.ListMessages(ctx, thread, before, ask)
		if err != nil {
			return nil, err
		}
		newest = append(newest, page...)
		if len(page) < ask {
			break
		}
		before = page[len(page)-1].ID
	}
	ch, err := c.client.GetChannel(ctx, thread)
	if err != nil {
		return nil, err
	}
	var starter *discordbot.Message
	for _, place := range []string{ch.ParentID, thread} {
		if place == "" {
			continue
		}
		m, err := c.client.GetMessage(ctx, place, thread)
		if errors.Is(err, discordbot.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		starter = &m
		break
	}
	var msgs []discordbot.Message
	for i := len(newest) - 1; i >= 0; i-- {
		m := newest[i]
		if !discordAuthored(m.Type) || (starter != nil && m.ID == starter.ID) {
			continue
		}
		msgs = append(msgs, m)
	}
	if starter != nil {
		msgs = append([]discordbot.Message{*starter}, msgs...)
	}
	return msgs, nil
}

// discordAuthored is a message type someone wrote: a message (0), a reply
// (19), or an application command's response (20 from a slash command, 23
// from a context menu). The rest are system messages, the starter's stub
// in a thread (21) among them.
func discordAuthored(messageType int) bool {
	switch messageType {
	case 0, 19, 20, 23:
		return true
	}
	return false
}

// readReplyChain is message and the messages it replies to, up to max of
// them, root first. A message that is gone ends the chain.
func (c *discordConnection) readReplyChain(ctx context.Context, channel, message string, max int) ([]discordbot.Message, error) {
	m, err := c.client.GetMessage(ctx, channel, message)
	if err != nil {
		return nil, err
	}
	chain := []discordbot.Message{m}
	for len(chain) < max && m.ReferenceID != "" {
		m, err = c.client.GetMessage(ctx, channel, m.ReferenceID)
		if errors.Is(err, discordbot.ErrNotFound) {
			break
		}
		if err != nil {
			return nil, err
		}
		chain = append(chain, m)
	}
	slices.Reverse(chain)
	return chain, nil
}
