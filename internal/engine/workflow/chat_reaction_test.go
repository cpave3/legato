package workflow_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// chat.reaction.add and chat.reaction.remove react as a chat bot (LEG-633),
// with the bot and channel fields of chat.message.send.
func TestAChatReactionStepNamesItsBotMessageAndEmoji(t *testing.T) {
	for _, stepType := range []string{"chat.reaction.add", "chat.reaction.remove"} {
		step := func(with string) string {
			return checkBase + `steps:
  - id: react
    title: React
    type: ` + stepType + `
    with:
` + with
		}
		for name, with := range map[string]string{
			"all":        "      bot: slack\n      channel: C1\n      message_id: \"1.5\"\n      emoji: eyes\n",
			"no channel": "      bot: discord\n      message_id: m1\n      emoji: \"👀\"\n",
			"templates":  "      bot: ${{ workflow.inputs.task.metadata.bot }}\n      message_id: ${{ workflow.inputs.task.metadata.m }}\n      emoji: ${{ workflow.inputs.task.metadata.e }}\n",
		} {
			if definition, diagnostics := workflow.Check("react.yaml", []byte(step(with))); definition == nil || len(diagnostics) != 0 {
				t.Fatalf("%s %s: diagnostics = %+v", stepType, name, diagnostics)
			}
		}
		for name, with := range map[string]string{
			"no bot":     "      message_id: m1\n      emoji: eyes\n",
			"no message": "      bot: slack\n      emoji: eyes\n",
			"no emoji":   "      bot: slack\n      message_id: m1\n",
			"bad bot":    "      bot: Slack!\n      message_id: m1\n      emoji: eyes\n",
		} {
			if _, err := workflow.Parse("react.yaml", []byte(step(with))); err == nil {
				t.Fatalf("%s %s: accepted", stepType, name)
			}
		}
		spec, ok := workflow.LookupExecutor(stepType)
		if !ok || spec.Group != "Chat" {
			t.Fatalf("spec = %+v", spec)
		}
		send, _ := workflow.LookupExecutor("chat.message.send")
		if !reflect.DeepEqual(spec.With[:2], send.With[:2]) {
			t.Fatalf("%s bot and channel = %+v, %+v; want send's %+v, %+v", stepType, spec.With[0], spec.With[1], send.With[0], send.With[1])
		}
		var names []string
		for _, field := range spec.With {
			names = append(names, field.Name)
			if !field.Essential {
				t.Errorf("%s: %s is not essential", stepType, field.Name)
			}
		}
		if !slices.Equal(names, []string{"bot", "channel", "message_id", "emoji"}) {
			t.Fatalf("%s fields = %v", stepType, names)
		}
		if len(spec.Outputs) != 1 || spec.Outputs[0].Name != "ok" || spec.Outputs[0].Type != "boolean" {
			t.Fatalf("%s outputs = %+v", stepType, spec.Outputs)
		}
	}
}
