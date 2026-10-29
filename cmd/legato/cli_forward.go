package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/service"
)

// In Legato's sandbox the store and configuration are out of reach, so the
// `legato` commands agents run as part of their work are forwarded to the
// host through the broker. The host runs its own binary as the calling
// session, whose identity comes from the token and never from the agent.

// forwardedCommands are the agent-facing commands: the first word, then the
// second words allowed ("*" for any). Host-only commands (device, secret,
// audit, hooks, auth, serve, …) are not here.
var forwardedCommands = map[string][]string{
	"task":       {"show", "notes", "note", "update", "description", "list", "create", "link"},
	"plan":       {"*"},
	"review":     {"*"},
	"swarm":      {"*"},
	"learn":      {"*"},
	"credential": {"request", "get", "list"},
	"agent":      {"peers", "message", "broadcast"},
	"workflow":   {"step", "show"},
	"attention":  {"list"},
	"resource":   {"list"},
	"workspace":  {"list"},
}

// forwardedEnv is what the host takes of the agent's environment: values
// that name the work a command acts on, never its identity.
var forwardedEnv = []string{"LEGATO_PARENT_TASK_ID", "LEGATO_SUBTASK_ID", "LEGATO_AGENT_ROLE", "LEGATO_WORKFLOW_RUN_ID",
	"LEGATO_WORKFLOW_STEP_ID", "LEGATO_WORKFLOW_ATTEMPT_ID", "LEGATO_LEARNING_REQUEST_ID", "LEGATO_PLAN_NAME", "LEGATO_REVIEW_NAME", "LEGATO_REVIEW_TASK_ID"}

func forwardable(args []string) bool {
	if len(args) < 2 {
		return false
	}
	for _, a := range args {
		if a == "--help" || a == "-h" || a == "help" {
			return false
		}
	}
	allowed, ok := forwardedCommands[args[0]]
	if !ok {
		return false
	}
	for _, sub := range allowed {
		if sub == "*" || sub == args[1] {
			return true
		}
	}
	return false
}

type cliRunResult struct {
	Stdout []byte `json:"stdout"`
	Stderr []byte `json:"stderr"`
	Exit   int    `json:"exit"`
}

// cliRunTimeout bounds a forwarded command: propose-plan waits for the
// person's verdict.
const cliRunTimeout = time.Hour

// cliRunOp is the host's side: run binary with the forwarded arguments as
// the caller. A path under the sandbox's /tmp is the session's folder on the
// host.
func cliRunOp(binary, runtimeDir string) cli.BrokerOp {
	return func(ctx context.Context, caller cli.BrokerCaller, raw json.RawMessage) (any, error) {
		var in struct {
			Args  []string          `json:"args"`
			Cwd   string            `json:"cwd"`
			Stdin []byte            `json:"stdin"`
			Env   map[string]string `json:"env"`
		}
		if err := json.Unmarshal(raw, &in); err != nil || !forwardable(in.Args) {
			return nil, &broker.Error{Code: "forbidden", Message: "this command is not run for agents in the sandbox"}
		}
		tmp := filepath.Join(runtimeDir, "sandbox", caller.ExecutionID, "tmp")
		args := make([]string, len(in.Args))
		for i, a := range in.Args {
			args[i] = hostPath(a, tmp)
		}
		ctx, cancel := context.WithTimeout(ctx, cliRunTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		if cwd := hostPath(in.Cwd, tmp); cwd != "" {
			if info, err := os.Stat(cwd); err == nil && info.IsDir() {
				cmd.Dir = cwd
			}
		}
		env := make([]string, 0, len(os.Environ())+len(forwardedEnv)+2)
		for _, kv := range os.Environ() {
			name, _, _ := strings.Cut(kv, "=")
			switch name {
			case "LEGATO_TASK_ID", "LEGATO_EXECUTION_ID", "LEGATO_BROKER_SOCKET", "LEGATO_BROKER_TOKEN", "LEGATO_SANDBOXED":
				continue
			}
			env = append(env, kv)
		}
		for _, name := range forwardedEnv {
			if v := in.Env[name]; v != "" {
				env = append(env, name+"="+v)
			}
		}
		env = append(env, "LEGATO_TASK_ID="+caller.TaskID, "LEGATO_EXECUTION_ID="+caller.ExecutionID)
		cmd.Env = env
		cmd.Stdin = bytes.NewReader(in.Stdin)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		result := cliRunResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
		var exitErr *exec.ExitError
		switch {
		case errors.As(err, &exitErr):
			result.Exit = exitErr.ExitCode()
		case err != nil:
			return nil, err
		}
		return result, nil
	}
}

// hostPath maps a path under the sandbox's /tmp to the session's folder on
// the host. Pasted images are at the same path inside and out.
func hostPath(arg, tmp string) string {
	prefix, value, hasFlag := "", arg, false
	if strings.HasPrefix(arg, "--") {
		if name, v, ok := strings.Cut(arg, "="); ok {
			prefix, value, hasFlag = name+"=", v, true
		}
	}
	if !hasFlag && strings.HasPrefix(arg, "-") {
		return arg
	}
	pasted := filepath.Dir(filepath.Dir(service.PastedImageDir("x")))
	if (value == "/tmp" || strings.HasPrefix(value, "/tmp/")) && !strings.HasPrefix(value, pasted+"/") {
		return prefix + tmp + strings.TrimPrefix(value, "/tmp")
	}
	return arg
}

// forwardToHost is the agent's side: run args on the host and pass its
// output and exit code on. Stdin is sent when the command reads it.
func forwardToHost(args []string) int {
	var stdin []byte
	if readsStdin(args) {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 16<<20))
		if err != nil {
			fmt.Fprintf(os.Stderr, "legato: reading stdin: %v\n", err)
			return exitInternal
		}
		stdin = data
	}
	out, err := runOnHost(args, stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "legato: %v\n", err)
		return exitEnvironment
	}
	_, _ = os.Stdout.Write(out.Stdout)
	_, _ = os.Stderr.Write(out.Stderr)
	return out.Exit
}

