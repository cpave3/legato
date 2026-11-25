package workflow

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// TriggerChatButton starts a workflow when someone clicks a button a chat
// bot posted with chat.message.send's buttons.
const TriggerChatButton = "chat.button"

// The limits a button keeps on both platforms: Slack's label and Discord's
// custom_id, which is the value, are the smaller.
const (
	MaxChatButtons          = 5
	MaxChatButtonLabel      = 75
	MaxChatButtonValue      = 100
	ChatButtonStylePrimary  = "primary"
	ChatButtonStyleDanger   = "danger"
	chatButtonStyleDefault  = ""
	chatButtonWithoutUserMs = "anyone in the channel can start this workflow by clicking; set user to who may"
)

// ChatButtonTrigger starts a workflow on a click on a button a bot
// posted. Its inputs may read ${{ event.bot }}, ${{ event.platform }},
// ${{ event.channel }}, ${{ event.user }}, ${{ event.value }},
// ${{ event.label }}, ${{ event.message_id }}, ${{ event.thread_id }} and,
// for a value regex with named groups, ${{ event.match.<name> }}.
type ChatButtonTrigger struct {
	// Bot is the bot's name: slack or discord.
	Bot string `yaml:"bot" json:"bot"`
	// Channel and User, when set, are the platform's IDs the click must be
	// in and from. A click from another user starts nothing.
	Channel string         `yaml:"channel,omitempty" json:"channel,omitempty"`
	User    string         `yaml:"user,omitempty" json:"user,omitempty"`
	Value   *ChatTextMatch `yaml:"value,omitempty" json:"value,omitempty"`

	Enabled       *bool          `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Confirm       bool           `yaml:"confirm,omitempty" json:"confirm,omitempty"`
	MaxRunsPerDay int            `yaml:"max_runs_per_day,omitempty" json:"max_runs_per_day,omitempty"`
	Inputs        map[string]any `yaml:"inputs,omitempty" json:"inputs,omitempty"`
}

// ChatClickFields is what a chat.button trigger matches a click on.
type ChatClickFields struct {
	Bot, Channel, User, Value, MessageID string
}

// ClickMatch is how a click stands against a chat.button trigger.
type ClickMatch int

const (
	// ClickUnmatched: the bot, channel or value differ.
	ClickUnmatched ClickMatch = iota
	// ClickRefused: everything matches but who clicked.
	ClickRefused
	// ClickMatched: the click starts a run.
	ClickMatched
)

// Validate checks the bot's name, the value match, the limit and that the
// inputs are declared.
func (t *ChatButtonTrigger) Validate(inputs map[string]Input) error {
	kind := TriggerChatButton
	if strings.TrimSpace(t.Bot) == "" {
		return within("bot", fmt.Errorf("%s bot is required", kind))
	}
	if !chatBotPattern.MatchString(t.Bot) {
		return within("bot", fmt.Errorf("%s bot %q is not a bot's name (lower case letters, digits, - and _)", kind, t.Bot))
	}
	if t.Value != nil {
		if err := t.Value.validateAs("value"); err != nil {
			return within("value", fmt.Errorf("%s %w", kind, err))
		}
	}
	if err := validateMaxRunsPerDay(t.MaxRunsPerDay); err != nil {
		return within("max_runs_per_day", fmt.Errorf("%s %w", kind, err))
	}
	return validateTriggerInputs(t.Inputs, inputs)
}

// Match says how a click stands against the trigger.
func (t ChatButtonTrigger) Match(e ChatClickFields) ClickMatch {
	if e.Bot != t.Bot || (t.Channel != "" && e.Channel != t.Channel) || !t.Value.matches(e.Value) {
		return ClickUnmatched
	}
	if t.User != "" && e.User != t.User {
		return ClickRefused
	}
	return ClickMatched
}

// Groups are the named groups of the value regex in the click's value.
func (t ChatButtonTrigger) Groups(e ChatClickFields) map[string]string {
	if t.Value == nil || t.Value.Regex == "" {
		return nil
	}
	re, err := regexp.Compile(t.Value.Regex)
	if err != nil {
		return nil
	}
	found := re.FindStringSubmatch(e.Value)
	if found == nil {
		return nil
	}
	groups := map[string]string{}
	for i, name := range re.SubexpNames() {
		if name != "" {
			groups[name] = found[i]
		}
	}
	return groups
}

// ItemFor is what the loop stop counts a click's runs by: the message with
// the buttons.
func (t ChatButtonTrigger) ItemFor(e ChatClickFields) string {
	return e.Bot + ":" + e.Channel + "/" + e.MessageID
}

func (t *ChatButtonTrigger) common() TriggerCommon {
	return TriggerCommon{Enabled: t.Enabled, Confirm: t.Confirm, MaxRunsPerDay: t.MaxRunsPerDay, Inputs: t.Inputs}
}

// describe is the trigger's summary in the workflow graph.
func (t *ChatButtonTrigger) describe() string {
	s := "Button"
	if t.Value != nil && t.Value.Contains != "" {
		s += " " + t.Value.Contains + "…"
	}
	if t.Channel != "" {
		s += " in " + t.Channel
	}
	return s + " on " + t.Bot
}

// chatButtonsWithoutUser warns of a chat.button trigger with no user.
func chatButtonsWithoutUser(root *yaml.Node, definition *Definition) []Diagnostic {
	if t := definition.On.ChatButton; t == nil || t.User != "" {
		return nil
	}
	d := diagnosticAt(root, "on."+TriggerChatButton+".bot", TriggerChatButton+": "+chatButtonWithoutUserMs)
	d.Severity = SeverityWarning
	return []Diagnostic{d}
}

// checkChatButtons checks a chat.message.send step's buttons as far as the
// file can tell: a list of 1 to 5 buttons, each a label and a value with
// an optional style, within the limits, and no two values alike. A value
// given by ${{ }} is checked when the step runs.
func checkChatButtons(value any) error {
	if text, ok := value.(string); ok && HasExpression(text) {
		return nil
	}
	list, ok := value.([]any)
	if !ok {
		return fmt.Errorf("with buttons must be a list of buttons, each {label, value, style}")
	}
	if len(list) < 1 || len(list) > MaxChatButtons {
		return fmt.Errorf("with buttons must have 1 to %d buttons", MaxChatButtons)
	}
	values := map[string]bool{}
	for i, item := range list {
		at := fmt.Sprintf("with buttons[%d]", i)
		button, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be a map of label, value and style", at)
		}
		for _, key := range sortedKeys(button) {
			if key != "label" && key != "value" && key != "style" {
				return fmt.Errorf("%s: no field %q: a button has label, value and style", at, key)
			}
		}
		label, ok := button["label"].(string)
		if !ok || strings.TrimSpace(label) == "" {
			return fmt.Errorf("%s: label is required, as text", at)
		}
		if !HasExpression(label) && utf8.RuneCountInString(label) > MaxChatButtonLabel {
			return fmt.Errorf("%s: label is longer than %d characters", at, MaxChatButtonLabel)
		}
		text, ok := button["value"].(string)
		if !ok || strings.TrimSpace(text) == "" {
			return fmt.Errorf("%s: value is required, as text", at)
		}
		if !HasExpression(text) {
			if utf8.RuneCountInString(text) > MaxChatButtonValue {
				return fmt.Errorf("%s: value is longer than %d characters", at, MaxChatButtonValue)
			}
			if values[text] {
				return fmt.Errorf("%s: value %q is another button's too; each button needs its own", at, text)
			}
			values[text] = true
		}
		if style, present := button["style"]; present {
			s, ok := style.(string)
			if !ok || !(s == ChatButtonStylePrimary || s == ChatButtonStyleDanger || s == chatButtonStyleDefault || HasExpression(s)) {
				return fmt.Errorf("%s: style must be primary or danger, or left out", at)
			}
		}
	}
	return nil
}
