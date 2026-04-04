## Context

`prompt.Detect()` in `internal/engine/prompt/detect.go` classifies terminal output and returns hardcoded action lists. Tool approval always returns `[Yes (Enter), Always (Down Enter), No (Down Down Enter)]` using arrow-key navigation. This breaks if the user manually moves the cursor before clicking a web UI button.

Claude Code presents numbered options like:
```
1. Yes
2. Yes, and don't ask again
3. No
```

Pressing the digit key directly selects the option regardless of cursor position.

## Goals / Non-Goals

**Goals:**
- Parse numbered option lines from terminal output to build actions dynamically
- Use digit keys (`"1"`, `"2"`, `"3"`) instead of arrow sequences for deterministic selection
- Maintain the same `PromptState` struct and `Action` type — no API changes
- Continue detecting prompt type (tool_approval vs plan_approval vs free_text)

**Non-Goals:**
- Changing the WebSocket protocol or frontend components (they already handle arbitrary key strings)
- Supporting non-numbered prompt formats (e.g. `[Y/n]` inline prompts) with number keys — these aren't numbered lists
- Changing plan approval detection (Accept/Reject doesn't use numbered options)

## Decisions

### 1. Extract numbered options from tail lines

After detecting the prompt type, scan the tail lines for a contiguous block of lines matching `^\s*(\d+)\.\s+(.+)$` near the end of output. Each match produces an `Action{Label: <text>, Keys: <digit>}`.

**Why**: This is the simplest approach — one regex, applied to lines we already extract. No new dependencies or parsing stages.

**Alternative considered**: Hardcode updated keys (just change `"Down Enter"` to `"2"`). Rejected because the option labels and count can vary across Claude Code versions and prompt types. Dynamic parsing is more robust.

### 2. Fall back to hardcoded actions when no numbered lines found

If the prompt type is detected (tool_approval/plan_approval) but no numbered option lines are found, fall back to the current hardcoded actions. This handles edge cases where Claude Code might present a prompt without numbered options.

**Why**: Graceful degradation. The numbered format is the common case but we shouldn't break if Claude Code changes its UI.

### 3. Keep prompt type detection separate from action extraction

Detect prompt type first (using existing regex patterns), then extract numbered actions as a second pass. The type detection gates whether we look for numbered options at all.

**Why**: Separation of concerns. Type detection tells us *what kind* of prompt it is (which affects UI behavior like dismiss tracking). Action extraction tells us *what options* are available. These are orthogonal.

### 4. Number regex anchored to line start with optional whitespace

Pattern: `^\s*(\d+)\.\s+(.+)$` applied per-line. Captures the digit and the label text. Lines must be contiguous from the bottom of the non-empty output (stop at the first non-matching line scanning upward).

**Why**: Anchoring to line start avoids false positives from numbered lists in task descriptions or conversation text. Requiring contiguity from the bottom ensures we only match the active prompt's options.

## Risks / Trade-offs

**[Risk]** Claude Code changes the numbered format (e.g. adds parentheses: `1)` instead of `1.`)
  **Mitigation**: The regex is simple to update. Fallback to hardcoded actions prevents total failure.

**[Risk]** Non-prompt numbered content appears in the last 8 lines (e.g. a numbered list in AI output)
  **Mitigation**: We only extract numbered lines when a prompt type pattern has already been detected. The combination of prompt detection + numbered lines makes false positives unlikely.

**[Risk]** Plan approval prompts might gain numbered options in future
  **Mitigation**: The extraction logic applies to all prompt types generically. If plan approval starts using numbers, it will automatically work.
