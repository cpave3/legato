package main

import (
	"fmt"
	"os"

	"github.com/cpave3/legato/internal/engine/sandbox"
)

// isSandboxExec says whether this process starts a command in a Seatbelt
// sandbox: `legato sandbox exec <spec> -- <command>`, the line a sandboxed
// spawn types on macOS (sandbox.Seatbelt.Prefix). It runs before anything
// else, as git's askpass helper does: no configuration, no database.
func isSandboxExec(args []string) bool {
	return len(args) >= 2 && args[0] == "sandbox" && args[1] == "exec"
}

// sandboxExec becomes sandbox-exec with the spec, or says why it cannot;
// it never runs the command outside the sandbox.
func sandboxExec(args []string) int {
	if len(args) < 3 || args[1] != "--" {
		fmt.Fprintln(os.Stderr, "usage: legato sandbox exec <spec> -- <command> [args...]")
		return exitUsage
	}
	err := sandbox.ExecLaunchSpec(args[0], args[2:])
	fmt.Fprintf(os.Stderr, "legato: the sandbox did not start, so the agent did not: %v\n", err)
	return 1
}
