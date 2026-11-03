package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cpave3/legato/internal/engine/clonesetup"
)

// cloneSetupLog is the log of a clone's setup, in its .git, so it is never
// part of the work.
const cloneSetupLog = "legato-setup.log"

// noteSetupFailure records a failed setup on the task. It runs
// `legato task note`, which the sandbox forwards to the host Legato.
var noteSetupFailure = func(taskID, text string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return exec.Command(exe, "task", "note", taskID, text).Run()
}

// runCloneSetupCmd runs the setup commands of the clone in the working
// directory (its .legato.toml, else its .yggdrasil.toml's post_create
// hooks). Legato runs it in a fresh clone's pane, in the agent's sandbox,
// before the agent tool starts. What the commands write goes to the
// terminal and to .git/legato-setup.log; a failure is noted on the task.
func runCloneSetupCmd(args []string) int {
	const command = "clone setup"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("clone", "setup"))
	if parseErr != nil {
		return renderCommandError(command, parseErr, false)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "clone setup accepts no positional arguments"), false)
	}
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "legato: %v\n", err)
		return exitEnvironment
	}
	setup, err := clonesetup.Load(dir)
	if errors.Is(err, clonesetup.ErrNoSetup) || (err == nil && len(setup.Run) == 0) {
		return exitOK
	}
	out := io.Writer(os.Stdout)
	logPath := filepath.Join(dir, ".git", cloneSetupLog)
	if logFile, logErr := os.Create(logPath); logErr == nil {
		defer logFile.Close()
		out = io.MultiWriter(os.Stdout, logFile)
	} else {
		logPath = ""
	}
	if err == nil {
		fmt.Fprintf(out, "legato: setting up this clone from %s\n", setup.File)
		err = clonesetup.Run(context.Background(), setup.Run, clonesetup.RunOptions{
			Dir: dir, Base: parsed.Values["base"], Primary: parsed.Values["primary"], Out: out,
		})
	}
	if err == nil {
		fmt.Fprintln(out, "legato: this clone is set up")
		return exitOK
	}
	fmt.Fprintf(out, "legato: setting up this clone failed: %v\n", err)
	text := fmt.Sprintf("Setting up the clone at %s failed: %v.", dir, err)
	if logPath != "" {
		text += " Its output is in " + logPath + "."
	}
	if taskID := os.Getenv("LEGATO_TASK_ID"); taskID != "" {
		if noteErr := noteSetupFailure(taskID, text); noteErr != nil {
			fmt.Fprintf(os.Stderr, "legato: noting the failure on %s: %v\n", taskID, noteErr)
		}
	}
	return exitInternal // the setup failed; the agent still starts
}
