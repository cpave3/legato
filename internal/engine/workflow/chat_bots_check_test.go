package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func TestChatBotDiagnosticsNameBotsThisLegatoLacks(t *testing.T) {
	data := []byte(`schema_version: 1
name: ops
title: Ops
inputs:
  bot: {type: string}
on:
  chat.message:
    bot: slack
  chat.mention:
    bot: teams
steps:
  - {id: a, title: A, type: chat.message.send, with: {bot: discord, channel: C1, text: hi}}
  - {id: b, title: B, type: chat.message.send, with: {bot: "${{ workflow.inputs.bot }}", channel: C1, text: hi}}
`)
	definition, diagnostics := workflow.Check("ops.yaml", data)
	if definition == nil {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
	bots := map[string]workflow.ChatBotState{
		"slack":   {Configured: true, Enabled: false},
		"discord": {Configured: false},
	}
	got := workflow.ChatBotDiagnostics(data, definition, func(name string) (workflow.ChatBotState, bool) {
		state, ok := bots[name]
		return state, ok
	})
	if len(got) != 3 {
		t.Fatalf("diagnostics = %+v", got)
	}
	want := []struct {
		path     string
		severity workflow.Severity
		line     int
		says     string
	}{
		{"on.chat.message.bot", workflow.SeverityWarning, 8, "switched off"},
		{"on.chat.mention.bot", workflow.SeverityError, 10, `no bot named "teams"`},
		{"steps[0].with.bot", workflow.SeverityWarning, 12, "not set up"},
	}
	for i, w := range want {
		if got[i].Path != w.path || got[i].Severity != w.severity || got[i].Line != w.line || !strings.Contains(got[i].Message, w.says) {
			t.Errorf("diagnostic %d = %+v, want %+v", i, got[i], w)
		}
	}
}
