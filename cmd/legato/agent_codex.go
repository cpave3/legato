package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/codexbridge"
	"github.com/cpave3/legato/internal/engine/ipc"
)

// codexRemoteTokenEnv names the variable the Codex terminal UI reads its app
// server token from. The token is never put on a command line.
const codexRemoteTokenEnv = "LEGATO_CODEX_REMOTE_TOKEN"

// codexBridgeReadyTimeout bounds how long a launch waits for its bridge
// before starting Codex without one.
const codexBridgeReadyTimeout = 30 * time.Second

// splitCodexLaunch separates the launcher's own arguments from the ones meant
// for Codex, which follow a bare "--".
func splitCodexLaunch(args []string) (own, codex []string) {
	for index, arg := range args {
		if arg == "--" {
			return args[:index], args[index+1:]
		}
	}
	return args, nil
}

// codexServerArgs picks out of a Codex command line the arguments that
// configure the session rather than the terminal: -c/--config overrides and
// feature switches. The terminal UI's own copies are not applied to a thread
// that lives on a remote app server, so the server is given them too.
func codexServerArgs(args []string) []string {
	var out []string
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-c" || arg == "--config" || arg == "--enable" || arg == "--disable":
			if index+1 < len(args) {
				out = append(out, arg, args[index+1])
				index++
			}
		case strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "--enable=") || strings.HasPrefix(arg, "--disable="):
			out = append(out, arg)
		}
	}
	return out
}

// codexSessionArgs is a Codex command line for an agent session. Codex opens
// with a menu when a newer version is out, and nothing gets past that menu
// until a person answers it: a message sent to the agent waits behind it. An
// agent Legato starts may have nobody at its terminal, so the check is off
// unless the launch arguments, which come later and win, ask for it.
func codexSessionArgs(args []string) []string {
	return append([]string{"-c", "check_for_update_on_startup=false"}, args...)
}

// codexAttachArgs is the Codex command line that attaches the terminal UI to
// the session's app server instead of starting a private one.
func codexAttachArgs(serverURL string, args []string) []string {
	return append([]string{"--remote", serverURL, "--remote-auth-token-env", codexRemoteTokenEnv}, args...)
}

// codexCanBeBridged reads `codex --help` for what the bridge depends on: an
// app server, and a terminal UI that can attach to one behind a token. An
// older Codex is started on its own, as it always was.
func codexCanBeBridged(help string) bool {
	return strings.Contains(help, "app-server") && strings.Contains(help, "--remote <") && strings.Contains(help, "--remote-auth-token-env")
}

