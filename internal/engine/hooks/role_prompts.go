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
// With reviews off, the parts of the prompt about Legato's review packet are
// left out (see withReviews).
func resolveRolePrompt(overrides RolePromptOverrides, role string, reviews bool) string {
	if overrides != nil {
		if p, ok := overrides[role]; ok {
			return withReviews(p, reviews)
		}
	}
	return withReviews(builtinRolePrompt(role), reviews)
}

// Lines of a prompt between these markers are for one setting of reviews:
// "<!-- reviews -->" to "<!-- /reviews -->" when they are on, and
// "<!-- no-reviews -->" to "<!-- /no-reviews -->" when they are off. Each
// marker is a line of its own.
const (
	reviewsStart   = "<!-- reviews -->"
	reviewsEnd     = "<!-- /reviews -->"
	noReviewsStart = "<!-- no-reviews -->"
	noReviewsEnd   = "<!-- /no-reviews -->"
)

// withReviews keeps the lines of prompt for this setting of reviews and
// drops the markers.
func withReviews(prompt string, reviews bool) string {
	if !strings.Contains(prompt, "<!--") {
		return prompt
	}
	lines := strings.SplitAfter(prompt, "\n")
	kept := make([]string, 0, len(lines))
	keep := true
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case reviewsStart:
			keep = reviews
		case noReviewsStart:
			keep = !reviews
		case reviewsEnd, noReviewsEnd:
			keep = true
		default:
			if keep {
				kept = append(kept, line)
			}
		}
	}
	return strings.Join(kept, "")
}
