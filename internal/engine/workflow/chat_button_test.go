package workflow_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

const buttonWorkflow = `
schema_version: 1
name: fix
title: Fix
inputs:
  task: {type: string}
on:
TRIGGER
steps:
  - id: reply
    title: Reply
    type: chat.message.send
    with:
      bot: slack
      channel: C1
      text: seen
`

func parseButton(t *testing.T, trigger string) (*workflow.Definition, error) {
	t.Helper()
	return workflow.Parse("fix.yaml", []byte(strings.Replace(buttonWorkflow, "TRIGGER\n", trigger, 1)))
}

func TestAChatButtonTriggerParsesAndLists(t *testing.T) {
	definition, err := parseButton(t, `  chat.button:
    bot: slack
    channel: C1
    user: U7
    value: {regex: "^fix:(?P<task>.+)$"}
    max_runs_per_day: 2
    inputs:
      task: ${{ event.match.task }}
`)
	if err != nil {
		t.Fatal(err)
	}
	list := definition.On.List()
	if len(list) != 1 || list[0].Kind != workflow.TriggerChatButton || list[0].MaxRunsPerDay != 2 || !list[0].EnabledByDefault {
		t.Fatalf("triggers = %+v", list)
	}
	button := definition.On.ChatButton
	if button == nil || button.Bot != "slack" || button.User != "U7" || button.Value == nil || button.Value.Regex == "" {
		t.Fatalf("chat.button = %+v", button)
	}
}

