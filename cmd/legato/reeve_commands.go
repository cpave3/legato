package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
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
		return "never", "Not a legato command: legato " + strings.Join(args, " ")
	}
	entry, ok := legatoCommandClasses[strings.Join(path, " ")]
	if !ok {
		return "never", "A legato command the Reeve has no class for: " + strings.Join(path, " ")
	}
	return entry.Class, entry.Description + " (legato " + strings.Join(args, " ") + ")"
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

func (legatoCommands) Run(ctx context.Context, args []string) (int, string, error) {
	return runLegatoCommand(ctx, args)
}
