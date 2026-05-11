package swarm

import (
	"fmt"
	"os"
	"path/filepath"
)

// LegatoHome returns the legato runtime root directory.
// Honors $LEGATO_HOME for tests/overrides, otherwise falls
// back to $HOME/.legato. Does NOT create the directory.
func LegatoHome() (string, error) {
	if h := os.Getenv("LEGATO_HOME"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user home dir: %w", err)
	}
	return filepath.Join(home, ".legato"), nil
}

// PlansDir returns ~/.legato/plans, ensuring it exists (0o755).
func PlansDir() (string, error) {
	root, err := LegatoHome()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "plans")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create plans dir: %w", err)
	}
	return dir, nil
}

// AgentDir returns ~/.legato/agents/<taskID>, ensuring it exists (0o755).
// Returns an error if taskID is empty.
func AgentDir(taskID string) (string, error) {
	if taskID == "" {
		return "", fmt.Errorf("taskID is required")
	}
	root, err := LegatoHome()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "agents", taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create agent dir: %w", err)
	}
	return dir, nil
}
