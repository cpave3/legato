package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/cli"
)

// legatoCommandClass is what running one legato subcommand on the host
// needs when the Reeve asks for it, and a plain description for approval
// cards. The table is reeve_command_table.go.
type legatoCommandClass struct {
	Class       string
	Description string
}

// legatoCommandOutputCap is how much of a command's output the Reeve gets.
const legatoCommandOutputCap = 64 << 10

// classifyLegatoCommand classes the subcommand args name. A command the
// table does not class, or args that name none, is never run.
func classifyLegatoCommand(args []string) (class, description string) {
	if len(args) == 0 {
		return "never", "No command"
	}
	node, path, _, err := resolveCommand(commandTree(), args)
	if err != nil || node == nil || len(node.Children) > 0 {
		return "never", "Not a legato command: legato " + strings.Join(args, " ") + ". " + legatoInstead(args, path, node)
	}
	entry, ok := legatoCommandClasses[strings.Join(path, " ")]
	if !ok {
		return "never", "A legato command the Reeve has no class for: " + strings.Join(path, " ")
	}
	description = entry.Description + " (legato " + strings.Join(args, " ") + ")"
	if entry.Class == "never" {
		description += ": the Reeve never runs it. Instead: " + legatoCommandAlternatives[strings.Join(path, " ")]
	}
	return entry.Class, description
}

// legatoInstead says what to run instead of args, which name no command:
// the alternative for what was asked, and the commands of the group it
// reached.
func legatoInstead(args, path []string, node *commandDef) string {
	var out []string
	for n := len(args); n > 0; n-- {
		if alt := legatoCommandAlternatives[strings.Join(args[:n], " ")]; alt != "" {
			out = append(out, "Instead: "+alt+".")
			break
		}
	}
	if node != nil && len(path) > 0 && len(node.Children) > 0 {
		var runs []string
		for _, c := range (legatoCommands{}).Commands() {
			if strings.HasPrefix(c.Path, strings.Join(path, " ")+" ") {
				runs = append(runs, c.Path+" ("+c.Class+")")
			}
		}
		if len(runs) > 0 {
			out = append(out, "Under legato "+strings.Join(path, " ")+" you may run: "+strings.Join(runs, ", ")+".")
		}
	}
	if len(out) == 0 {
		out = append(out, "The legato tool's description lists the commands it runs; for the API, legato_api, whose routes are on the manual page reference/api.")
	}
	return strings.Join(out, " ")
}

// runLegatoCommand runs one legato subcommand on the host, as this
// process's own binary with no shell, in the home folder, for at most two
// minutes. Its output is capped.
func runLegatoCommand(ctx context.Context, args []string) (int, string, error) {
	if class, description := classifyLegatoCommand(args); class == "never" {
		return 0, "", fmt.Errorf("refused: %s", description)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, launchExecutable, args...)
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	cmd.Env = hostCommandEnv()
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	text := out.String()
	if len(text) > legatoCommandOutputCap {
		text = text[:legatoCommandOutputCap] + "\n… (output cut at 64 KB)"
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), text, nil
	}
	if err != nil {
		return -1, text, err
	}
	return 0, text, nil
}

// hostCommandEnv is the environment of a command run for the Reeve: this
// process's, less any agent session's identity, so the command acts as the
// person's Legato and not as an agent.
func hostCommandEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "LEGATO_TASK_ID", "LEGATO_EXECUTION_ID", "LEGATO_BROKER_SOCKET", "LEGATO_BROKER_TOKEN", "LEGATO_SANDBOXED", "LEGATO_HOT_RELOAD_CHILD":
			continue
		}
		env = append(env, kv)
	}
	return env
}

// legatoCommands is the Reeve's legato tool in this process.
type legatoCommands struct{}

func (legatoCommands) Classify(args []string) (string, string) { return classifyLegatoCommand(args) }

// Commands lists the commands the Reeve may run, in the CLI's own order,
// for the tool's description.
func (legatoCommands) Commands() []cli.LegatoCommand {
	var out []cli.LegatoCommand
	var walk func(prefix []string, c *commandDef)
	walk = func(prefix []string, c *commandDef) {
		path := append(append([]string{}, prefix...), c.Name)
		if len(c.Children) == 0 {
			key := strings.Join(path[1:], " ")
			if entry, ok := legatoCommandClasses[key]; ok && entry.Class != "never" {
				out = append(out, cli.LegatoCommand{Path: key, Class: entry.Class, Description: entry.Description})
			}
			return
		}
		for _, child := range c.Children {
			walk(path, child)
		}
	}
	walk(nil, commandTree())
	return out
}

func (legatoCommands) Run(ctx context.Context, args []string) (int, string, error) {
	return runLegatoCommand(ctx, args)
}
