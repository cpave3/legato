package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"github.com/cpave3/legato/config"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

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
	// SocketWait bounds how long a launch waits for the execution's
	// sockets the sandbox binds (sandboxSockets); 0 is
	// defaultSandboxSocketWait.
	SocketWait time.Duration
}

// defaultSandboxSocketWait outlasts the channel host's reconcile tick (5 s):
// when another process serves the credential channels, it opens a new
// execution's socket on its next reconcile at the latest.
const defaultSandboxSocketWait = 15 * time.Second

// sandboxSockets are the sockets of an execution that the sandbox binds as
// files and that another process may open after the spawn has begun: bwrap
// refuses to start when a path it binds is missing (LEG-709). The broker's
// and Docker's are bound by their folders, so only the SSH agent's is here.
func sandboxSockets(in SandboxPlanInput) []string {
	if in.SSHSocket == "" {
		return nil
	}
	return []string{in.SSHSocket}
}

// runLaunch issues a launch in its pane once the sockets its sandbox binds
// exist: the launch is immediate, so a socket the channel host has yet to
// open would stop bwrap (LEG-709).
func (a *agentService) runLaunch(ctx context.Context, sessionName string, argv, sockets, envVars []string) error {
	if err := a.sandbox.awaitSandboxSockets(ctx, sockets); err != nil {
		return err
	}
	return a.tmux.RunInPane(sessionName, argv, envVars...)
}