// runAgentCodexContract starts Codex for a Legato agent session. It runs in
// the agent's tmux pane in place of a bare `codex`:
//
//	legato agent codex -- <codex arguments>
//
// It starts the session's bridge, which brings up a Codex app server, then
// replaces itself with the Codex terminal UI attached to that server. The
// terminal and Legato's native panel are then two views of one conversation.
// Whatever goes wrong on the way, Codex still starts: without the bridge the
// session is driven through its terminal, as before.
func runAgentCodexContract(args []string) int {
	const command = "agent.codex"
	own, codexArgs := splitCodexLaunch(args)
	parsed, parseErr := parseCommandArgs(own, commandFlagSpecs("agent", "codex"))
	if parseErr != nil {
		return renderCommandError(command, parseErr, false)
	}
	codexArgs = codexSessionArgs(codexArgs)
	codexBin, err := exec.LookPath("codex")
	if err != nil {
		return renderCommandError(command, &commandError{Code: "codex_missing", Message: "codex is not on PATH", Exit: exitEnvironment}, false)
	}
	plain := func(reason string) int {
		if reason != "" {
			fmt.Fprintf(os.Stderr, "legato: starting Codex without the native bridge: %s\n", reason)
		}
		return execReplacing(command, codexBin, append([]string{"codex"}, codexArgs...), os.Environ())
	}

	taskID, _, idErr := resolveTaskID(parsed.Positionals)
	if idErr != nil {
		// Not a Legato session: there is no agent to bridge.
		return plain("")
	}
	if help, err := exec.Command(codexBin, "--help").Output(); err != nil || !codexCanBeBridged(string(help)) {
		return plain("this Codex has no app server its terminal can attach to")
	}
	self, err := os.Executable()
	if err != nil {
		return plain(err.Error())
	}

	bridgeArgs := []string{"agent", "codex-bridge", taskID}
	if executionID := strings.TrimSpace(parsed.Values["execution-id"]); executionID != "" {
		bridgeArgs = append(bridgeArgs, "--execution-id", executionID)
	}
	bridgeArgs = append(append(bridgeArgs, "--"), codexServerArgs(codexArgs)...)
	bridge := exec.Command(self, bridgeArgs...)
	// The bridge outlives this process image (Codex replaces it) and must not
	// take the terminal's signals or write to its screen.
	bridge.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	logPath := codexbridge.StatePath(ipc.SocketDir(), taskID, ".log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err == nil {
		if logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err == nil {
			bridge.Stderr = logFile
			defer logFile.Close()
		}
	}
	announcements, err := bridge.StdoutPipe()
	if err != nil {
		return plain(err.Error())
	}
	if err := bridge.Start(); err != nil {
		return plain(err.Error())
	}

	type announcement struct {
		Type  string `json:"type"`
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	ready := make(chan announcement, 1)
	go func() {
		defer close(ready)
		scanner := bufio.NewScanner(announcements)
		for scanner.Scan() {
			var said announcement
			if json.Unmarshal(scanner.Bytes(), &said) == nil && said.Type == "ready" && said.URL != "" {
				ready <- said
				return
			}
		}
	}()
	select {
	case said, ok := <-ready:
		if !ok {
			_ = bridge.Process.Kill()
			return plain("its bridge exited (see " + logPath + ")")
		}
		env := append(os.Environ(), codexRemoteTokenEnv+"="+said.Token)
		// From here the bridge's parent is Codex itself: it ends when Codex does.
		return execReplacing(command, codexBin, append([]string{"codex"}, codexAttachArgs(said.URL, codexArgs)...), env)
	case <-time.After(codexBridgeReadyTimeout):
		_ = bridge.Process.Kill()
		return plain("its bridge was not ready in time (see " + logPath + ")")
	}
}

// execReplacing replaces this process with another program. It returns only
// when that fails.
func execReplacing(command, path string, argv, env []string) int {
	err := syscall.Exec(path, argv, env)
	return renderCommandError(command, &commandError{Code: "exec_failed", Message: fmt.Sprintf("starting %s: %v", path, err), Exit: exitEnvironment}, false)
}

// runAgentCodexBridgeContract hosts the bridge of one Codex session. The
// launcher starts it and reads one line from its stdout, the announcement of
// where the terminal UI should connect; everything else goes to stderr.
func runAgentCodexBridgeContract(args []string) int {
	const command = "agent.codex-bridge"
	own, serverArgs := splitCodexLaunch(args)
	parsed, parseErr := parseCommandArgs(own, commandFlagSpecs("agent", "codex-bridge"))
	if parseErr != nil {
		return renderCommandError(command, parseErr, false)
	}
	taskID, rest, idErr := resolveTaskID(parsed.Positionals)
	if idErr != nil {
		return renderCommandError(command, idErr, false)
	}
	if len(rest) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "agent codex-bridge accepts one task ID"), false)
	}
	executionID := strings.TrimSpace(parsed.Values["execution-id"])
	if executionID == "" {
		executionID = strings.TrimSpace(os.Getenv("LEGATO_EXECUTION_ID"))
	}
	host, db, _, failed := bridgeHost(command, taskID, executionID, func(*config.Config) []string { return nil })
	if failed != 0 {
		return failed
	}
	if db != nil {
		defer db.Close()
	}

	// The launcher stops reading once Codex has replaced it; a later write
	// to that pipe must be an error, not the end of the bridge.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	signal.Ignore(syscall.SIGPIPE)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// The launcher becomes Codex without changing its process ID, so the
	// bridge's parent is the session: when it is gone, so is the bridge.
	go cancelWhenOrphaned(ctx, cancel, os.Getppid(), os.Getppid, time.Second)

	err := cli.RunCodexBridge(ctx, db, cli.CodexBridgeOptions{
		TaskID: taskID, ExecutionID: executionID, ServerArgs: serverArgs, Host: host,
		Ready: func(server cli.CodexAppServer) {
			line, _ := json.Marshal(map[string]string{"type": "ready", "url": server.URL, "token": server.Token})
			fmt.Fprintln(os.Stdout, string(line))
		},
	})
	if err != nil {
		return renderCommandError(command, commandDomainError(err), false)
	}
	return exitOK
}