// runOnHost runs a forwardable command on the host for this session.
func runOnHost(args []string, stdin []byte) (cliRunResult, error) {
	socket, token, ok := brokerSession()
	if !ok {
		return cliRunResult{}, errors.New("this command needs Legato's broker, which this sandboxed session has not been given")
	}
	cwd, _ := os.Getwd()
	env := map[string]string{}
	for _, name := range forwardedEnv {
		if v := os.Getenv(name); v != "" {
			env[name] = v
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), cliRunTimeout)
	defer cancel()
	var out cliRunResult
	err := broker.Call(ctx, socket, token, "cli.run", map[string]any{"args": args, "cwd": cwd, "stdin": stdin, "env": env}, &out)
	return out, err
}

// sandboxedCredentialExec runs a command in the sandbox with a credential's
// value in one variable, the value fetched from the host by `credential get`
// for this session.
func sandboxedCredentialExec(command string, args []string, variable string, commandLine []string, jsonMode bool) int {
	if variable == "" || len(commandLine) == 0 {
		return renderCommandError(command, usageError("missing_argument", "credential exec needs --env VARIABLE and a command after --"), jsonMode)
	}
	getArgs := []string{"credential", "get"}
	for i := 0; i < len(args); i++ {
		if args[i] == "--env" {
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--env=") {
			continue
		}
		getArgs = append(getArgs, args[i])
	}
	out, err := runOnHost(getArgs, nil)
	if err != nil {
		return renderCommandError(command, &commandError{Code: "broker_unreachable", Message: err.Error(), Exit: exitEnvironment}, jsonMode)
	}
	if out.Exit != 0 {
		_, _ = os.Stderr.Write(out.Stderr)
		_, _ = os.Stdout.Write(out.Stdout)
		return out.Exit
	}
	run := exec.Command(commandLine[0], commandLine[1:]...)
	run.Env = append(os.Environ(), variable+"="+string(out.Stdout))
	run.Stdin, run.Stdout, run.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := run.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		return renderCommandError(command, &commandError{Code: "exec_failed", Message: err.Error(), Exit: exitDependency}, jsonMode)
	}
	return exitOK
}

// readsStdin reports whether a command takes its input from stdin: a "-"
// value, or a reflection submitted without --file.
func readsStdin(args []string) bool {
	hasFile := false
	for _, a := range args {
		if a == "-" || strings.HasSuffix(a, "=-") {
			return true
		}
		if a == "--file" || strings.HasPrefix(a, "--file=") {
			hasFile = true
		}
	}
	return len(args) >= 3 && args[0] == "learn" && args[1] == "reflection" && !hasFile
}
