package workflow_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

// chat.thread.read reads the thread a chat message is in (LEG-635), with the
// bot and channel fields of chat.message.send.
func TestAChatThreadStepNamesItsBotAndThread(t *testing.T) {
	step := func(with string) string {
		return checkBase + `steps:
  - id: thread
    title: Thread
    type: chat.thread.read
    with:
` + with
	}
	for name, with := range map[string]string{
		"all":        "      bot: slack\n      channel: C1\n      thread_id: \"1.5\"\n      limit: 20\n      include_bots: true\n",
		"no channel": "      bot: discord\n      thread_id: m1\n",
		"templates":  "      bot: ${{ workflow.inputs.task.metadata.bot }}\n      thread_id: ${{ workflow.inputs.task.metadata.thread }}\n",
	} {
		if definition, diagnostics := workflow.Check("thread.yaml", []byte(step(with))); definition == nil || len(diagnostics) != 0 {
			t.Fatalf("%s: diagnostics = %+v", name, diagnostics)
		}
	}
	for name, with := range map[string]string{
		"no bot":           "      thread_id: m1\n",
		"no thread":        "      bot: slack\n",
		"bad bot":          "      bot: Slack!\n      thread_id: m1\n",
		"include_bots fed": "      bot: slack\n      thread_id: m1\n      include_bots: ${{ workflow.inputs.task.metadata.b }}\n",
	} {
		if _, err := workflow.Parse("thread.yaml", []byte(step(with))); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	spec, ok := workflow.LookupExecutor("chat.thread.read")
	if !ok || spec.Group != "Chat" {
		t.Fatalf("spec = %+v", spec)
	}
	send, _ := workflow.LookupExecutor("chat.message.send")
	if !reflect.DeepEqual(spec.With[:2], send.With[:2]) {
		t.Fatalf("bot and channel = %+v, %+v; want send's %+v, %+v", spec.With[0], spec.With[1], send.With[0], send.With[1])
	}
	var fields, outputs []string
	for _, field := range spec.With {
		fields = append(fields, field.Name)
	}
	for _, output := range spec.Outputs {
		outputs = append(outputs, output.Name+":"+output.Type)
	}
	if !slices.Equal(fields, []string{"bot", "channel", "thread_id", "limit", "include_bots"}) {
		t.Fatalf("fields = %v", fields)
	}
	if !slices.Equal(outputs, []string{"transcript:string", "messages:json", "count:integer"}) {
		t.Fatalf("outputs = %v", outputs)
	}
}

func TestChatBotDiagnosticsCheckAThreadReadsBot(t *testing.T) {
	data := []byte(checkBase + `steps:
  - {id: thread, title: Thread, type: chat.thread.read, with: {bot: teams, thread_id: "1.5"}}
`)
	definition, diagnostics := workflow.Check("thread.yaml", data)
	if definition == nil {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	got := workflow.ChatBotDiagnostics(data, definition, func(string) (workflow.ChatBotState, bool) { return workflow.ChatBotState{}, false })
	if len(got) != 1 || got[0].Path != "steps[0].with.bot" || !strings.Contains(got[0].Message, `no bot named "teams"`) {
		t.Fatalf("diagnostics = %+v", got)
	}
}
