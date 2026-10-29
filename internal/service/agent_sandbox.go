package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cpave3/legato/internal/engine/sandbox"
)

// SandboxSettings is Legato's sandbox for the agents a service spawns.
type SandboxSettings struct {
	Backend sandbox.Backend
	// Base is the plan's input that does not change from agent to agent:
	// home, runtime and data folders, toolchains, configured mounts.
	Base SandboxPlanInput
}

// sandboxPrefix is the command line that starts an agent's launch command
// in its sandbox, ending in "--".
func (a *agentService) sandboxPrefix(ctx context.Context, taskID, executionID, agentKind, workDir string, env map[string]string, resources []Resource) (string, error) {
	backend := a.sandbox.Backend
	if err := backend.Available(); err != nil {
		return "", err
	}
	in := a.sandbox.Base
	in.WorkDir, in.ExecutionID, in.TaskID, in.AgentKind = workDir, executionID, taskID, agentKind
	in.BrokerSocket = env["LEGATO_BROKER_SOCKET"]
	if sock := env["SSH_AUTH_SOCK"]; sock != "" && in.RuntimeDir != "" && strings.HasPrefix(sock, filepath.Join(in.RuntimeDir, "ssh")+"/") {
		in.SSHSocket = sock
	}
	// The folder of the role prompt, brief and first message.
	for _, name := range []string{"LEGATO_ROLE_PROMPT_FILE", "LEGATO_BRIEF_FILE", "LEGATO_PROMPT_FILE"} {
		if file := env[name]; file != "" {
			in.AgentFilesDir = filepath.Dir(file)
			break
		}
	}
	in.PastedImagesDir = PastedImageDir(taskID)
	in.ToolState = toolStateDirs(agentKind, env, in.Home)
	for _, dir := range in.ToolState {
		// Made when missing, so what the tool writes there lands on the host
		// and not in the sandbox's empty home.
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
	}
	in.GitCommonDir = gitCommonDir(ctx, workDir)
	in.Extra = append([]SandboxPath{}, in.Extra...)
	for _, resource := range resources {
		for _, p := range resource.Paths {
			in.Extra = append(in.Extra, SandboxPath{Path: p.Path, Access: p.Access})
		}
	}
	if in.RuntimeDir != "" {
		in.SSHConfigDir = filepath.Join(in.RuntimeDir, "sandbox", executionID, "ssh")
		copies, err := writeSandboxSSHConfig("/etc/ssh", in.SSHConfigDir)
		if err != nil {
			return "", err
		}
		in.SSHConfig = copies
	}
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		return "", err
	}
	for k, v := range sandboxGitFetches(env) {
		plan.Setenv[k] = v
	}
	if in.SSHSocket == "" {
		// The person's own agent (1Password's) is not this session's.
		plan.Unsetenv = append(plan.Unsetenv, "SSH_AUTH_SOCK")
	}
	if env["GH_TOKEN"] != GHSignedOut {
		plan.Unsetenv = append(plan.Unsetenv, "GH_TOKEN")
	}
	if in.RuntimeDir != "" {
		if err := os.MkdirAll(filepath.Join(in.RuntimeDir, "sandbox", executionID, "tmp"), 0o700); err != nil {
			return "", err
		}
	}
	// The broker's folder is mounted whether or not a host serves it yet:
	// one that starts later is then reachable from inside.
	if in.BrokerSocket != "" {
		if err := os.MkdirAll(filepath.Dir(in.BrokerSocket), 0o700); err != nil {
			return "", err
		}
	}
	args, err := backend.Wrap(plan, []string{"true"})
	if err != nil {
		return "", err
	}
	args = args[:len(args)-1] // the backend's command, then its options, then "--"
	if in.RuntimeDir != "" && len(args) >= 2 && args[len(args)-1] == "--" {
		file := filepath.Join(in.RuntimeDir, "sandbox", executionID, "bwrap.args")
		return argsFileCommand(args[0], args[1:len(args)-1], file)
	}
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellWord(arg)
	}
	return strings.Join(quoted, " "), nil
}

// argsFileCommand writes a backend's options to file, NUL-separated, and
// returns the short command line that reads them from it: a launch command
// is typed into a terminal, which cuts a line at 4096 bytes before the shell
// reads it.
func argsFileCommand(backend string, options []string, file string) (string, error) {
	var data []byte
	for _, option := range options {
		if strings.ContainsRune(option, 0) {
			return "", fmt.Errorf("sandbox option %q holds a NUL byte", option)
		}
		data = append(append(data, option...), 0)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(file, data, 0o600); err != nil {
		return "", err
	}
	return shellWord(backend) + " --args 3 3<" + shellWord(file) + " --", nil
}

// gitCommonDir is the .git a worktree's commits are written to, when it is
// outside the working directory; "" otherwise.
func gitCommonDir(ctx context.Context, dir string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellWord quotes a word for the shell only when it needs it.
func shellWord(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return shellQuote(s)
}

// toolStateVars name the variables that move an agent tool's state, and so
// its sessions, out of the folders the sandbox mounts for it.
var toolStateVars = map[string]string{"claude-code": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME"}

// toolStateDirs is where the agent's tool keeps its state when a variable
// moves it: the session's own value (a resumed session's storage folder),
// else the one this process inherited, which the pane inherits too.
func toolStateDirs(agentKind string, env map[string]string, home string) []string {
	name, ok := toolStateVars[agentKind]
	if !ok {
		return nil
	}
	dir := env[name]
	if dir == "" {
		dir = os.Getenv(name)
	}
	if strings.HasPrefix(dir, "~/") && home != "" {
		dir = filepath.Join(home, dir[2:])
	}
	if !filepath.IsAbs(dir) {
		return nil
	}
	return []string{filepath.Clean(dir)}
}
