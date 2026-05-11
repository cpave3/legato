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

// RolePromptingAdapter is an optional capability adapters can implement to
// supply role-specific system prompts for swarm agents. The agent service
// performs an interface assertion at spawn time — adapters that don't implement
// this interface skip prompt injection.
type RolePromptingAdapter interface {
	// RoleSystemPrompt returns a system prompt for the given swarm role label
	// (e.g. "conductor", "worker", or any project-defined free-form name).
	// Unknown roles SHOULD return an empty string.
	RoleSystemPrompt(role string) string
}

// LaunchCommandAdapter is an optional capability adapters can implement to
// supply the shell command that starts the AI tool inside a freshly-spawned
// tmux session. When implemented, the agent service auto-launches the tool by
// running the command via `tmux send-keys` after session creation. Adapters
// that do not implement this interface leave the session at a shell prompt.
type LaunchCommandAdapter interface {
	// LaunchCommand returns the single-line shell command that, when sent to
	// the agent's tmux session, starts the AI tool with the role's system
	// prompt applied. The brief argument carries the per-worker initial
	// brief; adapters MAY embed it in the command line or MAY return an empty
	// string here and rely on the agent service delivering the brief as a
	// separate send-keys call after the tool has booted.
	//
	// The tier argument names a launch profile from the adapter's per-tier
	// configuration (typically a model selector like `--model claude-haiku-4-5`).
	// Empty tier means "use the adapter's base launch_args only".
	//
	// Adapters that don't want to auto-launch SHOULD return an empty string.
	LaunchCommand(env map[string]string, brief, tier string) string
}

// LaunchSelfKickoff is an optional adapter capability for tools whose launch
// command itself constitutes the first user turn — e.g. Chimera's `--prompt`
// flag treats its value as the initial user message rather than as system
// context. For such adapters, the agent service skips the post-launch
// "read your brief" send-keys to avoid sending a redundant second user turn.
//
// Adapters that don't implement this interface (or that return false) get
// the default behavior: launch + post-launch kickoff send-keys.
type LaunchSelfKickoff interface {
	// LaunchIsSelfKickoff reports whether the adapter's launch command
	// already serves as the first user turn for the agent.
	LaunchIsSelfKickoff() bool
}

// RolePromptPreambleAdapter is an optional adapter capability for tools that
// need a tool-specific note prepended to every role prompt. Use this for
// quirks that the agent must know about regardless of which role it's
// playing — for example, Chimera workers running in sandbox mode need to
// invoke legato CLI / read legato env vars in host mode rather than sandbox
// mode, otherwise the calls fail silently.
//
// The preamble is written to the role prompt file ahead of the role-specific
// content, so the agent sees it as part of its standing instructions.
type RolePromptPreambleAdapter interface {
	// RolePromptPreamble returns adapter-specific guidance prepended to the
	// role prompt file. Return empty string for no preamble.
	RolePromptPreamble() string
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
