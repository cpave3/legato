package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// hostExecEnv marks the legato binary run as the wrapper of a Reeve host
// command: it sets no_new_privs (on Linux), so nothing it runs can gain
// privileges (sudo fails), then replaces itself with sh -c <command>.
const hostExecEnv = "LEGATO_HOST_EXEC"

// isHostExec reports whether this process is that wrapper.
func isHostExec(getenv func(string) string) bool { return getenv(hostExecEnv) == "1" }

// hostExec runs the wrapper: args are the command line for sh -c.
func hostExec(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "legato host exec: want one command")
		return 2
	}
	if err := setNoNewPrivs(); err != nil {
		fmt.Fprintf(os.Stderr, "legato host exec: no_new_privs: %v\n", err)
		return 2
	}
	env := []string{}
	for _, kv := range os.Environ() {
		if len(kv) > len(hostExecEnv) && kv[:len(hostExecEnv)+1] == hostExecEnv+"=" {
			continue
		}
		env = append(env, kv)
	}
	err := syscall.Exec("/bin/sh", []string{"sh", "-c", args[0]}, env)
	fmt.Fprintf(os.Stderr, "legato host exec: %v\n", err)
	return 127
}

// hostCommandPath is the PATH of a Reeve host command: the system's, and
// the user's own bin folders.
const hostCommandPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// hostOutputWait is how long a stopped host command's output is still read
// before it is given up.
const hostOutputWait = 2 * time.Second

// runHostCommand runs a Reeve host command as Legato's user, in its home
// folder, with a small fixed environment (not Legato's own) and, unless as
// root, through this binary's no_new_privs wrapper so sudo fails. As root
// it runs with sudo -n, which works only where the user may sudo without a
// password. It stops at timeout.
func runHostCommand(ctx context.Context, command service.HostCommand, timeout time.Duration) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	home, _ := os.UserHomeDir()
	env := []string{"HOME=" + home, "PATH=" + home + "/.local/bin:" + home + "/bin:" + hostCommandPath, "LANG=C.UTF-8"}
	for _, name := range []string{"USER", "LOGNAME", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"} {
		if v := os.Getenv(name); v != "" {
			env = append(env, name+"="+v)
		}
	}
	var cmd *exec.Cmd
	if command.AsRoot {
		cmd = exec.CommandContext(ctx, "sudo", append([]string{"-n", "--"}, command.Argv...)...)
	} else {
		line := shellLine(command.Argv)
		if len(command.Argv) == 3 && command.Argv[0] == "sh" && command.Argv[1] == "-c" {
			line = command.Argv[2]
		}
		cmd = exec.CommandContext(ctx, launchExecutable, line)
		env = append(env, hostExecEnv+"=1")
	}
	cmd.Env, cmd.Dir = env, home
	// The command and everything it starts form a process group, which the
	// timeout kills whole; a child that still holds the output open is not
	// waited for more than hostOutputWait.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = hostOutputWait
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return -1, out.Bytes(), fmt.Errorf("stopped after %s", timeout)
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), out.Bytes(), nil
	}
	return 0, out.Bytes(), err
}

// shellLine quotes argv for sh -c.
func shellLine(argv []string) string {
	words := make([]string, len(argv))
	for i, w := range argv {
		words[i] = "'" + strings.ReplaceAll(w, "'", `'\''`) + "'"
	}
	return strings.Join(words, " ")
}
