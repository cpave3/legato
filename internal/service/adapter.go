package service

import (
	"fmt"
	"sort"
)

// AIToolAdapter abstracts an AI tool integration (Claude Code, Aider, etc.).
type AIToolAdapter interface {
	// Name returns the adapter's unique identifier (e.g., "claude-code").
	Name() string
	// InstallHooks configures the tool's hook system in the given project directory.
	InstallHooks(projectDir string) error
	// UninstallHooks removes previously installed hooks from the project directory.
	UninstallHooks(projectDir string) error
	// EnvVars returns environment variables to inject into tmux sessions.
	EnvVars(taskID, socketPath string) map[string]string
}

// AdapterRegistry holds registered AI tool adapters.
type AdapterRegistry struct {
	adapters map[string]AIToolAdapter
}

// NewAdapterRegistry creates an empty adapter registry.
func NewAdapterRegistry() *AdapterRegistry {
	return &AdapterRegistry{adapters: make(map[string]AIToolAdapter)}
}

// Register adds an adapter to the registry.
func (r *AdapterRegistry) Register(a AIToolAdapter) {
	r.adapters[a.Name()] = a
}

// Get returns the adapter with the given name, or an error if not found.
func (r *AdapterRegistry) Get(name string) (AIToolAdapter, error) {
	a, ok := r.adapters[name]
	if !ok {
		return nil, fmt.Errorf("unknown AI tool adapter: %q", name)
	}
	return a, nil
}

// List returns the names of all registered adapters, sorted alphabetically.
func (r *AdapterRegistry) List() []string {
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
