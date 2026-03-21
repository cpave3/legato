package tmux

import (
	"fmt"
	"os/exec"
	"strings"
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
