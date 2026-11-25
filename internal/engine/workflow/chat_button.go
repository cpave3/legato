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
	MaxChatButtons = 5
	// MaxChatButtonEntries bounds a step's list of buttons, those with an
	// if: included; at most MaxChatButtons of them may be shown at once.
	MaxChatButtonEntries    = 25
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
// ${{ event.label }}, ${{ event.message_id }}, ${{ event.message_text }},
// ${{ event.thread_id }} and,
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
// file can tell: a list of 1 to 25 buttons, each a label and a value with
// an optional style and if:, within the limits. At most 5 have no if:, and
// no two of those share a value; a button with an if: is checked when the
// step runs, as is a label, value or style given by ${{ }}.
func checkChatButtons(value any) error {
	if text, ok := value.(string); ok && HasExpression(text) {
		return nil
	}
	list, ok := value.([]any)
	if !ok {
		return fmt.Errorf("with buttons must be a list of buttons, each {label, value, style, if}")
	}
	if len(list) < 1 || len(list) > MaxChatButtonEntries {
		return fmt.Errorf("with buttons must have 1 to %d buttons", MaxChatButtonEntries)
	}
	values := map[string]bool{}
	always := 0
	for i, item := range list {
		at := fmt.Sprintf("with buttons[%d]", i)
		button, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be a map of label, value, style and if", at)
		}
		for _, key := range sortedKeys(button) {
			if key != "label" && key != "value" && key != "style" && key != "if" {
				return fmt.Errorf("%s: no field %q: a button has label, value, style and if", at, key)
			}
		}
		_, conditional := button["if"]
		if !conditional {
			always++
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
			if !conditional && values[text] {
				return fmt.Errorf("%s: value %q is another button's too; each button needs its own", at, text)
			}
			if !conditional {
				values[text] = true
			}
		}
		if style, present := button["style"]; present {
			s, ok := style.(string)
			if !ok || !(s == ChatButtonStylePrimary || s == ChatButtonStyleDanger || s == chatButtonStyleDefault || HasExpression(s)) {
				return fmt.Errorf("%s: style must be primary or danger, or left out", at)
			}
		}
	}
	if always > MaxChatButtons {
		return fmt.Errorf("with buttons must have 1 to %d buttons without if", MaxChatButtons)
	}
	return nil
}

// checkChatButtonConditions checks the if: of each button of a step's
// chat buttons fields: a condition whose fields the step can read.
func checkChatButtonConditions(step Step, inputs map[string]Input, previous map[string]map[string]string) *startProblem {
	spec := catalogByType[step.Type]
	for _, name := range sortedKeys(step.With) {
		field, ok := spec.Field(name)
		if !ok || field.Kind != KindChatButtons {
			continue
		}
		list, _ := step.With[name].([]any)
		for i, item := range list {
			button, _ := item.(map[string]any)
			value, conditional := button["if"]
			if !conditional {
				continue
			}
			at := fmt.Sprintf("with.%s[%d].if", name, i)
			fail := func(err error) *startProblem {
				return &startProblem{at, fmt.Errorf("with %s[%d] if: %w", name, i, err)}
			}
			if _, ok := value.(map[string]any); !ok {
				return fail(fmt.Errorf("must be a condition: all, any, not or field"))
			}
			condition, err := decodeCondition(value)
			if err != nil {
				return fail(err)
			}
			err = condition.validate(func(field string) error {
				path, err := ParsePath(field)
				if err != nil {
					return err
				}
				_, err = pathType(path, inputs, nil, previous)
				return err
			})
			if err != nil {
				return fail(err)
			}
		}
	}
	return nil
}

// chatButtonConditions are the if: of the step's chat buttons that read
// as conditions.
func chatButtonConditions(step Step) []Condition {
	spec := catalogByType[step.Type]
	var conditions []Condition
	for name, value := range step.With {
		if field, ok := spec.Field(name); !ok || field.Kind != KindChatButtons {
			continue
		}
		list, _ := value.([]any)
		for _, item := range list {
			button, _ := item.(map[string]any)
			if raw, conditional := button["if"]; conditional {
				if condition, err := decodeCondition(raw); err == nil {
					conditions = append(conditions, condition)
				}
			}
		}
	}
	return conditions
}

// chatButtons resolves a list of buttons as the step runs: a button whose
// if: is false is left out, and the rest resolved without their if:. Every
// button may be left out. A list given whole by ${{ }} resolves as it is.
func (s resolveScope) chatButtons(value any) (any, error) {
	list, ok := value.([]any)
	if !ok {
		return s.value(value)
	}
	shown := make([]any, 0, len(list))
	for i, item := range list {
		button, ok := item.(map[string]any)
		if !ok {
			shown = append(shown, item)
			continue
		}
		if raw, conditional := button["if"]; conditional {
			condition, err := decodeCondition(raw)
			if err != nil {
				return nil, fmt.Errorf("buttons[%d] if: %w", i, err)
			}
			show, err := condition.evaluate(s.field)
			if err != nil {
				return nil, fmt.Errorf("buttons[%d] if: %w", i, err)
			}
			if !show {
				continue
			}
			rest := make(map[string]any, len(button))
			for key, field := range button {
				if key != "if" {
					rest[key] = field
				}
			}
			button = rest
		}
		resolved, err := s.value(button)
		if err != nil {
			return nil, fmt.Errorf("buttons[%d]: %w", i, err)
		}
		shown = append(shown, resolved)
	}
	return shown, nil
}
