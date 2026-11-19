package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// DoctorResult is how one check of `legato doctor` came out.
type DoctorResult string

const (
	DoctorOK          DoctorResult = "ok"
	DoctorMissing     DoctorResult = "missing"
	DoctorNotSignedIn DoctorResult = "not_signed_in"
	DoctorFailed      DoctorResult = "failed"
	// DoctorSkipped is a check that could not run, such as the sign-in of
	// a tool that is missing.
	DoctorSkipped DoctorResult = "skipped"
)

// Check is one thing the doctor verifies about the machine Legato runs on
// (GLOSSARY.md), with its result and, when it is not ok, the fix.
type DoctorCheck struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Required bool   `json:"required"`
	// RequiredBy says which setting made an optional check required.
	RequiredBy string       `json:"required_by,omitempty"`
	Result     DoctorResult `json:"result"`
	Detail     string       `json:"detail,omitempty"`
	Fix        string       `json:"fix,omitempty"`
}

// Failed reports whether the check is not ok. A skipped check has not
// failed: what it depends on has, and says so.
func (c DoctorCheck) Failed() bool { return c.Result != DoctorOK && c.Result != DoctorSkipped }

// DoctorReport is the result of one run of the checks.
type DoctorReport struct {
	Checks    []DoctorCheck `json:"checks"`
	Online    bool          `json:"online"`
	CheckedAt time.Time     `json:"checked_at"`
}

// RequiredFailed lists the required checks that failed.
func (r DoctorReport) RequiredFailed() []DoctorCheck {
	var out []DoctorCheck
	for _, c := range r.Checks {
		if c.Required && c.Failed() {
			out = append(out, c)
		}
	}
	return out
}

// OK reports whether every required check passed.
func (r DoctorReport) OK() bool { return len(r.RequiredFailed()) == 0 }

// DoctorConfig is what the settings say the doctor must require.
type DoctorConfig struct {
	// DefaultAgent is the agent tool started when none is named
	// (claude-code, codex or chimera); empty is claude-code.
	DefaultAgent string
	// SandboxEnabled requires bwrap.
	SandboxEnabled bool
	// RequiredBy makes optional checks required, by check ID, with the
	// setting that needs them, such as gh for PR tracking.
	RequiredBy map[string]string
}

// DoctorOptions are the doctor's ways of looking at the machine; tests give
// fakes. Left zero, each is the real one.
type DoctorOptions struct {
	// Run runs a command and returns its standard output. A command that
	// exits non-zero returns an error; one cut off by ctx returns ctx's
	// error.
	Run      func(ctx context.Context, path string, args ...string) ([]byte, error)
	LookPath func(name string) (string, error)
	Getenv   func(key string) string
	ReadFile func(path string) ([]byte, error)
	Now      func() time.Time
	// Timeout bounds each command; 0 is DoctorTimeout.
	Timeout time.Duration
	// SpawnBound is the longest SpawnCheck holds a spawn; 0 is
	// DoctorSpawnBound.
	SpawnBound time.Duration
	// SpawnMaxAge is how long a passed check spares a spawn from running
	// it again; 0 is DoctorSpawnMaxAge.
	SpawnMaxAge time.Duration
}

// DoctorTimeout is how long one command of a check may take.
const DoctorTimeout = 5 * time.Second

// DoctorSpawnBound is the longest a spawn waits for its tool's check.
const DoctorSpawnBound = 300 * time.Millisecond

// DoctorSpawnMaxAge is how long a passed tool check stays fresh for spawns.
const DoctorSpawnMaxAge = 10 * time.Minute

// Doctor runs the checks of what Legato needs from the machine it runs on.
// It runs commands only to ask a version or whether credentials are present:
// it never starts a session, spends model tokens or opens a browser, and it
// uses the network only for the online check.
type Doctor struct {
	opts DoctorOptions

	mu sync.Mutex
	// installed is the last installed-and-runnable check of each tool,
	// which spawns read.
	installed map[string]timedCheck
}

type timedCheck struct {
	check DoctorCheck
	at    time.Time
}

