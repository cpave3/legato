package main

import (
	"fmt"
	"strings"
)

const agentPrimer = `# Legato CLI primer

Legato is a local kanban task manager and AI-agent orchestrator. Run commands as
` + "`legato <noun> <verb> [arguments] [flags]`" + `. Every command level accepts
` + "`--help`" + ` or ` + "`-h`" + `; ` + "`legato help`" + ` and ` + "`legato learn`" + ` print this primer.

## Core task workflow

` + "```bash" + `
# Create a local task; its generated ID is printed to stdout.
legato task create "Implement login" --description "Handle expired sessions" --status Backlog --priority High --workspace Personal

# Read task context. JSON is best for structured automation.
legato task show <task-id>
legato task show <task-id> --format full
legato task show <task-id> --format json

# Update one or several fields.
legato task update <task-id> --status Doing
legato task update <task-id> --title "Clarify login failure"
legato task update <task-id> --description "New Markdown details"
legato task update <task-id> --status Review --title "Ready for review"

# Append a timestamped note.
legato task note <task-id> "Implementation complete"
` + "```" + `

Task IDs are opaque: preserve them exactly. Status matching is case-insensitive,
but must name a configured board column. If create omits ` + "`--status`" + `, the
first configured column is used.

Jira-backed tasks treat Jira as authoritative. Their status may be changed, but
their title and description cannot be edited with Legato. Local task title,
description, and notes are editable.

## Agent sessions

Inside a Legato-launched agent session, ` + "`LEGATO_TASK_ID`" + ` identifies the
current task:

` + "```bash" + `
legato task show "$LEGATO_TASK_ID" --format full
legato agent state "$LEGATO_TASK_ID" --activity working
legato agent state "$LEGATO_TASK_ID" --activity waiting
` + "```" + `

Agent activity changes the UI indicator; it does not move the task between
columns. Use ` + "`legato task update ... --status ...`" + ` for that.

## Other command families

- ` + "`legato agent --help`" + ` — agent activity and session status
- ` + "`legato workspace --help`" + ` — discover configured workspace names
- ` + "`legato plan --help`" + ` — collaborative implementation plans
- ` + "`legato review --help`" + ` — review tours, findings, and annotations
- ` + "`legato swarm --help`" + ` — multi-agent orchestration
- ` + "`legato hooks --help`" + ` — install or remove AI-tool hooks
- ` + "`legato serve --help`" + ` — run the web server
- ` + "`legato auth --help`" + ` — manage the web authentication token
- ` + "`legato pair --help`" + ` — pair another device with the web UI

## Discovery and failure behavior

Use ` + "`legato <path> --help`" + ` before invoking an unfamiliar command. Help
prints to stdout and exits 0. Normal command results generally print to stdout;
diagnostics print to stderr and return a non-zero exit status. CLI mutations
broadcast a best-effort refresh to running Legato interfaces.
`

var commandSummaries = map[string]string{
	"":          "Legato task management and AI-agent orchestration",
	"task":      "Create, inspect, update, and connect tasks",
	"workspace": "Discover and manage task workspaces",
	"agent":     "Report and inspect AI-agent activity",
	"hooks":     "Install or uninstall AI-tool integration hooks",
	"serve":     "Run the Legato web server",
	"auth":      "Manage the web UI authentication token",
	"pair":      "Pair a device with the Legato web UI",
	"plan":      "Manage collaborative implementation plans",
	"review":    "Create and manage review tours",
	"swarm":     "Coordinate multi-agent swarms",
}

var commandChildren = map[string][]string{
	"":          {"task", "workspace", "agent", "plan", "review", "swarm", "hooks", "serve", "auth", "pair", "help", "learn"},
	"task":      {"create", "show", "update", "description", "note", "link", "unlink", "worktree"},
	"workspace": {"list"},
	"agent":     {"state", "summary", "status"},
	"hooks":     {"install", "uninstall"},
	"auth":      {"token", "regenerate"},
	"plan":      {"submit", "show", "feedback", "status", "answer", "complete", "withdraw"},
	"review":    {"chapter", "chapters", "annotate", "annotation", "answer", "ready", "show", "sync", "discard", "restart"},
	"swarm":     {"validate-plan", "propose-plan", "extend-plan", "cancel", "dispatch", "message", "broadcast", "close", "finish", "progress", "question", "built", "status", "inbox"},
}

var commandUsage = map[string]string{
	"task create":      `legato task create <title> [--description <text>] [--status <status>] [--priority <priority>] [--workspace <name>]`,
	"task show":        `legato task show <task-id> [--format description|full|json]`,
	"task update":      `legato task update <task-id> [--status <status>] [--title <title>] [--description <text>] [--workspace <name>]`,
	"task description": `legato task description <task-id> <text>`,
	"task note":        `legato task note <task-id> <message>`,
	"task link":        `legato task link <task-id> [--branch <branch>] [--repo <owner/repo>] [--sha <commit-sha>]`,
	"task unlink":      `legato task unlink <task-id>`,
	"task worktree":    `legato task worktree [set|clear] <task-id> ...`,
	"workspace list":   `legato workspace list [--format text|json]`,
	"agent state":      `legato agent state <task-id> --activity <working|waiting|""> [--working-dir <path>]`,
	"agent summary":    `legato agent summary [--exclude <task-id>]`,
	"agent status":     `legato agent status <task-id> --format tmux`,
	"hooks install":    `legato hooks install [--tool claude-code|staccato|chimera|codex|yggdrasil]`,
	"hooks uninstall":  `legato hooks uninstall [--tool claude-code|staccato|chimera|codex|yggdrasil]`,
	"serve":            `legato serve [--port <port>]`,
	"auth token":       `legato auth token`,
	"auth regenerate":  `legato auth regenerate`,
	"pair":             `legato pair [--port <port>]`,
}

func helpRequest(args []string) ([]string, bool) {
	for i, arg := range args {
		if arg == "--help" || arg == "-h" || (arg == "help" && i > 0) {
			return args[:i], true
		}
	}
	return nil, false
}

func helpText(path []string) string {
	key := strings.Join(path, " ")
	name := "legato"
	if key != "" {
		name += " " + key
	}
	summary := commandSummaries[key]
	if summary == "" {
		if parent := strings.Join(path[:max(0, len(path)-1)], " "); parent != "" {
			summary = commandSummaries[parent]
		}
	}
	if summary == "" {
		summary = "Legato command"
	}

	usage := commandUsage[key]
	if usage == "" {
		usage = name
		if len(commandChildren[key]) > 0 {
			usage += " <command> [options]"
		} else {
			usage += " [options]"
		}
	}

	var out strings.Builder
	fmt.Fprintf(&out, "%s\n\nUsage:\n  %s\n", summary, usage)
	if children := commandChildren[key]; len(children) > 0 {
		out.WriteString("\nCommands:\n")
		for _, child := range children {
			fmt.Fprintf(&out, "  %s\n", child)
		}
	}
	if key == "task update" {
		out.WriteString(`
Fields:
  --status       Move any task to a configured column
  --title        Replace a local task's title (rejected for Jira-backed tasks)
  --description  Replace a local task's description (rejected for Jira-backed tasks)
  --workspace    Assign by workspace name; use "none" or "unassigned" to clear
`)
	}
	out.WriteString("\nRun `legato help` for the AI-agent primer.\n")
	return out.String()
}
