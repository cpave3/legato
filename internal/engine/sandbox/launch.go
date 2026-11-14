//go:build unix

package sandbox

import (
	"fmt"
	"os"
	"syscall"
)

// ExecLaunchSpec is `legato sandbox exec <file> -- <command>`: it reads the
// spec a Seatbelt Prefix wrote, moves to its working directory, and becomes
// sandbox-exec with the spec's profile, parameters and environment, so the
// command and all it starts run in the sandbox. It returns only when that
// fails; nothing runs outside the sandbox.
func ExecLaunchSpec(file string, command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("no command to run in the sandbox")
	}
	spec, err := ReadLaunchSpec(file)
	if err != nil {
		return err
	}
	if spec.Chdir != "" {
		if err := os.Chdir(spec.Chdir); err != nil {
			return err
		}
	}
	argv := spec.Argv(terminalPath(os.Stdin), command)
	return syscall.Exec(argv[0], argv, spec.Environ(os.Environ()))
}
