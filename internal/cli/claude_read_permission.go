package cli

import (
	"encoding/json"
	"io"

	"github.com/cpave3/legato/internal/service"
)

// claudeReadAllowed is what a Claude Code PreToolUse hook prints to let a
// tool call run without a permission prompt.
const claudeReadAllowed = `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","permissionDecisionReason":"An image the person pasted for this session"}}`

// ClaudeReadPermission answers a Claude Code PreToolUse hook for the Read
// tool. Claude Code asks the person before it reads a file outside its
// working directory; an image the person pasted for the session is such a
// file, and asking about it is asking them to approve what they just did.
//
// It returns the hook output that allows the read when the file is one of
// the task's pasted images, and "" for everything else: no answer leaves the
// decision to Claude Code's own rules. A payload it cannot read is also "",
// so the hook never fails a tool call.
func ClaudeReadPermission(payload io.Reader, taskID string) string {
	var call struct {
		Tool  string `json:"tool_name"`
		Input struct {
			Path string `json:"file_path"`
		} `json:"tool_input"`
	}
	if err := json.NewDecoder(payload).Decode(&call); err != nil {
		return ""
	}
	if call.Tool != "Read" || !service.IsPastedImage(taskID, call.Input.Path) {
		return ""
	}
	return claudeReadAllowed
}
