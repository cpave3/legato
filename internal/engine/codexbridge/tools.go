package codexbridge

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The native panel has dedicated cards for a handful of tool names. Codex's
// own items are mapped onto them, so a shell command or a file change renders
// as richly as its Chimera or Claude Code counterpart.
const (
	nativeBash      = "bash"
	nativePatch     = "patch"
	nativeWebSearch = "web_search"
)

// outputLimit bounds the tool output kept and streamed per call. The model
// read the whole result; the panel only needs enough to inspect.
const outputLimit = 64 * 1024

// threadItem is one entry of a Codex turn, with the fields of every kind the
// bridge reads.
type threadItem struct {
	Type string `json:"type"`
	ID   string `json:"id"`

	// userMessage: what was sent, as inputs. reasoning: the model's own
	// reasoning, as strings, when the model lets it be read.
	Content json.RawMessage `json:"content"`

	// reasoning: what the model said of its reasoning, in parts
	Summary []string `json:"summary"`

	// agentMessage, plan
	Text string `json:"text"`

	// commandExecution
	Command          string  `json:"command"`
	Status           string  `json:"status"`
	AggregatedOutput *string `json:"aggregatedOutput"`
	ExitCode         *int    `json:"exitCode"`
	DurationMs       *int64  `json:"durationMs"`

	// fileChange
	Changes []fileChange `json:"changes"`

	// mcpToolCall, dynamicToolCall
	Server    string          `json:"server"`
	Tool      json.RawMessage `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Result    json.RawMessage `json:"result"`
	Error     json.RawMessage `json:"error"`

	// webSearch
	Query string `json:"query"`
}

type userInput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type fileChange struct {
	Path string `json:"path"`
	Kind struct {
		Type     string  `json:"type"`
		MovePath *string `json:"move_path"`
	} `json:"kind"`
	Diff string `json:"diff"`
}

// reasoningText is what can be read of a reasoning item: the model's summary
// of its reasoning, or the reasoning itself when there is no summary.
func (i threadItem) reasoningText() string {
	parts := i.Summary
	if strings.TrimSpace(strings.Join(parts, "")) == "" {
		parts = nil
		_ = json.Unmarshal(i.Content, &parts)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func (i threadItem) userText() string {
	var out strings.Builder
	var inputs []userInput
	_ = json.Unmarshal(i.Content, &inputs)
	for _, input := range inputs {
		if input.Type == "text" {
			out.WriteString(input.Text)
		}
	}
	return out.String()
}

func (i threadItem) toolName() string {
	var name string
	_ = json.Unmarshal(i.Tool, &name)
	return name
}

// isSettled reports whether the item's work is over, whatever its outcome.
func (i threadItem) isSettled() bool {
	return i.Status != "" && i.Status != "inProgress"
}

// nativeTool converts a Codex item into the name, arguments and summary the
// native panel renders. It reports false for an item that is not a tool call.
func nativeTool(item threadItem) (name string, args map[string]any, summary string, ok bool) {
	switch item.Type {
	case "commandExecution":
		command := shellCommand(item.Command)
		return nativeBash, map[string]any{"command": command}, firstLine(command), true
	case "fileChange":
		changes := make([]map[string]any, 0, len(item.Changes))
		names := make([]string, 0, len(item.Changes))
		for _, change := range item.Changes {
			entry := map[string]any{"path": change.Path, "kind": change.Kind.Type, "diff": truncate(change.Diff, outputLimit)}
			if change.Kind.MovePath != nil && *change.Kind.MovePath != "" {
				entry["movePath"] = *change.Kind.MovePath
			}
			changes = append(changes, entry)
			names = append(names, filepath.Base(change.Path))
		}
		return nativePatch, map[string]any{"changes": changes}, summarizeNames(names), true
	case "mcpToolCall", "dynamicToolCall":
		tool := item.toolName()
		if tool == "" {
			return "", nil, "", false
		}
		if item.Server != "" {
			tool = item.Server + "." + tool
		}
		args := map[string]any{}
		if json.Unmarshal(item.Arguments, &args) != nil || args == nil {
			args = map[string]any{}
			if len(item.Arguments) > 0 && string(item.Arguments) != "null" {
				args["arguments"] = json.RawMessage(item.Arguments)
			}
		}
		return tool, args, toolSummary(args), true
	case "webSearch":
		return nativeWebSearch, map[string]any{"query": item.Query}, truncate(firstLine(item.Query), 160), true
	}
	return "", nil, "", false
}

// nativeResult is the record the panel renders for a settled tool call.
func nativeResult(item threadItem) map[string]any {
	switch item.Type {
	case "commandExecution":
		output := ""
		if item.AggregatedOutput != nil {
			output = truncate(*item.AggregatedOutput, outputLimit)
		}
		if item.Status == "declined" {
			return map[string]any{"stderr": "Declined: the command was not run.", "exit_code": 1}
		}
		code := 0
		if item.ExitCode != nil {
			code = *item.ExitCode
		} else if item.Status != "completed" {
			code = 1
		}
		if code != 0 {
			return map[string]any{"stderr": output, "exit_code": code}
		}
		return map[string]any{"stdout": output, "exit_code": 0}
	case "fileChange":
		if item.Status == "completed" {
			return map[string]any{"ok": true}
		}
		return map[string]any{"error": "The change was " + item.Status + "."}
	case "mcpToolCall", "dynamicToolCall":
		if len(item.Error) > 0 && string(item.Error) != "null" {
			var failure struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(item.Error, &failure) == nil && failure.Message != "" {
				return map[string]any{"error": truncate(failure.Message, outputLimit)}
			}
			return map[string]any{"error": truncate(string(item.Error), outputLimit)}
		}
		return map[string]any{"output": truncate(resultText(item.Result), outputLimit)}
	}
	return map[string]any{"ok": true}
}

// resultText reads the text of an MCP tool result, falling back to its JSON.
func resultText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &result) == nil && len(result.Content) > 0 {
		var out strings.Builder
		for _, part := range result.Content {
			if part.Type == "text" {
				out.WriteString(part.Text)
			}
		}
		if out.Len() > 0 {
			return out.String()
		}
	}
	return string(raw)
}

var shellWrapper = regexp.MustCompile(`^(?:\S*/)?(?:ba|z|da|fi)?sh -l?c (.+)$`)

// shellCommand strips the login-shell wrapper Codex runs every command in
// (`/usr/bin/zsh -lc 'git status'`), leaving what the model asked to run.
func shellCommand(command string) string {
	match := shellWrapper.FindStringSubmatch(strings.TrimSpace(command))
	if match == nil {
		return command
	}
	inner := match[1]
	switch {
	case len(inner) >= 2 && strings.HasPrefix(inner, "'") && strings.HasSuffix(inner, "'"):
		return strings.ReplaceAll(inner[1:len(inner)-1], `'\''`, `'`)
	case len(inner) >= 2 && strings.HasPrefix(inner, `"`) && strings.HasSuffix(inner, `"`):
		if unquoted, err := strconv.Unquote(inner); err == nil {
			return unquoted
		}
	}
	return inner
}

// summaryKeys are the arguments that best say what a call is about, in the
// order tried.
var summaryKeys = []string{"description", "pattern", "query", "url", "file_path", "path", "command", "prompt", "subject", "name"}

func toolSummary(args map[string]any) string {
	for _, key := range summaryKeys {
		if value, _ := args[key].(string); value != "" {
			return truncate(firstLine(value), 160)
		}
	}
	return ""
}

func summarizeNames(names []string) string {
	const shown = 3
	if len(names) <= shown {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:shown], ", ") + " +" + strconv.Itoa(len(names)-shown) + " more"
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
	return text[:limit] + "\n… (truncated)"
}
