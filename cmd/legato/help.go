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
		out.WriteString(" <command>")
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
	if len(node.Examples) > 0 {
		out.WriteString("\nExamples:\n")
		for _, example := range node.Examples {
			fmt.Fprintf(&out, "  %s\n", example)
		}
	}
	out.WriteString("\nRun `legato help` for the AI-agent primer.\n")
	return out.String()
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
	for _, path := range [][]string{{"task", "create"}, {"task", "show"}, {"task", "update"}, {"task", "note"}} {
		if node, ok := findCommand(root, path); ok && len(node.Examples) > 0 {
			out.WriteString(node.Examples[0] + "\n")
		}
	}
	out.WriteString("```\n\n")
	out.WriteString(`Task IDs are opaque: preserve them exactly. Status names must match a configured
board column. Jira-backed tasks may be moved between statuses, but their title
and description are read-only in Legato.

Inside an agent session, task-scoped commands can use ` + "`LEGATO_TASK_ID`" + ` when
the task ID is omitted. Agent activity changes the UI indicator; use
` + "`task update --status`" + ` to move the task itself.

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
