package main

import (
	"fmt"
	"strings"
)

func helpRequest(args []string) ([]string, bool) {
	for i, arg := range args {
		if arg == "--help" || arg == "-h" || (arg == "help" && i > 0) {
			return args[:i], true
		}
	}
	return nil, false
}

func helpText(path []string) string {
	return helpTextFor(commandTree(), path)
}

func helpTextFor(root *commandDef, path []string) string {
	node, ok := findCommand(root, path)
	if !ok {
		return fmt.Sprintf("Unknown command: legato %s\n", strings.Join(path, " "))
	}
	name := strings.TrimSpace("legato " + strings.Join(path, " "))
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n\nUsage:\n  %s", node.Summary, name)
	if len(node.Children) > 0 {
		if node.Run != nil {
			for _, argument := range node.Arguments {
				token := "<" + argument.Name + ">"
				if argument.Optional {
					token = "[" + token + "]"
				}
				fmt.Fprintf(&out, " %s", token)
			}
			out.WriteString(" | <command>")
		} else {
			out.WriteString(" <command>")
		}
	} else {
		for _, argument := range node.Arguments {
			token := "<" + argument.Name + ">"
			if argument.Repeatable {
				token += "..."
			}
			if argument.Optional {
				token = "[" + token + "]"
			}
			fmt.Fprintf(&out, " %s", token)
		}
		if len(node.Flags) > 0 {
			out.WriteString(" [options]")
		}
	}
	out.WriteString("\n")
	if node.Description != "" {
		fmt.Fprintf(&out, "\n%s\n", node.Description)
	}
	if len(node.Children) > 0 {
		out.WriteString("\nCommands:\n")
		for _, child := range node.Children {
			fmt.Fprintf(&out, "  %-18s %s\n", child.Name, child.Summary)
		}
	}
	if len(node.Arguments) > 0 {
		out.WriteString("\nArguments:\n")
		for _, argument := range node.Arguments {
			fmt.Fprintf(&out, "  %-18s %s\n", argument.Name, argument.Description)
		}
	}
	if len(node.Flags) > 0 {
		out.WriteString("\nOptions:\n")
		for _, flag := range node.Flags {
			label := "--" + flag.Name
			if !flag.Boolean {
				label += " <" + flag.Value + ">"
			}
			if flag.Repeatable {
				label += " (repeatable)"
			}
			fmt.Fprintf(&out, "  %-28s %s\n", label, flag.Description)
		}
	}
	if node.Capabilities.Integration {
		out.WriteString("\nIntegration command: intended for installed tool hooks and adapters.\n")
	}
	examples := helpExamples(node, name)
	if len(examples) > 0 {
		if len(node.Children) > 0 {
			out.WriteString("\nExamples (run subcommand help for more specific examples):\n")
		} else {
			out.WriteString("\nExamples:\n")
		}
		for _, example := range examples {
			fmt.Fprintf(&out, "  %s\n", example.Command)
			if example.More > 0 {
				fmt.Fprintf(&out, "    (and %d more; run `%s --help`)\n", example.More, example.HelpPath)
			}
		}
	}
	out.WriteString("\nRun `legato help` for the AI-agent primer.\n")
	return out.String()
}

type helpExample struct {
	Command  string
	More     int
	HelpPath string
}

const parentExamplePreviewLimit = 3

func helpExamples(node *commandDef, name string) []helpExample {
	if len(node.Children) == 0 {
		examples := make([]helpExample, 0, len(node.Examples))
		for _, example := range node.Examples {
			examples = append(examples, helpExample{Command: example})
		}
		return examples
	}

	examples := make([]helpExample, 0, len(node.Children)*parentExamplePreviewLimit)
	for _, child := range node.Children {
		previewCount := min(len(child.Examples), parentExamplePreviewLimit)
		for i, command := range child.Examples[:previewCount] {
			example := helpExample{Command: command}
			if i == previewCount-1 {
				example.More = len(child.Examples) - previewCount
				example.HelpPath = name + " " + child.Name
			}
			examples = append(examples, example)
		}
	}
	return examples
}

var agentPrimer = agentPrimerText(commandTree())

func agentPrimerText(root *commandDef) string {
	var out strings.Builder
	out.WriteString(`# Legato CLI primer

Legato is a local kanban task manager and AI-agent orchestrator. Commands use
` + "`legato <noun> <verb> [arguments] [flags]`" + `. Every command level accepts
` + "`--help`" + ` or ` + "`-h`" + `.

## Core task workflow

` + "```bash\n")
	for _, path := range [][]string{{"task", "list"}, {"task", "create"}, {"task", "show"}, {"task", "update"}, {"task", "note"}} {
		if node, ok := findCommand(root, path); ok && len(node.Examples) > 0 {
			out.WriteString(node.Examples[0] + "\n")
		}
	}
	out.WriteString("```\n\n")
	out.WriteString(`Task IDs are opaque: preserve them exactly. Status names must match a configured
board column. Jira-backed task titles and provider descriptions are read-only,
but their Legato-owned Markdown details remain editable.

Inside an agent session, task-scoped commands can use ` + "`LEGATO_TASK_ID`" + ` when
the task ID is omitted. Agent activity changes the UI indicator; use
` + "`task update --status`" + ` to move the task itself.

Plan and review feedback is durable. If you take over an existing artifact from
another agent, run ` + "`legato plan claim --task <board-task-id>`" + ` or
` + "`legato review claim --task <board-task-id>`" + `. When Legato notifies you
about pending interactions, drain them with ` + "`legato plan inbox`" + ` (the
plan and review inbox commands read the same agent-scoped queue).

A plan, a review chapter and an answer are Markdown, and can show image
files such as a screenshot. In plan.md, name a file of the plan bundle:
` + "`![The board today](assets/board.png)`" + `. For a chapter or an answer,
write the Markdown to a file and name the image beside it, then pass
` + "`legato review chapter <title> --narration-file <path> --include <path>:<hunk>`" + `,
` + "`legato review answer <step-id> --file <path>`" + ` or
` + "`legato plan answer <thread-id> --file <path>`" + `.

## Command families

`)
	for _, child := range root.Children {
		fmt.Fprintf(&out, "- `%s --help` — %s\n", strings.TrimSpace("legato "+child.Name), child.Summary)
	}
	out.WriteString(`
## Discovery and failure behavior

Use ` + "`legato <path> --help`" + ` before invoking an unfamiliar command. Help
prints to stdout and exits 0. Results print to stdout; diagnostics print to
stderr. Where advertised, ` + "`--json`" + ` emits a stable result or error envelope.
`)
	return out.String()
}
