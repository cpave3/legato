package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// `legato agent bridge-hook claude-code` is what the legato-state plugin's
// PermissionRequest and StopFailure hooks run (LEG-684). It finds the
// session's bridge in LEGATO_CLAUDE_BRIDGE_URL, needs neither Legato's
// configuration nor its database, and never fails the hook.
func TestAgentBridgeHookAnswersFromTheSessionsBridge(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("LEGATO_CONFIG", filepath.Join(temp, "no-such-config.yaml"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(temp, "data"))
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/secret/mod/permissions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"decision": "allow"})
	}))
	t.Cleanup(bridge.Close)
	payload := `{"hook_event_name":"PermissionRequest","tool_name":"Bash","tool_input":{"command":"ls"}}`
	run := func(args ...string) (string, int) {
		var out string
		var code int
		withReviewStdin(t, payload, func() {
			out = captureStdout(t, func() int {
				code = runAgentContract(append([]string{"bridge-hook"}, args...))
				return code
			})
		})
		return out, code
	}

	t.Setenv("LEGATO_CLAUDE_BRIDGE_URL", bridge.URL+"/secret")
	if out, code := run("claude-code"); code != 0 || !strings.Contains(out, `"behavior":"allow"`) {
		t.Fatalf("with a bridge: exit %d, printed %q", code, out)
	}
	if out, code := run("codex"); code != 0 || out != "" {
		t.Fatalf("for another tool: exit %d, printed %q", code, out)
	}

	t.Setenv("LEGATO_CLAUDE_BRIDGE_URL", "")
	if out, code := run("claude-code"); code != 0 || out != "" {
		t.Fatalf("without a bridge: exit %d, printed %q", code, out)
	}
}
