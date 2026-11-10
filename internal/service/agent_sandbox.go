package service

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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

// CheckoutOverlayProber says whether an agent's checkout can be shown
// beneath a throwaway layer (AgentSpawnOptions.ThrowawayCheckout); without
// it, a throwaway checkout is mounted read-only, and builds that write to
// the tree fail.
type CheckoutOverlayProber interface {
	CheckoutOverlayAvailable() bool
}

func (a *agentService) CheckoutOverlayAvailable() bool {
	if a.sandbox == nil {
		return false
	}
	overlays, ok := a.sandbox.Backend.(sandbox.OverlaySupporter)
	return ok && overlays.SupportsOverlay()
}

// sandboxPrefix is the command line that starts an agent's launch command
// in its sandbox, ending in "--".
func (a *agentService) sandboxPrefix(ctx context.Context, taskID, executionID, agentKind, workDir string, env map[string]string, resources []Resource, readOnly, throwaway bool) (string, error) {
	backend := a.sandbox.Backend
	if err := backend.Available(); err != nil {
		return "", err
	}
	in := a.sandbox.Base
	in.WorkDir, in.ExecutionID, in.TaskID, in.AgentKind = workDir, executionID, taskID, agentKind
	in.ReadOnlyCheckout = readOnly
	// Without overlays, a throwaway checkout is the plain read-only one.
	in.ThrowawayCheckout = readOnly && throwaway && a.CheckoutOverlayAvailable()
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
	in.JobDir = a.taskJobClone(ctx, taskID, in.DataDir, workDir)
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
	if gh, ok := sandboxGH(in.CredentialWrapperDir, env["PATH"], filepath.Join(in.RuntimeDir, "sandbox", executionID, "gh-real")); ok {
		if err := os.WriteFile(gh.RealMount, nil, 0o600); err != nil {
			return "", err
		}
		in.GH = gh
	}
	az, err := sandboxAz(in, executionID)
	if err != nil {
		return "", err
	}
	in.Az = az
	// The scratch folder exists before the plan is made, so a plan whose
	// mounts hold it covers it on the first spawn too.
	if root := scratchFor(in).Root; root != "" {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return "", err
		}
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
	// The scratch folders on disk: private, as an agent may write a token
	// there, and kept until the execution ends.
	scratch := scratchFor(in)
	var private []string
	if scratch.Root != "" {
		private = []string{scratch.Root, scratch.Dir(executionID), scratch.Tmp(executionID), scratch.Home(executionID)}
	}
	for _, m := range plan.Mounts {
		if m.Kind == sandbox.Overlay && m.Upper != "" {
			private = append(private, filepath.Dir(m.Upper), m.Upper, m.Work)
		}
	}
	for _, dir := range private {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		if dir != scratch.Root {
			if err := os.Chmod(dir, 0o700); err != nil {
				return "", err
			}
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

// sandboxGH is how a sandbox puts Legato's gh wrapper in place of every real
// gh, for a session whose PATH starts with the wrapper's folder (a session
// with a GitHub credential). The agent tools run commands in login shells,
// which reorder PATH, so the wrapper cannot rely on coming first.
func sandboxGH(wrapperDir, sessionPath, realMount string) (SandboxWrapper, bool) {
	if wrapperDir == "" {
		return SandboxWrapper{}, false
	}
	first, _, _ := strings.Cut(sessionPath, string(os.PathListSeparator))
	wrapper := filepath.Join(wrapperDir, "gh")
	if filepath.Clean(first) != filepath.Clean(wrapperDir) {
		return SandboxWrapper{}, false
	}
	if _, err := os.Stat(wrapper); err != nil {
		return SandboxWrapper{}, false
	}
	gh := SandboxWrapper{Wrapper: wrapper, RealMount: realMount, Overlays: realCommands("gh", wrapperDir)}
	if len(gh.Overlays) == 0 {
		return SandboxWrapper{}, false
	}
	gh.Real = gh.Overlays[0]
	return gh, true
}

// sandboxAz is how a sandbox puts Legato's az wrapper in place of every real
// az, when an azure credential is configured: the wrapper is written into
// the execution's runtime folder (removed when it ends), and the first real
// az is mounted aside for it at az-real. There is no wrapper outside the
// sandbox, where ~/.azure is the person's own.
func sandboxAz(in SandboxPlanInput, executionID string) (SandboxWrapper, error) {
	if in.Azure.Credential == "" || in.RuntimeDir == "" {
		return SandboxWrapper{}, nil
	}
	reals := realCommands("az", "")
	if len(reals) == 0 {
		return SandboxWrapper{}, nil
	}
	dir := filepath.Join(in.RuntimeDir, "sandbox", executionID)
	az := SandboxWrapper{Wrapper: filepath.Join(dir, "az"), Real: reals[0], RealMount: filepath.Join(dir, "az-real"), Overlays: reals}
	legato := in.BinaryPath
	if legato == "" {
		legato = "legato"
	}
	if err := writeAzWrapper(az.Wrapper, legato, in.Azure); err != nil {
		return SandboxWrapper{}, err
	}
	if err := os.WriteFile(az.RealMount, nil, 0o600); err != nil {
		return SandboxWrapper{}, err
	}
	return az, nil
}

// realCommands are the real paths (links resolved) of every executable named
// name on this process's PATH, in PATH's order, skipping the folder skip.
func realCommands(name, skip string) []string {
	var out []string
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || (skip != "" && filepath.Clean(dir) == filepath.Clean(skip)) {
			continue
		}
		real, err := filepath.EvalSymlinks(filepath.Join(dir, name))
		if err != nil || seen[real] {
			continue
		}
		if info, err := os.Stat(real); err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		seen[real] = true
		out = append(out, real)
	}
	return out
}

// taskJobClone is the job clone that holds workDir when the task works in
// it (Legato made it for the task, or the task joined it), "" otherwise.
// Only such a clone of Legato's data folder can be a sandboxed agent's
// work: also when the agent is started again in it.
func (a *agentService) taskJobClone(ctx context.Context, taskID, dataDir, workDir string) string {
	if dataDir == "" {
		return ""
	}
	jobs := filepath.Join(dataDir, "jobs")
	rel, err := filepath.Rel(jobs, workDir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 {
		return ""
	}
	path := filepath.Join(jobs, parts[0], parts[1])
	clone, err := a.store.JobCloneByPath(ctx, path)
	if err != nil || !slices.Contains(clone.TaskIDs, taskID) {
		return ""
	}
	return path
}
