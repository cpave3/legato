package service

import (
	"context"
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
	if prompt := env["LEGATO_ROLE_PROMPT_FILE"]; prompt != "" {
		in.AgentFilesDir = filepath.Dir(prompt)
	}
	in.PastedImagesDir = PastedImageDir(taskID)
	in.GitCommonDir = gitCommonDir(ctx, workDir)
	in.Extra = append([]SandboxPath{}, in.Extra...)
	for _, resource := range resources {
		for _, p := range resource.Paths {
			in.Extra = append(in.Extra, SandboxPath{Path: p.Path, Access: p.Access})
		}
	}
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		return "", err
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
	args, err := backend.Wrap(plan, []string{"true"})
	if err != nil {
		return "", err
	}
	args = args[:len(args)-1]
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellWord(arg)
	}
	return strings.Join(quoted, " "), nil
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