// NewDoctor returns a doctor that looks at the machine through opts.
func NewDoctor(opts DoctorOptions) *Doctor {
	if opts.Run == nil {
		opts.Run = runDoctorCommand
	}
	if opts.LookPath == nil {
		opts.LookPath = exec.LookPath
	}
	if opts.Getenv == nil {
		opts.Getenv = os.Getenv
	}
	if opts.ReadFile == nil {
		opts.ReadFile = os.ReadFile
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DoctorTimeout
	}
	if opts.SpawnBound <= 0 {
		opts.SpawnBound = DoctorSpawnBound
	}
	if opts.SpawnMaxAge <= 0 {
		opts.SpawnMaxAge = DoctorSpawnMaxAge
	}
	return &Doctor{opts: opts, installed: map[string]timedCheck{}}
}

// runDoctorCommand runs a command with no input, its error carrying the
// first line it wrote to stderr.
func runDoctorCommand(ctx context.Context, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if line := doctorLine(string(exitErr.Stderr)); line != "" {
			return out, fmt.Errorf("%w: %s", err, line)
		}
	}
	return out, err
}

// doctorTool is an agent tool or command-line tool the doctor checks.
type doctorTool struct {
	id      string // check ID
	command string
	title   string
	args    []string
	install string // the fix when it is missing or broken
	// verify reads the version command's output; nil accepts any.
	verify func(out []byte) error
	// signIn checks credentials; nil for a tool with none.
	signIn func(ctx context.Context, d *Doctor, path string) DoctorCheck
}

func doctorTools() []doctorTool {
	return []doctorTool{
		{id: "tmux", command: "tmux", title: "tmux", args: []string{"-V"},
			install: "Install tmux (apt install tmux, brew install tmux): agents run in tmux sessions."},
		{id: "git", command: "git", title: "git", args: []string{"--version"},
			install: "Install git (apt install git, brew install git)."},
		{id: "claude-code", command: "claude", title: "Claude Code", args: []string{"--version"},
			install: "Install Claude Code: https://code.claude.com/docs/en/setup",
			signIn: func(ctx context.Context, d *Doctor, path string) DoctorCheck {
				return d.exitCodeSignIn(ctx, path, []string{"auth", "status"}, "Run claude auth login, or start claude and sign in.")
			}},
		{id: "codex", command: "codex", title: "Codex", args: []string{"--version"},
			install: "Install Codex: npm install -g @openai/codex",
			signIn: func(ctx context.Context, d *Doctor, path string) DoctorCheck {
				return d.exitCodeSignIn(ctx, path, []string{"login", "status"}, "Run codex login.")
			}},
		{id: "chimera", command: "chimera", title: "Chimera", args: []string{"version", "--json"},
			install: "Install Chimera: https://github.com/cpave3/chimera",
			verify:  verifyChimeraModAPI,
			signIn: func(ctx context.Context, d *Doctor, _ string) DoctorCheck {
				return d.chimeraSignIn()
			}},
		{id: "gh", command: "gh", title: "GitHub CLI", args: []string{"--version"},
			install: "Install the GitHub CLI: https://cli.github.com",
			signIn: func(ctx context.Context, d *Doctor, path string) DoctorCheck {
				return d.exitCodeSignIn(ctx, path, []string{"auth", "token"}, "Run gh auth login.")
			}},
		{id: "st", command: "st", title: "Staccato (st)", args: []string{"version", "--json"},
			install: "Install Staccato: https://github.com/cpave3/staccato"},
		{id: "yg", command: "yg", title: "Yggdrasil (yg)", args: []string{"--help"},
			install: "Install Yggdrasil: https://github.com/cpave3/yggdrasil"},
		{id: "bwrap", command: "bwrap", title: "bubblewrap (sandbox)", args: []string{"--version"},
			install: "Install bubblewrap (apt install bubblewrap), or turn the sandbox off (agents.sandbox.enabled)."},
	}
}

// chimeraModAPI is the version of Chimera's mod API Legato's mod needs.
const chimeraModAPI = 1

