package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// `legato agent allow-read` is what the Claude Code plugin's hook runs
// before a Read. It finds its session from the pane's environment, like the
// other hook commands, and never fails the tool call it is asked about.
func TestAgentAllowReadAnswersTheHookForAPastedImage(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	t.Setenv("LEGATO_TASK_ID", "task-1")
	// It must not need Legato's configuration or database to answer.
	t.Setenv("LEGATO_CONFIG", filepath.Join(temp, "no-such-config.yaml"))
	dir := service.PastedImageDir("task-1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(dir, "pasted-1.png")
	if err := os.WriteFile(image, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	read := func(path string) string {
		payload, _ := json.Marshal(map[string]any{"tool_name": "Read", "tool_input": map[string]any{"file_path": path}})
		var out string
		withReviewStdin(t, string(payload), func() {
			out = captureStdout(t, func() int { return runAgentContract([]string{"allow-read"}) })
		})
		return out
	}

	if out := read(image); !strings.Contains(out, `"permissionDecision":"allow"`) {
		t.Errorf("a pasted image was not allowed: %q", out)
	}
	if out := read(filepath.Join(temp, "elsewhere.png")); out != "" {
		t.Errorf("another file got an answer: %q", out)
	}

	// Outside a Legato session there is nothing to allow, and no error.
	t.Setenv("LEGATO_TASK_ID", "")
	if out := read(image); out != "" {
		t.Errorf("answered without a session: %q", out)
	}
}
