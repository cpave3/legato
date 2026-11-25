package workflow_test

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func chatDefinition(t *testing.T, name, on string) (*workflow.Definition, []byte) {
	t.Helper()
	data := []byte("schema_version: 1\nname: " + name + "\ntitle: " + name + "\non:\n" + on +
		"steps:\n  - {id: say, title: Say, type: chat.message.send, with: {bot: slack, channel: C1, text: hi}}\n")
	definition, err := workflow.Parse(name+".yaml", data)
	if err != nil {
		t.Fatal(err)
	}
	return definition, data
}

func TestAFallbackBehindAnUnfilteredChatTriggerIsWarned(t *testing.T) {
	help, data := chatDefinition(t, "help", "  chat.mention:\n    bot: slack\n    fallback: true\n")
	everything := func(on string) *workflow.Definition {
		definition, _ := chatDefinition(t, "log", on)
		return definition
	}
	on := func(*workflow.Definition, string) bool { return true }
	cases := []struct {
		name  string
		other *workflow.Definition
		on    func(*workflow.Definition, string) bool
		warn  bool
	}{
		{"an unfiltered chat.message", everything("  chat.message:\n    bot: slack\n"), on, true},
		{"an unfiltered chat.mention", everything("  chat.mention:\n    bot: slack\n"), on, true},
		{"a paused one", everything("  chat.message:\n    bot: slack\n"), func(*workflow.Definition, string) bool { return false }, false},
		{"one on another bot", everything("  chat.message:\n    bot: discord\n"), on, false},
		{"one with a channel", everything("  chat.message:\n    bot: slack\n    channel: C1\n"), on, false},
		{"one with a user", everything("  chat.message:\n    bot: slack\n    user: U1\n"), on, false},
		{"one with text", everything("  chat.message:\n    bot: slack\n    text: {contains: log}\n"), on, false},
		{"another fallback", everything("  chat.message:\n    bot: slack\n    fallback: true\n"), on, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := workflow.ChatFallbackDiagnostics(data, help, []*workflow.Definition{help, c.other}, c.on)
			if !c.warn {
				if len(got) != 0 {
					t.Fatalf("diagnostics = %+v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Severity != workflow.SeverityWarning || got[0].Path != "on.chat.mention.fallback" ||
				!strings.Contains(got[0].Message, "never fire") || !strings.Contains(got[0].Message, "log") {
				t.Fatalf("diagnostics = %+v", got)
			}
		})
	}
}

func TestAChatMessageFallbackIsNotWarnedOfAnUnfilteredMention(t *testing.T) {
	help, data := chatDefinition(t, "help", "  chat.message:\n    bot: slack\n    fallback: true\n")
	log, _ := chatDefinition(t, "log", "  chat.mention:\n    bot: slack\n")
	if got := workflow.ChatFallbackDiagnostics(data, help, []*workflow.Definition{log}, func(*workflow.Definition, string) bool { return true }); len(got) != 0 {
		t.Fatalf("diagnostics = %+v", got)
	}
}

func TestTheWorkflowsOwnUnfilteredMessageTriggerHidesItsFallback(t *testing.T) {
	help, data := chatDefinition(t, "help", "  chat.message:\n    bot: slack\n  chat.mention:\n    bot: slack\n    fallback: true\n")
	// An older copy of the same workflow in the list is the file being
	// edited: only the definition given counts.
	older, _ := chatDefinition(t, "help", "  chat.mention:\n    bot: slack\n    fallback: true\n")
	got := workflow.ChatFallbackDiagnostics(data, help, []*workflow.Definition{older}, func(*workflow.Definition, string) bool { return true })
	if len(got) != 1 || !strings.Contains(got[0].Message, "help") {
		t.Fatalf("diagnostics = %+v", got)
	}
}

func TestAFallbackForOtherBotsMessagesIsNotWarned(t *testing.T) {
	help, data := chatDefinition(t, "help", "  chat.mention:\n    bot: slack\n    fallback: true\n    include_bots: true\n")
	log, _ := chatDefinition(t, "log", "  chat.message:\n    bot: slack\n")
	if got := workflow.ChatFallbackDiagnostics(data, help, []*workflow.Definition{log}, func(*workflow.Definition, string) bool { return true }); len(got) != 0 {
		t.Fatalf("diagnostics = %+v", got)
	}
}
