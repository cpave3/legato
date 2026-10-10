package claudebridge

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// The native panel has dedicated cards for a handful of tool names. Claude
// Code's own tools are mapped onto them so a shell command, an edit or a
// subagent renders as richly as its Chimera counterpart; every other tool
// keeps its name and gets a one-line summary.
const (
	nativeBash       = "bash"
	nativeEdit       = "edit"
	nativeWrite      = "write"
	nativeRead       = "read"
	nativeSpawnAgent = "spawn_agent"
)

// resultTextLimit bounds the tool output kept and streamed per call. The
// model read the whole result; the panel only needs enough to inspect.
const resultTextLimit = 64 * 1024

// nativeToolCall converts a Claude Code tool call into the name, arguments and
// summary the native panel renders.
func nativeToolCall(tool string, input map[string]any) (name string, args map[string]any, summary string) {
	switch tool {
	case "Bash":
		return nativeBash, input, firstLine(stringArg(input, "command"))
	case "Edit":
		return nativeEdit, renamed(input, "file_path", "path"), stringArg(input, "file_path")
	case "Write":
		return nativeWrite, renamed(input, "file_path", "path"), stringArg(input, "file_path")
	case "Read":
		return nativeRead, renamed(input, "file_path", "path"), stringArg(input, "file_path")
	case "Agent", "Task":
		args := map[string]any{
			"purpose": stringArg(input, "description"),
			"prompt":  stringArg(input, "prompt"),
		}
		if agent := stringArg(input, "subagent_type"); agent != "" {
			args["agent"] = agent
		}
		if model := stringArg(input, "model"); model != "" {
			args["model"] = model
		}
		return nativeSpawnAgent, args, stringArg(input, "description")
	}
	if input == nil {
		input = map[string]any{}
	}
	return tool, input, toolSummary(tool, input)
}

// summaryKeys are the arguments that best say what a call is about, in the
// order tried.
var summaryKeys = []string{"description", "pattern", "query", "url", "file_path", "path", "command", "skill", "prompt", "subject", "name"}

func toolSummary(tool string, input map[string]any) string {
	switch tool {
	case askUserQuestionTool:
		// The first question, and how many more came with it.
		if asked := questionsOf(tool, input); len(asked) > 0 {
			summary := truncate(firstLine(asked[0].Question), 160)
			if len(asked) > 1 {
				summary += " (+" + strconv.Itoa(len(asked)-1) + " more)"
			}
			return summary
		}
	case exitPlanModeTool:
		// A plan opens with its title.
		if title := strings.TrimSpace(strings.TrimLeft(firstLine(strings.TrimSpace(stringArg(input, "plan"))), "#")); title != "" {
			return truncate(title, 160)
		}
	}
	for _, key := range summaryKeys {
		if value := stringArg(input, key); value != "" {
			return truncate(firstLine(value), 160)
		}
	}
	return ""
}

var exitCodePattern = regexp.MustCompile(`(?m)^Exit code (\d+)`)

// nativeToolResult converts the text the model read as a tool's result into
// the record the native panel renders for that tool.
func nativeToolResult(name, text string, isError bool) map[string]any {
	text = truncate(text, resultTextLimit)
	switch name {
	case nativeBash:
		if !isError {
			return map[string]any{"stdout": text, "exit_code": 0}
		}
		exitCode := 1
		if match := exitCodePattern.FindStringSubmatch(text); match != nil {
			if parsed, err := strconv.Atoi(match[1]); err == nil {
				exitCode = parsed
			}
		}
		return map[string]any{"stderr": text, "exit_code": exitCode}
	case nativeSpawnAgent:
		if isError {
			return map[string]any{"reason": "error", "error": text}
		}
		return map[string]any{"reason": "stop", "output": text}
	case nativeEdit, nativeWrite, nativeRead:
		// The panel draws these from their arguments; the file's content is
		// not repeated in the result.
		if isError {
			return map[string]any{"error": text}
		}
		return map[string]any{"ok": true}
	}
	if isError {
		return map[string]any{"error": text}
	}
	return map[string]any{"output": text}
}

// permissionCommand renders a tool call as the text a person approves.
func permissionCommand(tool string, input map[string]any) string {
	switch tool {
	case "Bash":
		if command := stringArg(input, "command"); command != "" {
			return command
		}
	case "Edit":
		return "Edit " + stringArg(input, "file_path") + "\n" +
			prefixLines("- ", stringArg(input, "old_string")) + "\n" +
			prefixLines("+ ", stringArg(input, "new_string"))
	case "Write":
		return "Write " + stringArg(input, "file_path") + "\n" + truncate(stringArg(input, "content"), 4000)
	case exitPlanModeTool:
		// A client that does not draw plans still shows this one.
		if plan := stringArg(input, "plan"); plan != "" {
			return truncate(plan, 4000)
		}
	}
	encoded, err := json.MarshalIndent(input, "", "  ")
	if err != nil || len(input) == 0 {
		return tool
	}
	return tool + " " + truncate(string(encoded), 4000)
}

func prefixLines(prefix, text string) string {
	lines := strings.Split(truncate(text, 2000), "\n")
	for index, line := range lines {
		lines[index] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func renamed(input map[string]any, from, to string) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		if key == from {
			key = to
		}
		out[key] = value
	}
	return out
}

func stringArg(input map[string]any, key string) string {
	value, _ := input[key].(string)
	return value
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !isRuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "\n… (truncated)"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
