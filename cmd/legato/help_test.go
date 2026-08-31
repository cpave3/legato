package main

import (
	"strings"
	"testing"
)

func TestHelpRequestAtEveryLevel(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, ""},
		{[]string{"task", "--help"}, "task"},
		{[]string{"task", "update", "--help"}, "task update"},
		{[]string{"task", "update", "-h"}, "task update"},
		{[]string{"swarm", "propose-plan", "help"}, "swarm propose-plan"},
	}
	for _, tt := range tests {
		path, ok := helpRequest(tt.args)
		if !ok {
			t.Fatalf("helpRequest(%v) did not detect help", tt.args)
		}
		if got := strings.Join(path, " "); got != tt.want {
			t.Errorf("helpRequest(%v) path = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestPublicHelpIncludesUsefulExamplesAtEveryKindOfCommandLevel(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{name: "root", args: []string{"--help"}, want: []string{"Examples (run subcommand help for more specific examples):", "legato task list"}},
		{name: "group", args: []string{"task", "--help"}, want: []string{"Commands:", "Examples (run subcommand help for more specific examples):", "legato task list", "legato task update --status Doing", `legato task update <task-id> --title "Clarify failure"`, "legato task github refresh <task-id>", "legato task archive list"}},
		{name: "runnable group", args: []string{"workflow", "step", "--help"}, want: []string{"legato workflow step complete --output result=value --artifact review=review.md --json", `legato workflow step fail "reason" --json`}},
		{name: "legacy leaf", args: []string{"plan", "submit", "--help"}, want: []string{"<bundle-dir>", "Examples:", "legato plan submit .legato/plans/search"}},
		{name: "plan validate", args: []string{"plan", "validate", "--help"}, want: []string{"Validate", "<markdown-file>", "Plan Markdown file to validate", "Examples:", "legato plan validate plan.md", "legato plan validate .legato/plans/search/plan.md"}},
		{name: "configured swarm leaf", args: []string{"swarm", "propose-plan", "--help"}, want: []string{"<plan-file>", "--timeout", "legato swarm propose-plan swarm.yaml"}},
		{name: "nested complete callback", args: []string{"workflow", "step", "complete", "--help"}, want: []string{"--output", "--artifact", "legato workflow step complete"}},
		{name: "nested fail callback", args: []string{"workflow", "step", "fail", "--help"}, want: []string{"<reason>", "--attempt", `legato workflow step fail "reason"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() int {
				code := runCLI(tt.args)
				if code != 0 {
					t.Errorf("runCLI(%v) exit = %d, want 0", tt.args, code)
				}
				return code
			})
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("help missing %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestParentHelpShowsAllChildExamplesWhenThereAreAtMostThree(t *testing.T) {
	out := helpText([]string{"task"})
	for _, want := range []string{
		"legato task update --status Doing",
		`legato task update <task-id> --title "Clarify failure"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("parent help missing child example %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "run `legato task update --help`") {
		t.Errorf("parent help points to more update examples when all are shown:\n%s", out)
	}
}

func TestParentHelpTruncatesChildExamplesAfterThree(t *testing.T) {
	root := group("", "Root", []string{"legato child one"},
		leaf("child", "Child", func([]string) int { return 0 }, nil, nil,
			"legato child one", "legato child two", "legato child three", "legato child four"))
	out := helpTextFor(root, nil)
	for _, want := range []string{"legato child one", "legato child two", "legato child three", "(and 1 more; run `legato child --help`)"} {
		if !strings.Contains(out, want) {
			t.Errorf("parent help missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "legato child four") {
		t.Errorf("parent help includes child example beyond preview limit:\n%s", out)
	}
}

func TestTaskUpdateHelpExplainsProviderRestrictions(t *testing.T) {
	out := helpText([]string{"task", "update"})
	for _, want := range []string{"--status", "--title", "--description", "Provider-backed", "Legato-owned"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
}

func TestReflectionSubmitHelpDocumentsJSONContract(t *testing.T) {
	out := captureStdout(t, func() int {
		code := runCLI([]string{"learn", "reflection", "submit", "--help"})
		if code != 0 {
			t.Errorf("reflection submit help exit = %d, want 0", code)
		}
		return code
	})
	for _, want := range []string{
		`"schema_version": 1`,
		`"observations"`,
		`"kind"`,
		`"content"`,
		`"rationale"`,
		`"metadata"`,
		"Each observation requires kind and content",
		"Recommended kind values:",
		"An empty observations array is valid",
		`printf '%s\n' '{"schema_version":1,"observations":[]}' | legato learn reflection submit --request <id> --file -`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
}

func TestAgentPrimerCoversDiscoveryAndCoreWorkflow(t *testing.T) {
	for _, want := range []string{
		"# Legato CLI primer",
		"legato task create",
		"legato task update",
		"LEGATO_TASK_ID",
		"legato <path> --help",
	} {
		if !strings.Contains(agentPrimer, want) {
			t.Errorf("primer missing %q", want)
		}
	}
}