// awaitSandboxSockets returns once every path exists, or an error naming
// those still missing after the settings' wait.
func (s *SandboxSettings) awaitSandboxSockets(ctx context.Context, paths []string) error {
	missing := missingPaths(paths)
	if len(missing) == 0 {
		return nil
	}
	wait := cmp.Or(s.SocketWait, defaultSandboxSocketWait)
	deadline := time.Now().Add(wait)
	for len(missing) > 0 {
		if time.Now().After(deadline) {
			return fmt.Errorf("the sandbox binds %s, which was not there after %s", strings.Join(missing, ", "), wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
		missing = missingPaths(paths)
	}
	return nil
}

// missingPaths are those of paths that do not exist.
func missingPaths(paths []string) []string {
	var missing []string
	for _, p := range paths {
		if _, err := os.Lstat(p); err != nil {
			missing = append(missing, p)
		}
	}
	return missing
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
// in its sandbox, ending in "--". attributionTaskID is the task the agent's
// execution concerns: a workflow agent's may differ from taskID, and that
// task's job clone is the agent's to work in too.
func (a *agentService) sandboxPrefix(ctx context.Context, taskID, attributionTaskID, executionID, agentKind, workDir string, env map[string]string, resources []Resource, readOnly, throwaway, dockerHostNetwork bool, engram SandboxEngram) (string, []string, error) {
	backend := a.sandbox.Backend
	if err := backend.Available(); err != nil {
		return "", nil, err
	}
	in := a.sandbox.Base
	in.WorkDir, in.ExecutionID, in.TaskID, in.AgentKind = workDir, executionID, taskID, agentKind
	in.ReadOnlyCheckout = readOnly
	// Without overlays, a throwaway checkout is the plain read-only one.
	in.ThrowawayCheckout = readOnly && throwaway && a.CheckoutOverlayAvailable()
	in.BrokerSocket = env["LEGATO_BROKER_SOCKET"]
	if in.RuntimeDir != "" {
		// The options then go through a private file (argsFileCommand),
		// so a placeholder never reaches bwrap's command line.
		in.SessionEnv = sessionEnvValues(env)
	}
	if file := env["LEGATO_CHIMERA_SECRET_FILE"]; file != "" {
		in.ChimeraSecretDir = filepath.Dir(file)
	}
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
	// The execution's skills, when it was given any.
	if dir := a.capabilityFolders.Dir(executionID); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			in.CapabilitiesDir = dir
		}
	}
	if dir, err := EnsurePastedImageDir(taskID); err == nil {
		in.PastedImagesDir = dir
	} else {
		// The agent still starts; only images pasted for it will not show.
		log.Printf("sandbox: the pasted images folder of %s: %v", taskID, err)
	}
	in.ToolState = toolStateDirs(agentKind, env, in.Home)
	in = withClaudeAccounts(in, agentKind, env["CLAUDE_CONFIG_DIR"], os.Getenv("CLAUDE_CONFIG_DIR"), a.claudeAccounts)
	for _, dir := range in.ToolState {
		// Made when missing, so what the tool writes there lands on the host
		// and not in the sandbox's empty home.
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", nil, err
		}
	}
	in.GitCommonDir = gitCommonDir(ctx, workDir)
	jobDir, err := a.taskJobClone(ctx, in.DataDir, workDir, taskID, attributionTaskID)
	if err != nil {
		return "", nil, err
	}
	in.JobDir = jobDir
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
			return "", nil, err
		}
		in.SSHConfig = copies
	}
	if gh, ok := sandboxGH(in.CredentialWrapperDir, env["PATH"], filepath.Join(in.RuntimeDir, "sandbox", executionID, "gh-real")); ok {
		if err := os.WriteFile(gh.RealMount, nil, 0o600); err != nil {
			return "", nil, err
		}
		in.GH = gh
	}
	// An engram session's binary, at a path of the execution's own whose
	// folder goes first on its PATH.
	pathFirst := ""
	if engram.Store != "" && in.RuntimeDir == "" {
		return "", nil, errors.New("an engram session needs Legato's runtime folder for its engram binary")
	}
	if engram.Store != "" {
		binDir := filepath.Join(in.RuntimeDir, "sandbox", executionID, "bin")
		engram.BinMount = filepath.Join(binDir, "engram")
		if err := os.MkdirAll(binDir, 0o700); err != nil {
			return "", nil, err
		}
		if err := os.WriteFile(engram.BinMount, nil, 0o600); err != nil {
			return "", nil, err
		}
		pathFirst = binDir
	}
	in.Engram = engram
	az, err := sandboxAz(in, executionID)
	if err != nil {
		return "", nil, err
	}
	in.Az = az
	if a.sandbox.Docker != nil {
		in.DockerSocket = a.sandbox.Docker.SocketPath(executionID)
	}
	// The scratch folder exists before the plan is made, so a plan whose
	// mounts hold it covers it on the first spawn too.
	if root := scratchFor(in).Root; root != "" {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return "", nil, err
		}
	}
	// These mounts must exist even on the first spawn: their writable
	// parent would otherwise let the agent install its own mods or hooks.
	for _, dir := range chimeraProtectedDirs(in) {
		if err := checkProtectedDir(HostFS{}, dir); err != nil {
			return "", nil, err
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", nil, fmt.Errorf("prepare Chimera sandbox directory %s: %w", dir, err)
		}
	}
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		return "", nil, err
	}
	if a.sandbox.Docker != nil {
		// What the proxy lets this agent's containers mount.
		if err := a.sandbox.Docker.WriteView(executionID, plan, dockerHostNetwork); err != nil {
			return "", nil, err
		}
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
			return "", nil, err
		}
		if dir != scratch.Root {
			if err := os.Chmod(dir, 0o700); err != nil {
				return "", nil, err
			}
		}
	}
	// The broker's folder is mounted whether or not a host serves it yet:
	// one that starts later is then reachable from inside.
	if in.BrokerSocket != "" {
		if err := os.MkdirAll(filepath.Dir(in.BrokerSocket), 0o700); err != nil {
			return "", nil, err
		}
	}
	args, err := backend.Wrap(plan, []string{"true"})
	if err != nil {
		return "", nil, err
	}
	args = args[:len(args)-1] // the backend's command, then its options, then "--"
	// The shell that runs the line expands $PATH, so what the session's
	// shell put on it stays, after the engram's folder.
	withPath := func(line string) string {
		if pathFirst == "" {
			return line
		}
		return "PATH=" + shellWord(pathFirst) + `:"$PATH" ` + line
	}
	if in.RuntimeDir != "" && len(args) >= 2 && args[len(args)-1] == "--" {
		file := filepath.Join(in.RuntimeDir, "sandbox", executionID, "bwrap.args")
		line, err := argsFileCommand(args[0], args[1:len(args)-1], file)
		return withPath(line), sandboxSockets(in), err
	}
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellWord(arg)
	}
	return withPath(strings.Join(quoted, " ")), sandboxSockets(in), nil
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

