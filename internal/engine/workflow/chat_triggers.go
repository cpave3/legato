package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

// The kinds of chat trigger: a message a chat bot (Slack, Discord) set up
// in Settings › Integrations receives, and a message that mentions it.
const (
	TriggerChatMessage = "chat.message"
	TriggerChatMention = "chat.mention"
)

// How a chat trigger's loop stop counts runs: per channel (the default) or
// per thread.
const (
	ChatLoopChannel = "channel"
	ChatLoopThread  = "thread"
)

// MaxChatRegexLength bounds a chat trigger's text regex.
const MaxChatRegexLength = 512

// chatBotPattern is what a bot's name may be.
var chatBotPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// ChatBotNamePattern is the pattern a bot's name matches, for the schema.
const ChatBotNamePattern = `^[a-z][a-z0-9_-]*$`

// ChatTrigger starts a workflow on a chat message a bot receives. Its
// inputs may read ${{ event.bot }}, ${{ event.platform }},
// ${{ event.channel }}, ${{ event.user }}, ${{ event.text }},
// ${{ event.message_id }} and ${{ event.thread_id }}.
type ChatTrigger struct {
	// Bot is the bot's name: slack or discord.
	Bot string `yaml:"bot" json:"bot"`
	// Channel and User, when set, are the platform's IDs the message must
	// be in and from.
	Channel string         `yaml:"channel,omitempty" json:"channel,omitempty"`
	User    string         `yaml:"user,omitempty" json:"user,omitempty"`
	Text    *ChatTextMatch `yaml:"text,omitempty" json:"text,omitempty"`
	// IncludeBots lets messages other bots post start runs, which by
	// default they do not. The bot's own messages never do.
	IncludeBots bool `yaml:"include_bots,omitempty" json:"include_bots,omitempty"`
	// LoopItem is what the loop stop counts runs by: channel or thread.
	LoopItem      string         `yaml:"loop_item,omitempty" json:"loop_item,omitempty"`
	Enabled       *bool          `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Confirm       bool           `yaml:"confirm,omitempty" json:"confirm,omitempty"`
	MaxRunsPerDay int            `yaml:"max_runs_per_day,omitempty" json:"max_runs_per_day,omitempty"`
	Inputs        map[string]any `yaml:"inputs,omitempty" json:"inputs,omitempty"`
}

// ChatMessageTrigger and ChatMentionTrigger are a ChatTrigger under each
// kind's name, so the schema gives each its own definition.
type (
	ChatMessageTrigger ChatTrigger
	ChatMentionTrigger ChatTrigger
)

// ChatTextMatch is a chat trigger's test of a message's text: contains
// (ignoring case) or regex (Go's RE2), exactly one.
type ChatTextMatch struct {
	Contains string `yaml:"contains,omitempty" json:"contains,omitempty"`
	Regex    string `yaml:"regex,omitempty" json:"regex,omitempty"`
}

// ChatEventFields is what a chat trigger matches on.
type ChatEventFields struct {
	Bot, Channel, User, Text, ThreadID string
	// FromBot is a message another bot posted.
	FromBot bool
}

// Chat is the chat trigger of a kind the definition declares, or nil.
func (t Triggers) Chat(kind string) *ChatTrigger {
	switch kind {
	case TriggerChatMessage:
		return (*ChatTrigger)(t.ChatMessage)
	case TriggerChatMention:
		return (*ChatTrigger)(t.ChatMention)
	}
	return nil
}

// Validate checks the bot's name, the text match, the limit and that the
// inputs are declared.
func (t *ChatTrigger) Validate(kind string, inputs map[string]Input) error {
	if strings.TrimSpace(t.Bot) == "" {
		return within("bot", fmt.Errorf("%s bot is required", kind))
	}
	if !chatBotPattern.MatchString(t.Bot) {
		return within("bot", fmt.Errorf("%s bot %q is not a bot's name (lower case letters, digits, - and _)", kind, t.Bot))
	}
	if t.Text != nil {
		if err := t.Text.validate(); err != nil {
			return within("text", fmt.Errorf("%s %w", kind, err))
		}
	}
	switch t.LoopItem {
	case "", ChatLoopChannel, ChatLoopThread:
	default:
		return within("loop_item", fmt.Errorf("%s loop_item must be channel or thread", kind))
	}
	if err := validateMaxRunsPerDay(t.MaxRunsPerDay); err != nil {
		return within("max_runs_per_day", fmt.Errorf("%s %w", kind, err))
	}
	return validateTriggerInputs(t.Inputs, inputs)
}

func (m *ChatTextMatch) validate() error {
	switch {
	case m.Contains != "" && m.Regex != "":
		return fmt.Errorf("text takes contains or regex, not both")
	case m.Contains == "" && m.Regex == "":
		return fmt.Errorf("text needs contains or regex")
	case len(m.Regex) > MaxChatRegexLength:
		return fmt.Errorf("text regex is longer than %d characters", MaxChatRegexLength)
	case m.Regex != "":
		if _, err := regexp.Compile(m.Regex); err != nil {
			return fmt.Errorf("text regex: %w", err)
		}
	}
	return nil
}

// Matches says whether a message passes the trigger's filters.
func (t ChatTrigger) Matches(e ChatEventFields) bool {
	if e.Bot != t.Bot {
		return false
	}
	if e.FromBot && !t.IncludeBots {
		return false
	}
	if t.Channel != "" && e.Channel != t.Channel {
		return false
	}
	if t.User != "" && e.User != t.User {
		return false
	}
	if t.Text != nil {
		if t.Text.Contains != "" && !strings.Contains(strings.ToLower(e.Text), strings.ToLower(t.Text.Contains)) {
			return false
		}
		if t.Text.Regex != "" {
			re, err := regexp.Compile(t.Text.Regex)
			if err != nil || !re.MatchString(e.Text) {
				return false
			}
		}
	}
	return true
}

// ItemFor is what the loop stop counts a message's runs by: the bot and
// channel, and the thread too when the trigger says loop_item: thread and
// the message has one.
func (t ChatTrigger) ItemFor(e ChatEventFields) string {
	item := e.Bot + ":" + e.Channel
	if t.LoopItem == ChatLoopThread && e.ThreadID != "" {
		item += "/" + e.ThreadID
	}
	return item
}

func (t *ChatTrigger) common() TriggerCommon {
	return TriggerCommon{Enabled: t.Enabled, Confirm: t.Confirm, MaxRunsPerDay: t.MaxRunsPerDay, Inputs: t.Inputs}
}

// describe is the trigger's summary in the workflow graph.
func (t *ChatTrigger) describe(kind string) string {
	if kind == TriggerChatMention {
		if t.Channel != "" {
			return "Mention of " + t.Bot + " in " + t.Channel
		}
		return "Mention of " + t.Bot
	}
	if t.Channel != "" {
		return "Message in " + t.Channel + " on " + t.Bot
	}
	return "Message on " + t.Bot
}