func verifyChimeraModAPI(out []byte) error {
	var v struct {
		ModAPI []int `json:"modApi"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return fmt.Errorf("chimera version --json did not answer JSON")
	}
	if !slices.Contains(v.ModAPI, chimeraModAPI) {
		return fmt.Errorf("this Chimera is too old: Legato needs mod API %d", chimeraModAPI)
	}
	return nil
}

// Check runs every check. online adds the check that asks GitHub whether
// gh's token works.
func (d *Doctor) Check(ctx context.Context, cfg DoctorConfig, online bool) DoctorReport {
	tools := doctorTools()
	groups := make([][]DoctorCheck, len(tools)+1)
	var wg sync.WaitGroup
	for i, tool := range tools {
		wg.Add(1)
		go func() {
			defer wg.Done()
			groups[i] = d.checkTool(ctx, tool, online)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		groups[len(tools)] = []DoctorCheck{d.checkLoginShellPath(ctx)}
	}()
	wg.Wait()

	defaultAgent := cfg.DefaultAgent
	if defaultAgent == "" {
		defaultAgent = "claude-code"
	}
	required := map[string]string{"tmux": "", "git": "", defaultAgent: "the default agent tool", defaultAgent + ".auth": "the default agent tool"}
	if cfg.SandboxEnabled {
		required["bwrap"] = "the sandbox (agents.sandbox.enabled)"
	}
	for id, by := range cfg.RequiredBy {
		required[id] = by
	}
	report := DoctorReport{Online: online, CheckedAt: d.opts.Now()}
	for _, group := range groups {
		for _, c := range group {
			if by, ok := required[c.ID]; ok {
				c.Required, c.RequiredBy = true, by
			}
			report.Checks = append(report.Checks, c)
		}
	}
	return report
}

// CheckTool runs only the installed-and-runnable check of one tool, by
// check ID (such as claude-code), as a spawn does.
func (d *Doctor) CheckTool(ctx context.Context, id string) (DoctorCheck, bool) {
	for _, tool := range doctorTools() {
		if tool.id == id {
			_, c := d.checkInstalled(ctx, tool)
			return c, true
		}
	}
	return DoctorCheck{}, false
}

func (d *Doctor) checkTool(ctx context.Context, tool doctorTool, online bool) []DoctorCheck {
	path, installed := d.checkInstalled(ctx, tool)
	checks := []DoctorCheck{installed}
	if tool.signIn == nil {
		return checks
	}
	signIn := DoctorCheck{ID: tool.id + ".auth", Title: tool.title + " signed in"}
	if installed.Result != DoctorOK {
		signIn.Result, signIn.Detail = DoctorSkipped, tool.title+" is not installed and runnable"
	} else {
		signIn = tool.signIn(ctx, d, path)
		signIn.ID, signIn.Title = tool.id+".auth", tool.title+" signed in"
	}
	checks = append(checks, signIn)
	if tool.id == "gh" && online {
		check := DoctorCheck{ID: "gh.online", Title: "GitHub token works (online)"}
		if installed.Result != DoctorOK {
			check.Result, check.Detail = DoctorSkipped, tool.title+" is not installed and runnable"
		} else {
			check = d.exitCodeSignIn(ctx, path, []string{"auth", "status"}, "Run gh auth login, or replace the token gh uses.")
			check.ID, check.Title = "gh.online", "GitHub token works (online)"
			if check.Result == DoctorOK {
				check.Detail = "GitHub accepted the token"
			} else if check.Result == DoctorNotSignedIn {
				check.Detail += " (offline, a good token looks the same)"
			}
		}
		checks = append(checks, check)
	}
	return checks
}

// AgentToolUnavailableError refuses a spawn whose agent tool is missing or
// cannot run, with the check that says so and its fix.
type AgentToolUnavailableError struct {
	Check DoctorCheck
}

func (e *AgentToolUnavailableError) Error() string {
	msg := e.Check.Title + " cannot start: " + e.Check.Detail
	if e.Check.Fix != "" {
		msg += ". " + e.Check.Fix
	}
	return msg
}

// SpawnCheck says whether an agent tool (by check ID, which is its adapter
// name) can start, for a spawn: nil, or an *AgentToolUnavailableError. A
// check passed within SpawnMaxAge is taken as it is and runs nothing.
// Otherwise the tool's installed-and-runnable check runs, and the spawn
// waits for it at most SpawnBound: a check that has not answered by then
// lets the spawn go on and keeps running, so the next spawn knows. A tool
// the doctor does not know is let through.
func (d *Doctor) SpawnCheck(ctx context.Context, tool string) error {
	var known doctorTool
	for _, t := range doctorTools() {
		if t.id == tool {
			known = t
		}
	}
	if known.id == "" {
		return nil
	}
	d.mu.Lock()
	last, ok := d.installed[tool]
	d.mu.Unlock()
	if ok && last.check.Result == DoctorOK && d.opts.Now().Sub(last.at) < d.opts.SpawnMaxAge {
		return nil
	}
	done := make(chan DoctorCheck, 1)
	go func() {
		_, c := d.checkInstalled(context.WithoutCancel(ctx), known)
		done <- c
	}()
	timer := time.NewTimer(d.opts.SpawnBound)
	defer timer.Stop()
	select {
	case c := <-done:
		if c.Result != DoctorOK {
			return &AgentToolUnavailableError{Check: c}
		}
		return nil
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Doctor) checkInstalled(ctx context.Context, tool doctorTool) (string, DoctorCheck) {
	path, c := d.runInstalled(ctx, tool)
	d.mu.Lock()
	d.installed[tool.id] = timedCheck{check: c, at: d.opts.Now()}
	d.mu.Unlock()
	return path, c
}

func (d *Doctor) runInstalled(ctx context.Context, tool doctorTool) (string, DoctorCheck) {
	c := DoctorCheck{ID: tool.id, Title: tool.title}
	path, err := d.opts.LookPath(tool.command)
	if err != nil {
		c.Result, c.Detail, c.Fix = DoctorMissing, tool.command+" not found on Legato's PATH", tool.install
		return "", c
	}
	call := strings.Join(append([]string{tool.command}, tool.args...), " ")
	out, err := d.run(ctx, path, tool.args...)
	if err == nil && tool.verify != nil {
		err = tool.verify(out)
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		c.Result, c.Detail = DoctorFailed, fmt.Sprintf("%s is at %s but `%s` did not answer within %s", tool.command, path, call, d.opts.Timeout)
		c.Fix = "Reinstall it. " + tool.install
	case err != nil:
		c.Result, c.Detail = DoctorFailed, fmt.Sprintf("%s is at %s but `%s` failed: %s", tool.command, path, call, doctorLine(err.Error()))
		c.Fix = "Reinstall or upgrade it. " + tool.install
	default:
		c.Result, c.Detail = DoctorOK, doctorLine(string(out))
		if c.Detail == "" || tool.id == "yg" || strings.HasPrefix(c.Detail, "{") {
			c.Detail = path
		}
	}
	return path, c
}

func (d *Doctor) run(ctx context.Context, path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, d.opts.Timeout)
	defer cancel()
	return d.opts.Run(ctx, path, args...)
}

const credentialsPresent = "credentials are present (not proven to work)"

// exitCodeSignIn is a sign-in read by a command that exits 0 when
// credentials are present.
func (d *Doctor) exitCodeSignIn(ctx context.Context, path string, args []string, fix string) DoctorCheck {
	call := strings.Join(append([]string{filepath.Base(path)}, args...), " ")
	_, err := d.run(ctx, path, args...)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return DoctorCheck{Result: DoctorFailed, Detail: fmt.Sprintf("`%s` did not answer within %s", call, d.opts.Timeout), Fix: fix}
	case err != nil:
		return DoctorCheck{Result: DoctorNotSignedIn, Detail: fmt.Sprintf("`%s` says not signed in", call), Fix: fix}
	}
	return DoctorCheck{Result: DoctorOK, Detail: credentialsPresent}
}

// chimeraSignIn reads Chimera's config: a provider and a default model,
// with each env: key set. Chimera has no command that says so.
func (d *Doctor) chimeraSignIn() DoctorCheck {
	home := d.opts.Getenv("HOME")
	path := filepath.Join(home, ".chimera", "config.json")
	fix := "Add a provider and a defaultModel to ~/.chimera/config.json."
	data, err := d.opts.ReadFile(path)
	if err != nil {
		return DoctorCheck{Result: DoctorNotSignedIn, Detail: "no ~/.chimera/config.json", Fix: fix}
	}
	var cfg struct {
		Providers    json.RawMessage `json:"providers"`
		DefaultModel string          `json:"defaultModel"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return DoctorCheck{Result: DoctorFailed, Detail: "~/.chimera/config.json is not valid JSON: " + err.Error(), Fix: "Fix ~/.chimera/config.json."}
	}
	providers := chimeraProviders(cfg.Providers)
	if len(providers) == 0 {
		return DoctorCheck{Result: DoctorNotSignedIn, Detail: "~/.chimera/config.json has no provider", Fix: fix}
	}
	if cfg.DefaultModel == "" {
		return DoctorCheck{Result: DoctorNotSignedIn, Detail: "~/.chimera/config.json has no defaultModel", Fix: fix}
	}
	var unset []string
	for _, p := range providers {
		if name, ok := strings.CutPrefix(p.APIKey, "env:"); ok && d.opts.Getenv(name) == "" {
			unset = append(unset, name)
		}
	}
	if len(unset) > 0 {
		slices.Sort(unset)
		return DoctorCheck{Result: DoctorNotSignedIn, Detail: "Chimera reads " + strings.Join(unset, ", ") + ", which is not set",
			Fix: "Set " + strings.Join(unset, ", ") + " in the environment agents start in."}
	}
	return DoctorCheck{Result: DoctorOK, Detail: credentialsPresent}
}

