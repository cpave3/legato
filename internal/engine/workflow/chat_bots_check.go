package workflow

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ChatBotState is what a Legato knows of a chat bot of a name.
type ChatBotState struct {
	// Configured is a bot whose tokens are set; Enabled one switched on.
	Configured, Enabled bool
}

// ChatBotDiagnostics checks the bots a definition's chat triggers and chat
// steps name against this Legato's, which lookup gives (ok false: no bot of
// the name). A name no Legato has is an error; a bot not set up here, or
// switched off, is a warning, since a workflow file may be shared by
// Legatos that differ. A step's bot given by ${{ }} is checked when the
// step runs.
func ChatBotDiagnostics(data []byte, d *Definition, lookup func(name string) (ChatBotState, bool)) []Diagnostic {
	var root yaml.Node
	if d == nil || yaml.Unmarshal(data, &root) != nil {
		return nil
	}
	var out []Diagnostic
	check := func(path, name string) {
		state, ok := lookup(name)
		var diagnostic Diagnostic
		switch {
		case !ok:
			diagnostic = diagnosticAt(&root, path, fmt.Sprintf("no bot named %q: Legato's bots are slack and discord", name))
		case !state.Configured:
			diagnostic = diagnosticAt(&root, path, fmt.Sprintf("bot %s is not set up on this Legato (Settings › Integrations)", name))
			diagnostic.Severity = SeverityWarning
		case !state.Enabled:
			diagnostic = diagnosticAt(&root, path, fmt.Sprintf("bot %s is switched off on this Legato (Settings › Integrations)", name))
			diagnostic.Severity = SeverityWarning
		default:
			return
		}
		out = append(out, diagnostic)
	}
	for _, kind := range []string{TriggerChatMessage, TriggerChatMention} {
		if trigger := d.On.Chat(kind); trigger != nil {
			check("on."+kind+".bot", trigger.Bot)
		}
	}
	if trigger := d.On.ChatButton; trigger != nil {
		check("on."+TriggerChatButton+".bot", trigger.Bot)
	}
	for i, step := range d.Steps {
		if !strings.HasPrefix(step.Type, "chat.") {
			continue
		}
		if name, ok := step.With["bot"].(string); ok && name != "" && !expressionPattern.MatchString(name) {
			check(fmt.Sprintf("steps[%d].with.bot", i), name)
		}
	}
	return out
}
