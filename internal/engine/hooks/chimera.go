package hooks

import (
	"fmt"
	"os"
	"path/filepath"
)

// chimeraScripts pairs each Chimera hook event with its installed script name
// and the activity state it reports. Empty activity means "clear".
var chimeraScripts = []struct {
	event    string
	script   string
	activity string
}{
	{"UserPromptSubmit", "legato-prompt-submit.sh", "working"},
	{"PostToolUse", "legato-post-tool-use.sh", "working"},
	{"PermissionRequest", "legato-permission.sh", "waiting"},
	{"Stop", "legato-stop.sh", ""},
	{"SessionEnd", "legato-session-end.sh", ""},
}

// ChimeraAdapter implements the AIToolAdapter interface for the Chimera coding agent.
type ChimeraAdapter struct {
	legatoBin string
}

// NewChimeraAdapter creates a Chimera adapter.
func NewChimeraAdapter(legatoBin string) *ChimeraAdapter {
	return &ChimeraAdapter{legatoBin: legatoBin}
}

func (a *ChimeraAdapter) Name() string { return "chimera" }

func (a *ChimeraAdapter) EnvVars(taskID, socketPath string) map[string]string {
	// LEGATO_TASK_ID is already injected into the tmux session by the active adapter
	// (currently ClaudeCodeAdapter). Chimera reads it via env passthrough.
	return nil
}

// InstallHooks writes the Legato activity-update scripts into Chimera's global
// hook directories. projectDir is unused: Chimera hooks are per-user, not per-project.
func (a *ChimeraAdapter) InstallHooks(projectDir string) error {
	for _, s := range chimeraScripts {
		dir := chimeraHookDir(s.event)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating chimera hook dir %s: %w", dir, err)
		}
		path := filepath.Join(dir, s.script)
		if err := os.WriteFile(path, []byte(agentStateScript(a.legatoBin, s.activity)), 0o755); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}

// UninstallHooks removes the Legato-managed scripts from Chimera's hook directories.
func (a *ChimeraAdapter) UninstallHooks(projectDir string) error {
	for _, s := range chimeraScripts {
		path := filepath.Join(chimeraHookDir(s.event), s.script)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", path, err)
		}
	}
	return nil
}

// chimeraHookDir returns the global Chimera hook directory for a given event.
// Chimera scans ~/.chimera/hooks/<EventName>/ on every event firing.
func chimeraHookDir(event string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".chimera", "hooks", event)
}