func TestAChatButtonTriggerIsValidated(t *testing.T) {
	for name, trigger := range map[string]string{
		"no bot":           "  chat.button:\n    user: U1\n",
		"bad bot":          "  chat.button:\n    bot: Slack Bot\n",
		"bad regex":        "  chat.button:\n    bot: slack\n    value: {regex: \"(\"}\n",
		"both matches":     "  chat.button:\n    bot: slack\n    value: {contains: a, regex: b}\n",
		"empty match":      "  chat.button:\n    bot: slack\n    value: {}\n",
		"undeclared input": "  chat.button:\n    bot: slack\n    inputs: {nope: x}\n",
		"negative max":     "  chat.button:\n    bot: slack\n    max_runs_per_day: -1\n",
		"a message key":    "  chat.button:\n    bot: slack\n    text: {contains: x}\n",
	} {
		if _, err := parseButton(t, trigger); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	_, err := parseButton(t, "  chat.button:\n    bot: slack\n    value: {regex: \"(\"}\n")
	if err == nil || !strings.Contains(err.Error(), "value regex") {
		t.Errorf("err = %v, want it to name the value", err)
	}
}

func TestAChatButtonTriggerWithoutAUserIsAWarning(t *testing.T) {
	text := strings.Replace(buttonWorkflow, "TRIGGER\n", "  chat.button:\n    bot: slack\n", 1)
	definition, diagnostics := workflow.Check("fix.yaml", []byte(text))
	if definition == nil {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	if len(diagnostics) != 1 || diagnostics[0].Severity != workflow.SeverityWarning ||
		!strings.Contains(diagnostics[0].Message, "anyone") || diagnostics[0].Line != 9 {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	text = strings.Replace(buttonWorkflow, "TRIGGER\n", "  chat.button:\n    bot: slack\n    user: U7\n", 1)
	if _, diagnostics := workflow.Check("fix.yaml", []byte(text)); len(diagnostics) != 0 {
		t.Fatalf("with a user: diagnostics = %+v", diagnostics)
	}
}

func TestAChatButtonTriggerMatchesClicks(t *testing.T) {
	click := workflow.ChatClickFields{Bot: "slack", Channel: "C1", User: "U7", Value: "fix:LEG-1", MessageID: "5.5"}
	cases := []struct {
		name    string
		trigger workflow.ChatButtonTrigger
		click   workflow.ChatClickFields
		want    workflow.ClickMatch
	}{
		{"bot only", workflow.ChatButtonTrigger{Bot: "slack"}, click, workflow.ClickMatched},
		{"other bot", workflow.ChatButtonTrigger{Bot: "discord"}, click, workflow.ClickUnmatched},
		{"other channel", workflow.ChatButtonTrigger{Bot: "slack", Channel: "C2"}, click, workflow.ClickUnmatched},
		{"contains ignores case", workflow.ChatButtonTrigger{Bot: "slack", Value: &workflow.ChatTextMatch{Contains: "FIX:"}}, click, workflow.ClickMatched},
		{"contains misses", workflow.ChatButtonTrigger{Bot: "slack", Value: &workflow.ChatTextMatch{Contains: "dismiss"}}, click, workflow.ClickUnmatched},
		{"regex", workflow.ChatButtonTrigger{Bot: "slack", Value: &workflow.ChatTextMatch{Regex: "^fix:"}}, click, workflow.ClickMatched},
		{"the user", workflow.ChatButtonTrigger{Bot: "slack", User: "U7"}, click, workflow.ClickMatched},
		{"another user", workflow.ChatButtonTrigger{Bot: "slack", User: "U8"}, click, workflow.ClickRefused},
		{"another user, other value", workflow.ChatButtonTrigger{Bot: "slack", User: "U8", Value: &workflow.ChatTextMatch{Contains: "dismiss"}}, click, workflow.ClickUnmatched},
	}
	for _, tc := range cases {
		if got := tc.trigger.Match(tc.click); got != tc.want {
			t.Errorf("%s: Match = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAChatButtonTriggersRegexGroupsAreItsMatch(t *testing.T) {
	trigger := workflow.ChatButtonTrigger{Bot: "slack", Value: &workflow.ChatTextMatch{Regex: `^(?P<action>\w+):(?P<task>.+)$`}}
	got := trigger.Groups(workflow.ChatClickFields{Value: "fix:LEG-1"})
	if !reflect.DeepEqual(got, map[string]string{"action": "fix", "task": "LEG-1"}) {
		t.Fatalf("groups = %v", got)
	}
	if got := (workflow.ChatButtonTrigger{Bot: "slack", Value: &workflow.ChatTextMatch{Contains: "fix"}}).Groups(workflow.ChatClickFields{Value: "fix:1"}); len(got) != 0 {
		t.Fatalf("contains groups = %v", got)
	}
}

func TestAChatButtonTriggerCountsRunsPerMessage(t *testing.T) {
	click := workflow.ChatClickFields{Bot: "slack", Channel: "C1", MessageID: "5.5"}
	if got := (workflow.ChatButtonTrigger{Bot: "slack"}).ItemFor(click); got != "slack:C1/5.5" {
		t.Fatalf("item = %q", got)
	}
}

func buttonsStep(buttons string) string {
	return `schema_version: 1
name: check
title: Check
inputs:
  x: {type: string}
  buttons: {type: json}
steps:
  - id: say
    title: Say
    type: chat.message.send
    with:
      bot: slack
      channel: C1
      text: Findings
      buttons:
` + buttons
}

func TestAChatSendStepsButtonsAreChecked(t *testing.T) {
	for name, buttons := range map[string]string{
		"three": `        - {label: "🛠 Fix it", value: "fix:${{ workflow.inputs.x }}", style: primary}
        - {label: Ticket, value: ticket}
        - {label: Dismiss, value: dismiss, style: danger}
`,
		"an expression label": "        - {label: \"${{ workflow.inputs.x }}\", value: a}\n",
		"the whole list":      "        ${{ workflow.inputs.buttons }}\n",
	} {
		if definition, diagnostics := workflow.Check("say.yaml", []byte(buttonsStep(buttons))); definition == nil || len(diagnostics) != 0 {
			t.Errorf("%s: diagnostics = %+v", name, diagnostics)
		}
	}
	for name, tc := range map[string]struct{ buttons, says string }{
		"not a list":     {"        {label: a, value: b}\n", "a list"},
		"empty":          {"        []\n", "1 to 5"},
		"six":            {strings.Repeat("        - {label: a, value: b}\n", 6), "1 to 5"},
		"no label":       {"        - {value: b}\n", "buttons[0]: label"},
		"no value":       {"        - {label: a, value: x}\n        - {label: b}\n", "buttons[1]: value"},
		"unknown key":    {"        - {label: a, value: b, url: x}\n", "buttons[0]: no field \"url\""},
		"bad style":      {"        - {label: a, value: b, style: big}\n", "primary or danger"},
		"long label":     {"        - {label: " + strings.Repeat("a", 76) + ", value: b}\n", "75"},
		"long value":     {"        - {label: a, value: " + strings.Repeat("b", 101) + "}\n", "100"},
		"same value":     {"        - {label: a, value: b}\n        - {label: c, value: b}\n", "buttons[1]: value \"b\""},
		"not a map":      {"        - fix\n", "buttons[0]"},
		"a number value": {"        - {label: a, value: 3}\n", "buttons[0]: value"},
	} {
		_, err := workflow.Parse("say.yaml", []byte(buttonsStep(tc.buttons)))
		if err == nil || !strings.Contains(err.Error(), tc.says) {
			t.Errorf("%s: err = %v, want it to say %q", name, err, tc.says)
		}
	}
}

func TestTheChatButtonsFieldIsInTheCatalog(t *testing.T) {
	spec, _ := workflow.LookupExecutor("chat.message.send")
	field, ok := spec.Field("buttons")
	if !ok || field.Kind != workflow.KindChatButtons || field.Required {
		t.Fatalf("buttons field = %+v, %v", field, ok)
	}
}

func TestTheSchemaChecksButtonsAndTheButtonTrigger(t *testing.T) {
	schema := resolvedSchema(t)
	trigger := func(body string) string {
		return strings.Replace(buttonWorkflow, "TRIGGER\n", body, 1)
	}
	for name, text := range map[string]string{
		"buttons":          buttonsStep("        - {label: Fix, value: \"fix:${{ workflow.inputs.x }}\", style: primary}\n        - {label: No, value: no}\n"),
		"buttons by input": buttonsStep("        ${{ workflow.inputs.buttons }}\n"),
		"a button trigger": trigger("  chat.button:\n    bot: slack\n    user: U7\n    value: {regex: \"^fix:(?P<task>.+)$\"}\n    inputs: {task: \"${{ event.match.task }}\"}\n"),
	} {
		if _, err := workflow.Parse("s.yaml", []byte(text)); err != nil {
			t.Fatalf("%s: the engine refuses it: %v", name, err)
		}
		if err := schema.Validate(asJSON(t, []byte(text))); err != nil {
			t.Errorf("%s: the schema refuses it: %v", name, err)
		}
	}
	for name, text := range map[string]string{
		"six buttons":       buttonsStep(strings.Repeat("        - {label: a, value: b}\n", 6)),
		"a button's url":    buttonsStep("        - {label: a, value: b, url: x}\n"),
		"no value":          buttonsStep("        - {label: a}\n"),
		"a big style":       buttonsStep("        - {label: a, value: b, style: big}\n"),
		"a long value":      buttonsStep("        - {label: a, value: " + strings.Repeat("b", 101) + "}\n"),
		"a trigger's text":  trigger("  chat.button:\n    bot: slack\n    text: {contains: x}\n"),
		"a trigger, no bot": trigger("  chat.button:\n    user: U7\n"),
	} {
		if err := schema.Validate(asJSON(t, []byte(text))); err == nil {
			t.Errorf("%s: the schema accepts it", name)
		}
	}
}

func TestTheGraphSaysWhatAButtonTriggerWaitsFor(t *testing.T) {
	definition, err := parseButton(t, "  chat.button:\n    bot: discord\n    channel: C1\n    user: U7\n    value: {contains: \"fix:\"}\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range workflow.BuildGraph(definition).Nodes {
		if node.ID == "trigger:"+workflow.TriggerChatButton {
			if node.Trigger.Summary != "Button fix:… in C1 on discord" || node.Trigger.Definition == nil {
				t.Fatalf("node = %+v", node.Trigger)
			}
			return
		}
	}
	t.Fatal("no chat.button node")
}

func TestChatBotDiagnosticsCheckTheButtonTriggersBot(t *testing.T) {
	data := []byte(strings.Replace(buttonWorkflow, "TRIGGER\n", "  chat.button:\n    bot: teams\n    user: U7\n", 1))
	definition, diagnostics := workflow.Check("fix.yaml", data)
	if definition == nil {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	got := workflow.ChatBotDiagnostics(data, definition, func(name string) (workflow.ChatBotState, bool) {
		return workflow.ChatBotState{Configured: true, Enabled: true}, name == "slack"
	})
	if len(got) != 1 || got[0].Line != 9 || !strings.Contains(got[0].Message, `"teams"`) {
		t.Fatalf("diagnostics = %+v", got)
	}
}
