package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/hotreload"
	"github.com/cpave3/legato/internal/engine/ipc"
)

// ipcRestart is the IPC message that makes every Legato process of one
// instance restart. Content holds the instance's config.yaml path, which is
// known without loading it (so `legato restart` works after a bad edit), and
// a scratch instance sharing the runtime folder is left alone.
const ipcRestart = "restart"

// restartInPlace is set once this process was asked to restart itself: it
// shuts down as on SIGTERM, and main starts it again (reexec).
var restartInPlace atomic.Bool

// launchExecutable is the binary this process was started from, resolved
// at start: an update renames a new binary over it, and the restart runs
// that one.
var launchExecutable = resolveLaunchExecutable()

func resolveLaunchExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved
	}
	return exe
}

// requestRestart asks this process to restart: it shuts down as it does on
// SIGTERM (the TUI puts the terminal back), then main re-executes it. The
// signal waits a moment, so the answer to whoever asked goes out first.
func requestRestart() {
	restartInPlace.Store(true)
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
}

// restartMessage asks every Legato process of the configuration at
// configPath to restart.
func restartMessage(configPath string) ipc.Message {
	return ipc.Message{Type: ipcRestart, Content: filepath.Clean(configPath)}
}

// isRestartFor reports whether msg asks the processes of configPath to
// restart.
func isRestartFor(msg ipc.Message, configPath string) bool {
	return msg.Type == ipcRestart && msg.Content != "" && filepath.Clean(msg.Content) == filepath.Clean(configPath)
}

// restartAll restarts every Legato process of this instance, this one
// included when it listens for IPC.
func restartAll() {
	ipc.Broadcast(restartMessage(config.ResolveConfigPath()))
}

// restartHow is how this process starts again: under the hot-reload
// supervisor it exits for the supervisor to start a new child; otherwise it
// replaces itself with the same binary, arguments and environment, keeping
// its PID (so systemd sees the same service).
func restartHow(getenv func(string) string) string {
	if getenv(hotReloadChildEnv) != "" {
		return "supervisor"
	}
	return "exec"
}

// reexec starts this process again in place. It returns only on failure.
func reexec() error {
	if restartHow(os.Getenv) == "supervisor" {
		os.Exit(hotreload.RestartExitCode)
	}
	if err := syscall.Exec(launchExecutable, os.Args, os.Environ()); err != nil {
		return fmt.Errorf("restart: exec %s: %w", launchExecutable, err)
	}
	return nil
}
