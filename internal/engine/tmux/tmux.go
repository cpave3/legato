package tmux

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Options configures the tmux Manager.
type Options struct {
	// EscapeKey is the tmux key binding used to detach. Default: "C-]".
	EscapeKey string
	// LookPath overrides exec.LookPath for testing. If nil, uses exec.LookPath.
	LookPath func(name string) (string, error)
}

// Manager wraps tmux CLI operations.
type Manager struct {
	tmuxPath  string
	escapeKey string
}

// New creates a Manager, verifying tmux is available.
func New(opts Options) (*Manager, error) {
	lookup := opts.LookPath
	if lookup == nil {
		lookup = exec.LookPath
	}

	path, err := lookup("tmux")
	if err != nil {
		return nil, fmt.Errorf("tmux not found: %w", err)
	}

	escapeKey := opts.EscapeKey
	if escapeKey == "" {
		escapeKey = "C-]"
	}

	return &Manager{
		tmuxPath:  path,
		escapeKey: escapeKey,
	}, nil
}

// Spawn creates a new detached tmux session with the given name and working directory.
// Width and height set the initial pane geometry via -x/-y flags; zero values omit the flags.
// Optional env vars are injected via -e flags so the initial shell inherits them.
func (m *Manager) Spawn(name, workDir string, width, height int, envVars ...string) error {
	args := []string{"new-session", "-d", "-s", name, "-c", workDir}
	if width > 0 && height > 0 {
		args = append(args, "-x", fmt.Sprintf("%d", width), "-y", fmt.Sprintf("%d", height))
	}
	for _, e := range envVars {
		args = append(args, "-e", e)
	}
	cmd := exec.Command(m.tmuxPath, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux new-session: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// Kill destroys a tmux session. Returns nil if the session doesn't exist.
func (m *Manager) Kill(name string) error {
	cmd := exec.Command(m.tmuxPath, "kill-session", "-t", name)
	if err := cmd.Run(); err != nil {
		// If session doesn't exist, that's fine
		if m.isNotFoundError(name) {
			return nil
		}
		return fmt.Errorf("tmux kill-session: %w", err)
	}
	return nil
}

// Capture returns the current visible content of the session's pane.
func (m *Manager) Capture(name string) (string, error) {
	cmd := exec.Command(m.tmuxPath, "capture-pane", "-t", name, "-p")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

// CaptureWithEscapes returns the pane content including ANSI escape sequences
// and the full scrollback history (not just the visible viewport). Suitable
// for feeding into a terminal emulator (e.g. xterm.js) but not for text
// processing (use Capture for that).
func (m *Manager) CaptureWithEscapes(name string) (string, error) {
	cmd := exec.Command(m.tmuxPath, "capture-pane", "-t", name, "-p", "-e", "-S", "-")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

// Attach returns an exec.Cmd that attaches to the named tmux session.
// The escape key is set as the detach binding for this session.
// Use with tea.ExecProcess to suspend Bubbletea.
func (m *Manager) Attach(name string) *exec.Cmd {
	// We run a shell that first sets the escape key binding, then attaches.
	// This avoids modifying the user's global tmux config.
	script := fmt.Sprintf(
		`%s set-option -t %s -g prefix None \; bind-key -n %s detach-client \; attach-session -t %s`,
		m.tmuxPath, name, m.escapeKey, name,
	)
	return exec.Command("sh", "-c", script)
}

// ListSessions returns the names of all legato-prefixed tmux sessions.
func (m *Manager) ListSessions() ([]string, error) {
	cmd := exec.Command(m.tmuxPath, "list-sessions", "-F", "#{session_name}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// No server running = no sessions
		if strings.Contains(string(out), "no server running") ||
			strings.Contains(string(out), "no sessions") {
			return nil, nil
		}
		return nil, fmt.Errorf("tmux list-sessions: %s: %w", strings.TrimSpace(string(out)), err)
	}

	var sessions []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "legato-") {
			sessions = append(sessions, line)
		}
	}
	return sessions, nil
}

// IsAlive checks whether a tmux session with the given name exists.
func (m *Manager) IsAlive(name string) (bool, error) {
	cmd := exec.Command(m.tmuxPath, "has-session", "-t", name)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	// Exit code 1 means session doesn't exist — not an error for our purposes.
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("tmux has-session: %w", err)
}

// SendKeys sends literal text to the named tmux session's active pane.
// The text is sent verbatim (using -- to prevent key name interpretation).
func (m *Manager) SendKeys(name, keys string) error {
	cmd := exec.Command(m.tmuxPath, "send-keys", "-t", name, "--", keys)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux send-keys: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// SendKey sends a named key (e.g. "Enter", "Escape") to the session.
// Unlike SendKeys, this does NOT use -- so tmux interprets key names.
func (m *Manager) SendKey(name, key string) error {
	cmd := exec.Command(m.tmuxPath, "send-keys", "-t", name, key)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux send-keys: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// SendKeysLine sends a line of text followed by Enter as two separate
// `tmux send-keys` calls with a small delay between them.
//
// Why split + delay instead of one call: any TUI receiver with bracketed-paste
// mode enabled (bash, Chimera, anything using a modern input library) treats
// text+Enter delivered in one tmux invocation as a paste — and the Enter
// inside a paste is interpreted as a literal newline rather than a submit.
// The split, with a brief gap, prevents the receiver from coalescing the
// keys into a paste envelope. Confirmed broken with single-call delivery
// against Chimera's input handler.
//
// Use this for any send-keys that should result in an executed/submitted
// input — shell commands, agent prompts, conductor wake-ups, all of it.
//
// Higher-level callers that fire multiple SendKeysLine calls in rapid
// succession to the same target should serialize them (e.g. SwarmService's
// per-conductor mutex) and add a small inter-message gap so the receiver
// can process each turn before the next arrives.
func (m *Manager) SendKeysLine(name, line string) error {
	if err := m.SendKeys(name, line); err != nil {
		return err
	}
	time.Sleep(sendKeysInterCallGap)
	return m.SendKey(name, "Enter")
}

// SendKeysShellCommand is retained as a name-clear alias for SendKeysLine.
// Both have identical semantics now that SendKeysLine uses the split + gap
// approach universally — the distinction was never about the receiver, it
// was about whether bracketed paste would trap the input. All callers should
// prefer SendKeysLine; this alias exists to avoid churning existing call
// sites that explicitly chose "Shell" for clarity.
func (m *Manager) SendKeysShellCommand(name, command string) error {
	return m.SendKeysLine(name, command)
}

// sendKeysInterCallGap is the pause between the text and Enter halves of a
// SendKeysLine. Long enough that any bracketed-paste accumulator at the
// receiver clears between the two key events; short enough that humans don't
// notice latency on legato-driven input.
const sendKeysInterCallGap = 75 * time.Millisecond

// SendKeysMultiline delivers a payload that may contain newlines or quote
// characters. When the payload is multi-line or contains characters that
// would otherwise need careful escaping when sent through tmux, the message
// is base64-encoded and wrapped: `[swarm event b64:<encoded>]\n`. Receiving
// agents are expected to decode `b64:` envelopes via their role prompt.
//
// For single-line payloads with no embedded quotes or newlines, this is
// equivalent to SendKeysLine.
func (m *Manager) SendKeysMultiline(name, payload string) error {
	if needsBase64(payload) {
		encoded := base64.StdEncoding.EncodeToString([]byte(payload))
		return m.SendKeysLine(name, "[swarm event b64:"+encoded+"]")
	}
	return m.SendKeysLine(name, payload)
}

// needsBase64 reports whether the payload contains characters that warrant
// base64 wrapping for safe send-keys delivery (newlines, embedded literal
// quotes, or carriage returns).
func needsBase64(s string) bool {
	return strings.ContainsAny(s, "\n\r\"")
}

// PipeOutput starts piping the pane's PTY output to a FIFO and returns
// a reader for that stream. Call the returned close func to stop piping
// and clean up the FIFO.
func (m *Manager) PipeOutput(name string) (io.Reader, func(), error) {
	dir, err := os.MkdirTemp("", "legato-pipe-*")
	if err != nil {
		return nil, nil, fmt.Errorf("creating pipe dir: %w", err)
	}

	fifoPath := filepath.Join(dir, "pane.fifo")
	if err := syscall.Mkfifo(fifoPath, 0600); err != nil {
		os.RemoveAll(dir)
		return nil, nil, fmt.Errorf("mkfifo: %w", err)
	}

	// Clear any stale pipe-pane from a previous process before starting a new one.
	exec.Command(m.tmuxPath, "pipe-pane", "-t", name).Run()

	// Start pipe-pane — tmux writes pane output to cat which writes to the FIFO.
	cmd := exec.Command(m.tmuxPath, "pipe-pane", "-o", "-t", name,
		fmt.Sprintf("cat > %s", fifoPath))
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return nil, nil, fmt.Errorf("tmux pipe-pane: %s: %w", strings.TrimSpace(string(out)), err)
	}

	// Open FIFO for reading (this blocks until the writer opens it, which
	// pipe-pane's cat does immediately).
	f, err := os.Open(fifoPath)
	if err != nil {
		// Stop the pipe and clean up.
		exec.Command(m.tmuxPath, "pipe-pane", "-t", name).Run()
		os.RemoveAll(dir)
		return nil, nil, fmt.Errorf("opening fifo: %w", err)
	}

	cleanup := func() {
		// Stop pipe-pane (no shell-command arg = close existing pipe).
		exec.Command(m.tmuxPath, "pipe-pane", "-t", name).Run()
		f.Close()
		os.RemoveAll(dir)
	}

	return f, cleanup, nil
}

// SetEnv sets an environment variable in the given tmux session.
// The variable will be available to new processes started in the session.
func (m *Manager) SetEnv(sessionName, key, value string) error {
	cmd := exec.Command(m.tmuxPath, "set-environment", "-t", sessionName, key, value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux set-environment: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// PaneCommands returns a map of legato-prefixed session names to their
// current foreground process name. Returns an empty map when no sessions match.
func (m *Manager) PaneCommands() (map[string]string, error) {
	cmd := exec.Command(m.tmuxPath, "list-panes", "-a",
		"-F", "#{session_name} #{pane_current_command}",
		"-f", "#{m:legato-*,#{session_name}}")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// No server running = no sessions
		if strings.Contains(string(out), "no server running") ||
			strings.Contains(string(out), "no sessions") {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("tmux list-panes: %s: %w", strings.TrimSpace(string(out)), err)
	}

	result := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result, nil
}

// SetOption sets a tmux option on the given session.
func (m *Manager) SetOption(sessionName, key, value string) error {
	cmd := exec.Command(m.tmuxPath, "set-option", "-t", sessionName, key, value)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux set-option: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (m *Manager) isNotFoundError(name string) bool {
	alive, _ := m.IsAlive(name)
	return !alive
}
