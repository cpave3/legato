package prompt

import (
	"regexp"
	"strings"
)

// PromptType classifies the current state of a Claude Code terminal session.
type PromptType string

const (
	ToolApproval PromptType = "tool_approval"
	PlanApproval PromptType = "plan_approval"
	Question     PromptType = "question"
	FreeText     PromptType = "free_text"
	Working      PromptType = "working"
)

// Action represents a canned UI action mapped to a prompt type.
type Action struct {
	Label string `json:"label"`
	Keys  string `json:"keys"`
}

// PromptState is the result of classifying terminal output.
type PromptState struct {
	Type    PromptType `json:"type"`
	Context string     `json:"context,omitempty"`
	Actions []Action   `json:"actions,omitempty"`
}

// Detection is a prompt classification plus whether it is safe to treat as
// agent-blocking activity.
type Detection struct {
	State    PromptState
	Blocking bool
}

// ansiRe matches ANSI escape sequences.
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\][^\x07]*\x07|\x1b\[[\?]?[0-9;]*[a-zA-Z]`)

// Tool approval patterns.
var toolApprovalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)Do you want to`),
	regexp.MustCompile(`(?i)Allow\s+\w+`),
	regexp.MustCompile(`\[Y\/n\]`),
	regexp.MustCompile(`(?i)Yes\s*\/\s*Yes,?\s*and don'?t ask again\s*\/\s*No`),
	regexp.MustCompile(`(?i)Yes\s*\/\s*No`),
}

// Plan approval patterns.
var planApprovalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)Accept plan\??`),
	regexp.MustCompile(`(?i)Do you want to proceed with this plan`),
}

var codexQuestionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^Question\s+\d+/\d+\s+\(\d+\s+unanswered\)`),
	regexp.MustCompile(`(?i)tab to add notes\s+\|\s+enter to submit`),
}

// Free text input prompt patterns — the input cursor at end of output.
var freeTextPatterns = []*regexp.Regexp{
	regexp.MustCompile(`[❯>]\s*$`),
}

// stripANSI removes ANSI escape codes from terminal output.
func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// lastNLines returns the last n non-empty lines of s.
func lastNLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	// Trim trailing empty lines.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// numberedLineRe matches lines like "  1. Yes" or "❯ 1. Yes".
// Allows optional cursor prefix (❯ or >) that Claude Code puts on the selected option.
var numberedLineRe = regexp.MustCompile(`^\s*[❯>›]?\s*(\d+)\.\s+(.+)$`)

// extractNumberedActions finds the last contiguous block of numbered option
// lines anywhere in s. Non-numbered lines after the block (e.g. hint text)
// are skipped. Returns nil if no numbered lines found.
func extractNumberedActions(s string) []Action {
	lines := strings.Split(s, "\n")

	// Find all numbered lines with their indices.
	type match struct {
		index int
		label string
		key   string
	}
	var matches []match
	for i, line := range lines {
		m := numberedLineRe.FindStringSubmatch(line)
		if m != nil {
			matches = append(matches, match{index: i, label: strings.TrimSpace(m[2]), key: m[1] + " Enter"})
		}
	}

	if len(matches) == 0 {
		return nil
	}

	// Walk backwards through matches to find the last contiguous block.
	end := len(matches) - 1
	start := end
	for start > 0 && matches[start].index == matches[start-1].index+1 {
		start--
	}

	actions := make([]Action, 0, end-start+1)
	for i := start; i <= end; i++ {
		actions = append(actions, Action{
			Label: matches[i].label,
			Keys:  matches[i].key,
		})
	}
	return actions
}

// Detect classifies the terminal output and returns the prompt state.
// It is a pure function with no side effects, safe for concurrent use.
func Detect(output string) PromptState {
	cleaned := stripANSI(output)

	if state, ok := detectCodexQuestion(cleaned); ok {
		return state
	}

	// Check free text first — if the cursor prompt is at the end,
	// any approval prompts above it are stale/resolved.
	for _, re := range freeTextPatterns {
		if re.MatchString(cleaned) {
			return PromptState{
				Type: FreeText,
			}
		}
	}

	// Only scan the last few lines for approval prompts.
	// This avoids matching stale prompts still visible in the pane.
	tail := lastNLines(cleaned, 8)

	// Check plan approval (more specific, check first).
	for _, re := range planApprovalPatterns {
		if re.MatchString(tail) {
			return PromptState{
				Type:    PlanApproval,
				Actions: extractNumberedActions(tail),
			}
		}
	}

	// Check tool approval. Only show actions if numbered options are found —
	// never guess at options that might not be on screen.
	for _, re := range toolApprovalPatterns {
		if re.MatchString(tail) {
			return PromptState{
				Type:    ToolApproval,
				Actions: extractNumberedActions(tail),
			}
		}
	}

	// Default: show free text input.
	return PromptState{
		Type: FreeText,
	}
}

// DetectForAgent classifies output using adapter-specific strict prompt rules.
// Blocking is true only for prompts that indicate the agent is waiting for
// user input, not for generic idle/free-text prompts.
func DetectForAgent(agentKind, output string) Detection {
	cleaned := stripANSI(output)
	if agentKind == "codex" {
		if state, ok := detectCodexQuestion(cleaned); ok {
			return Detection{State: state, Blocking: true}
		}
	}

	tail := lastNLines(cleaned, 8)
	for _, re := range planApprovalPatterns {
		if re.MatchString(tail) {
			return Detection{State: PromptState{
				Type:    PlanApproval,
				Actions: extractNumberedActions(tail),
			}, Blocking: true}
		}
	}
	for _, re := range toolApprovalPatterns {
		if re.MatchString(tail) {
			return Detection{State: PromptState{
				Type:    ToolApproval,
				Actions: extractNumberedActions(tail),
			}, Blocking: true}
		}
	}

	return Detection{State: Detect(output)}
}

func detectCodexQuestion(cleaned string) (PromptState, bool) {
	tail := lastNLines(cleaned, 12)
	for _, re := range codexQuestionPatterns {
		if !re.MatchString(tail) {
			return PromptState{}, false
		}
	}
	return PromptState{
		Type:    Question,
		Actions: extractNumberedActions(tail),
	}, true
}
