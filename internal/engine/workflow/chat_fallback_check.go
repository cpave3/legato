package workflow

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ChatFallbackDiagnostics warns of each fallback chat trigger of d that can
// never fire: another enabled chat trigger on the same bot, with no
// channel, user or text filter, matches every message it would see (a
// chat.message trigger every message, a chat.mention trigger every
// mention). others are the other workflows; one named as d stands for d's
// file before this edit and is passed over. enabled says whether a
// workflow's trigger of a kind is on here.
func ChatFallbackDiagnostics(data []byte, d *Definition, others []*Definition, enabled func(definition *Definition, kind string) bool) []Diagnostic {
	var root yaml.Node
	if d == nil || yaml.Unmarshal(data, &root) != nil {
		return nil
	}
	definitions := []*Definition{d}
	for _, other := range others {
		if other != nil && other.Name != d.Name {
			definitions = append(definitions, other)
		}
	}
	var out []Diagnostic
	for _, kind := range []string{TriggerChatMessage, TriggerChatMention} {
		fallback := d.On.Chat(kind)
		if fallback == nil || !fallback.Fallback {
			continue
		}
		if name, coverKind, ok := coveringChatTrigger(fallback, kind, definitions, enabled); ok {
			diagnostic := diagnosticAt(&root, "on."+kind+".fallback", fmt.Sprintf(
				"this fallback can never fire: %s's %s trigger on %s has no channel, user or text filter, so it matches every message this one would see",
				name, coverKind, fallback.Bot))
			diagnostic.Severity = SeverityWarning
			out = append(out, diagnostic)
		}
	}
	return out
}

// HasChatFallback says whether a chat trigger of the definition is a
// fallback.
func (d *Definition) HasChatFallback() bool {
	for _, kind := range []string{TriggerChatMessage, TriggerChatMention} {
		if trigger := d.On.Chat(kind); trigger != nil && trigger.Fallback {
			return true
		}
	}
	return false
}

// coveringChatTrigger is the first enabled, unfiltered, normal chat trigger
// that matches every message a fallback of a kind would see.
func coveringChatTrigger(fallback *ChatTrigger, fallbackKind string, definitions []*Definition, enabled func(*Definition, string) bool) (string, string, bool) {
	for _, definition := range definitions {
		for _, kind := range []string{TriggerChatMessage, TriggerChatMention} {
			trigger := definition.On.Chat(kind)
			if trigger == nil || trigger.Fallback || trigger.Bot != fallback.Bot ||
				trigger.Channel != "" || trigger.User != "" || trigger.Text != nil ||
				// A mention trigger sees only mentions.
				(kind == TriggerChatMention && fallbackKind != TriggerChatMention) ||
				// A fallback that takes other bots' messages still sees
				// those a trigger without include_bots passes over.
				(fallback.IncludeBots && !trigger.IncludeBots) ||
				!enabled(definition, kind) {
				continue
			}
			return definition.Name, kind, true
		}
	}
	return "", "", false
}