type chimeraProvider struct {
	APIKey string `json:"apiKey"`
}

// chimeraProviders reads Chimera's providers, kept by name or as a list.
func chimeraProviders(raw json.RawMessage) []chimeraProvider {
	var byName map[string]chimeraProvider
	if json.Unmarshal(raw, &byName) == nil {
		out := make([]chimeraProvider, 0, len(byName))
		for _, p := range byName {
			out = append(out, p)
		}
		return out
	}
	var list []chimeraProvider
	_ = json.Unmarshal(raw, &list)
	return list
}

// checkLoginShellPath compares the PATH of the operator's login shell, in
// which agents start, with Legato's own.
func (d *Doctor) checkLoginShellPath(ctx context.Context) DoctorCheck {
	c := DoctorCheck{ID: "path", Title: "Login shell PATH matches Legato's"}
	shell := d.opts.Getenv("SHELL")
	if shell == "" {
		c.Result, c.Detail = DoctorSkipped, "SHELL is not set"
		return c
	}
	out, err := d.run(ctx, shell, "-lc", `printf %s "$PATH"`)
	if err != nil {
		c.Result, c.Detail = DoctorSkipped, fmt.Sprintf("could not read %s's PATH: %s", shell, doctorLine(err.Error()))
		return c
	}
	// A login shell may print a banner first; PATH is the last line.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	shellPath := splitPath(lines[len(lines)-1])
	ownPath := splitPath(d.opts.Getenv("PATH"))
	onlyShell, onlyOwn := pathDifference(shellPath, ownPath), pathDifference(ownPath, shellPath)
	if len(onlyShell) == 0 && len(onlyOwn) == 0 {
		c.Result, c.Detail = DoctorOK, "agents find the same tools Legato does"
		return c
	}
	var parts []string
	if len(onlyShell) > 0 {
		parts = append(parts, "only the login shell has "+strings.Join(onlyShell, ", "))
	}
	if len(onlyOwn) > 0 {
		parts = append(parts, "only Legato has "+strings.Join(onlyOwn, ", "))
	}
	c.Result, c.Detail = DoctorFailed, strings.Join(parts, "; ")
	c.Fix = "Start Legato from your login shell, or give its service the same PATH, so agents and Legato find the same tools."
	return c
}

func splitPath(p string) []string {
	var out []string
	for _, dir := range filepath.SplitList(p) {
		if dir != "" && !slices.Contains(out, dir) {
			out = append(out, dir)
		}
	}
	return out
}

func pathDifference(a, b []string) []string {
	var out []string
	for _, dir := range a {
		if !slices.Contains(b, dir) {
			out = append(out, dir)
		}
	}
	return out
}

func doctorLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}
