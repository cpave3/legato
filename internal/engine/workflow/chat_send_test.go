package workflow_test

import (
	"slices"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// chat.message.send posts as a chat bot (LEG-624).
func TestAChatSendStepNamesItsBotAndText(t *testing.T) {
	step := func(with string) string {
		return checkBase + `steps:
  - id: say
    title: Say
    type: chat.message.send
    with:
` + with
	}
	for name, with := range map[string]string{
		"all":          "      bot: slack\n      channel: C1\n      text: hello\n      thread_id: \"1.2\"\n",
		"no channel":   "      bot: discord\n      text: hello\n",
		"template bot": "      bot: ${{ workflow.inputs.task.metadata.bot }}\n      text: hello\n",
	} {
		if definition, diagnostics := workflow.Check("say.yaml", []byte(step(with))); definition == nil || len(diagnostics) != 0 {
			t.Fatalf("%s: diagnostics = %+v", name, diagnostics)
		}
	}
	for name, with := range map[string]string{
		"no bot":  "      text: hello\n",
		"no text": "      bot: slack\n",
		"bad bot": "      bot: Slack!\n      text: hello\n",
	} {
		if _, err := workflow.Parse("say.yaml", []byte(step(with))); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	spec, ok := workflow.LookupExecutor("chat.message.send")
	if !ok || spec.Group != "Chat" || !slices.Contains(workflow.StepGroups, "Chat") {
		t.Fatalf("spec = %+v", spec)
	}
	var outputs []string
	for _, output := range spec.Outputs {
		outputs = append(outputs, output.Name)
	}
	if !slices.Equal(outputs, []string{"message_id", "channel", "thread_id"}) {
		t.Fatalf("outputs = %v", outputs)
	}
	if spec.With[0].Name != "bot" || spec.With[0].Kind != workflow.KindChatBot {
		t.Fatalf("first field = %+v", spec.With[0])
	}
}