// taskJobClone is the job clone that holds workDir when one of taskIDs
// works in it (Legato made it for the task, or the task joined it), ""
// otherwise. Only such a clone of Legato's data folder can be a sandboxed
// agent's work: also when the agent is started again in it, and for a
// workflow agent about the task, such as the learning workflow's curator,
// which starts in the task's working folder (LEG-156).
//
// The paths are compared where they really are: the data folder and the
// clone's record may name it through a link (a /home that is var/home)
// while a working folder read back from the agent's pane does not.
func (a *agentService) taskJobClone(ctx context.Context, dataDir, workDir string, taskIDs ...string) (string, error) {
	if dataDir == "" {
		return "", nil
	}
	jobs := filepath.Join(dataDir, "jobs")
	realJobs := HostFS{}.Real(jobs)
	rel, err := filepath.Rel(realJobs, HostFS{}.Real(workDir))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", nil
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 2 {
		return "", nil
	}
	cloneRoot := filepath.Join(realJobs, parts[0], parts[1])
	for _, taskID := range taskIDs {
		if taskID == "" {
			continue
		}
		clones, err := a.store.ListLiveTaskJobClones(ctx, taskID)
		if err != nil {
			return "", fmt.Errorf("finding task %s's job clone: %w", taskID, err)
		}
		for _, clone := range clones {
			if (HostFS{}).Real(clone.Path) == cloneRoot {
				return cloneRoot, nil
			}
		}
	}
	return "", nil
}

// sessionEnvValues are the variables of a session's environment that Legato
// gives a value: SandboxPlanInput.SessionEnv.
func sessionEnvValues(env map[string]string) map[string]string {
	out := map[string]string{}
	for name, value := range env {
		if value != "" {
			out[name] = value
		}
	}
	return out
}

// withClaudeAccounts applies the Claude account rule to a sandbox's input.
// A claude-code agent works in its launch's CLAUDE_CONFIG_DIR (launchDir;
// explicit), else the home's .claude. Every other Claude folder is
// protected: Legato's own default (legatoDir, else the home's .claude),
// the home's .claude and each configured account's, except the one in
// use; and the home's .claude.json unless the agent is claude-code on the
// default account. Protected paths are added to a copy of the input's.
func withClaudeAccounts(in SandboxPlanInput, agentKind, launchDir, legatoDir string, accounts []config.ClaudeAccount) SandboxPlanInput {
	in.ProtectedClaudeAccounts = append([]string{}, in.ProtectedClaudeAccounts...)
	defaultClaude := legatoDir
	if defaultClaude == "" {
		defaultClaude = filepath.Join(in.Home, ".claude")
	}
	if agentKind == "claude-code" {
		in.ClaudeConfigDir = launchDir
		in.ClaudeConfigExplicit = in.ClaudeConfigDir != ""
		if in.ClaudeConfigDir == "" {
			in.ClaudeConfigDir = filepath.Join(in.Home, ".claude")
		}
	}
	roots := []string{defaultClaude, filepath.Join(in.Home, ".claude")}
	for _, account := range accounts {
		roots = append(roots, account.ConfigDir)
	}
	for _, root := range roots {
		selected := in.ClaudeConfigDir != "" && filepath.Clean(root) == filepath.Clean(in.ClaudeConfigDir)
		if !selected {
			in.ProtectedClaudeAccounts = append(in.ProtectedClaudeAccounts, root)
		}
	}
	if agentKind != "claude-code" || in.ClaudeConfigExplicit {
		in.ProtectedClaudeAccounts = append(in.ProtectedClaudeAccounts, filepath.Join(in.Home, ".claude.json"))
	}
	return in
}
