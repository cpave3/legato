package service

import (
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
	// Docker gives each sandboxed agent a filtered Docker socket
	// (agents.sandbox.docker). Nil: none, and Docker stays hidden.
	Docker *SandboxDocker
	// GitConfig notes the git config keys of the checkout that run a
	// command at spawn, and reports those an agent added when it ends. Nil:
	// not watched.
	GitConfig *SandboxGitConfig
	// ToolLogin is the environment an agent tool logs in with inside a
	// sandbox where it cannot reach its own login (Claude Code's is in the
	// macOS Keychain), or why it cannot start there. Nil: none needed.
	ToolLogin func(ctx context.Context, agentKind string) (map[string]string, error)
	// EndProcesses ends what an execution left running, for a backend with
	// no process namespace (EndSandboxProcesses). Nil: the backend's own
	// namespace ends everything with the pane.
	EndProcesses func(ctx context.Context, executionID string)
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
	return a.sandbox.Backend.Capabilities().Overlay
}

// sandboxPrefix is the command line that starts an agent's launch command
// in its sandbox, ending in "--".
func (a *agentService) sandboxPrefix(ctx context.Context, taskID, executionID, agentKind, workDir string, env map[string]string, resources []Resource, readOnly, throwaway bool, engram SandboxEngram) (string, error) {
	backend := a.sandbox.Backend
	if err := backend.Available(); err != nil {
		return "", err
	}
	in := a.sandbox.Base
	in.WorkDir, in.ExecutionID, in.TaskID, in.AgentKind = workDir, executionID, taskID, agentKind
	caps := backend.Capabilities()
	in.Capabilities = &caps
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
	if dir, err := EnsurePastedImageDir(taskID); err == nil {
		in.PastedImagesDir = dir
	} else {
		// The agent still starts; only images pasted for it will not show.
		log.Printf("sandbox: the pasted images folder of %s: %v", taskID, err)
	}
	in.ToolState = toolStateDirs(agentKind, env, in.Home)
	for _, dir := range in.ToolState {
		// Made when missing, so what the tool writes there lands on the host
		// and not in the sandbox's empty home.
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
	}
	in.GitCommonDir = gitCommonDir(ctx, workDir)
	if a.sandbox.GitConfig != nil && in.GitCommonDir != "" && in.RuntimeDir != "" && !readOnly {
		// What the person's git would run before the agent starts.
		if err := a.sandbox.GitConfig.Baseline(ctx, filepath.Join(in.RuntimeDir, "sandbox", executionID), taskID, filepath.Join(in.GitCommonDir, "config")); err != nil {
			log.Printf("sandbox: noting %s's git config: %v", workDir, err)
		}
	}
	in.JobDir = a.taskJobClone(ctx, taskID, in.DataDir, workDir)
	in.Extra = append([]SandboxPath{}, in.Extra...)
	for _, resource := range resources {
		for _, p := range resource.Paths {
			in.Extra = append(in.Extra, SandboxPath(p))
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
	// An engram session's binary, at a path of the execution's own whose
	// folder goes first on its PATH.
	pathFirst := ""
	if engram.Store != "" && in.RuntimeDir == "" {
		return "", errors.New("an engram session needs Legato's runtime folder for its engram binary")
	}
	if engram.Store != "" {
		binDir := sandboxBinDir(in.RuntimeDir, executionID)
		engram.BinMount = filepath.Join(binDir, "engram")
		if err := os.MkdirAll(binDir, 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(engram.BinMount, nil, 0o600); err != nil {
			return "", err
		}
		pathFirst = binDir
	}
	in.Engram = engram
	az, err := sandboxAz(in, executionID)
	if err != nil {
		return "", err
	}
	in.Az = az
	if a.sandbox.Docker != nil {
		in.DockerSocket = a.sandbox.Docker.SocketPath(executionID)
	}
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
	if a.sandbox.Docker != nil {
		// What the proxy lets this agent's containers mount.
		if err := a.sandbox.Docker.WriteView(executionID, plan); err != nil {
			return "", err
		}
	}
	for k, v := range sandboxGitFetches(env) {
		plan.Setenv[k] = v
	}
	if a.sandbox.ToolLogin != nil {
		// Inside the sandbox only: the pane's own shell never holds it.
		login, err := a.sandbox.ToolLogin(ctx, agentKind)
		if err != nil {
			return "", err
		}
		for k, v := range login {
			plan.Setenv[k] = v
		}
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
	dir := ""
	if in.RuntimeDir != "" {
		dir = filepath.Join(in.RuntimeDir, "sandbox", executionID)
	}
	for _, l := range plan.Links {
		if in.RuntimeDir != "" && filepath.Dir(l.Path) == sandboxBinDir(in.RuntimeDir, executionID) {
			// A command linked into the execution's bin (az's wrapper on
			// macOS) is found before any other of its name.
			pathFirst = filepath.Dir(l.Path)
		}
	}
	line, err := backend.Prefix(plan, dir)
	if err != nil {
		return "", err
	}
	// The shell that runs the line expands $PATH, so what the session's
	// shell put on it stays, after the engram's folder.
	if pathFirst != "" {
		line = "PATH=" + shellWord(pathFirst) + `:"$PATH" ` + line
	}
	return line, nil
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

// shellWord quotes a word for the shell only when it needs it.
func shellWord(s string) string { return sandbox.ShellWord(s) }

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
//
// The paths are compared where they really are: the data folder and the
// clone's record may name it through a link (a /home that is var/home)
// while a working folder read back from the agent's pane does not.
func (a *agentService) taskJobClone(ctx context.Context, taskID, dataDir, workDir string) string {
	if dataDir == "" {
		return ""
	}
	jobs := filepath.Join(dataDir, "jobs")
	realJobs := HostFS{}.Real(jobs)
	rel, err := filepath.Rel(realJobs, HostFS{}.Real(workDir))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ""
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 {
		return ""
	}
	// The record names the clone as the data folder did when it was made.
	for _, path := range []string{filepath.Join(jobs, parts[0], parts[1]), filepath.Join(realJobs, parts[0], parts[1])} {
		clone, err := a.store.JobCloneByPath(ctx, path)
		if err == nil && slices.Contains(clone.TaskIDs, taskID) {
			return path
		}
	}
	return ""
}
