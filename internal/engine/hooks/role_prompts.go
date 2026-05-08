package hooks

import (
	"embed"
	"strings"
)

// shellQuote returns a single-quoted version of arg suitable for inclusion in
// a shell command, with embedded single quotes escaped.
func shellQuote(arg string) string {
	if arg == "" {
		return "''"
	}
	if !strings.ContainsAny(arg, " '\"\\$`*?(){}[]<>|&;\n\t") {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

//go:embed prompts/*.md
var rolePromptsFS embed.FS

// RolePromptOverrides maps adapter-specific overrides keyed by role.
// When set on an adapter, RoleSystemPrompt checks this map first before
// falling back to the embedded built-in prompt.
type RolePromptOverrides map[string]string

// builtinRolePrompt returns the embedded role prompt for known role labels.
//   - "conductor" → the swarm conductor's operational manual.
//   - any other label (workers' free-form roles like "backend", "frontend",
//     etc.) → the generic worker brief, which the conductor's per-worker
//     prompt is layered on top of via the launch flow.
//
// Returns empty string only when the embedded files cannot be read.
func builtinRolePrompt(role string) string {
	switch role {
	case "conductor":
		data, err := rolePromptsFS.ReadFile("prompts/conductor.md")
		if err != nil {
			return ""
		}
		return string(data)
	case "":
		return ""
	default:
		// All non-conductor roles fall back to the worker brief. The conductor
		// supplies the role-specific guidance per worker via the plan's
		// `prompt` field, delivered separately as the initial brief.
		data, err := rolePromptsFS.ReadFile("prompts/worker.md")
		if err != nil {
			return ""
		}
		return string(data)
	}
}

// resolveRolePrompt checks override map then falls back to built-in.
// Returns "" if neither is configured.
func resolveRolePrompt(overrides RolePromptOverrides, role string) string {
	if overrides != nil {
		if p, ok := overrides[role]; ok {
			return p
		}
	}
	return builtinRolePrompt(role)
}
