package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/service"
)

// Claude Code asks the person before it reads a file outside its working
// directory. An image the person pasted for the session is such a file, and
// asking about it is asking them to approve what they just did. The hook's
// answer allows that read and no other.
func TestClaudeReadPermissionAllowsOnlyTheTasksPastedImages(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	dir := service.PastedImageDir("task-1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(dir, "pasted-1.png")
	if err := os.WriteFile(image, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(temp, "secret.txt")
	if err := os.WriteFile(secret, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := func(tool, path string) string {
		encoded, _ := json.Marshal(map[string]any{
			"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": map[string]any{"file_path": path},
		})
		return string(encoded)
	}

	allowed := cli.ClaudeReadPermission(strings.NewReader(payload("Read", image)), "task-1")
	var answer struct {
		Output struct {
			Event    string `json:"hookEventName"`
			Decision string `json:"permissionDecision"`
			Reason   string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(allowed), &answer); err != nil {
		t.Fatalf("the answer is not JSON: %q (%v)", allowed, err)
	}
	if answer.Output.Event != "PreToolUse" || answer.Output.Decision != "allow" || answer.Output.Reason == "" {
		t.Errorf("answer=%+v", answer.Output)
	}

	// Everything else gets no answer at all, which leaves the decision to
	// Claude Code's own rules and the person.
	silent := map[string]struct{ taskID, payload string }{
		"a file elsewhere":             {"task-1", payload("Read", secret)},
		"a path that climbs out":       {"task-1", payload("Read", filepath.Join(dir, "..", "..", "..", "secret.txt"))},
		"another task's session":       {"task-2", payload("Read", image)},
		"no task":                      {"", payload("Read", image)},
		"a tool that changes the file": {"task-1", payload("Write", image)},
		"a shell command":              {"task-1", `{"tool_name":"Bash","tool_input":{"command":"cat ` + image + `"}}`},
		"no path":                      {"task-1", `{"tool_name":"Read","tool_input":{}}`},
		"not JSON":                     {"task-1", "pasted-images"},
		"nothing":                      {"task-1", ""},
	}
	for name, tc := range silent {
		t.Run(name, func(t *testing.T) {
			if got := cli.ClaudeReadPermission(strings.NewReader(tc.payload), tc.taskID); got != "" {
				t.Errorf("answered %q", got)
			}
		})
	}
}
