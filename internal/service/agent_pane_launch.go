package service

import (
	"os"
	"path/filepath"
	"strings"
)

// paneLaunch is the command that runs an agent's launch line as its tmux
// pane's own process (TmuxManager.RunInPane), and then the operator's shell,
// so attaching after the agent exits finds a shell as before (LEG-689).
//
// Typing the line into the session's shell raced that shell's startup: its
// rc files and first prompt (a git-aware theme) run other programs in the
// foreground, and a launch that met one was given up. bash and zsh run the
// line with -i, so it still sees the operator's rc files (nvm, mise, PATH)
// but draws no prompt; any other shell gets /bin/sh, since the line is POSIX.
func paneLaunch(shell, line string) []string {
	if shell == "" {
		shell = "/bin/sh"
	}
	after := "; exec " + shellWord(shell) + " -l"
	switch filepath.Base(shell) {
	case "bash", "zsh":
		return []string{shell, "-i", "-c", line + after}
	}
	return []string{"/bin/sh", "-c", line + after}
}

// operatorShell is the shell an agent's pane falls back to.
func operatorShell(env map[string]string) string {
	if s := env["SHELL"]; s != "" {
		return s
	}
	return os.Getenv("SHELL")
}

// PaneLaunchLine is the launch line inside a paneLaunch command, without the
// shell it execs afterwards: for terminals that stand in for tmux in tests
// and run or record only the agent's launch.
func PaneLaunchLine(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	line := argv[len(argv)-1]
	if i := strings.LastIndex(line, "; exec "); i >= 0 {
		line = line[:i]
	}
	return line
}
