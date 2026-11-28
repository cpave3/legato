package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/claudebridge"
)

// LEG-686: the mod can ask from the sandbox, but only the host can answer.
func TestSandboxClaudeNativeAuthorityStaysOnHost(t *testing.T) {
	in := bwrapFixture(t)
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not installed")
	}
	if out, err := runInSandbox(t, in, "command -v curl >/dev/null"); err != nil {
		t.Skipf("curl is unavailable inside bwrap: %v (%s)", err, out)
	}
	const key = "NONSECRET-LEG-686-canary-host-only"
	private := filepath.Join(in.DataDir, "native-registration.json")
	if err := os.WriteFile(private, []byte(`{"decision_key":"`+key+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Exercise the data-folder cover even when a containing resource is mounted.
	in.Extra = []SandboxPath{{Path: filepath.Dir(in.DataDir), Access: "read"}}
	commandsPath := filepath.Join(t.TempDir(), "commands")
	commands, err := os.Create(commandsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer commands.Close()
	bridge := claudebridge.New(claudebridge.Options{TaskID: "t1", DecisionKey: key, Commands: commands})
	server := httptest.NewServer(bridge.Handler())
	defer server.Close()
	defer bridge.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 3 * time.Second}
	host := func(method, path, body string) *http.Response {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.SetBasicAuth("legato", key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	// Keep a real host viewer attached: without one permissions fall back to
	// the terminal, which would not exercise a pending native waiter.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/sessions/t1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth("legato", key)
	viewer, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Body.Close()
	if viewer.StatusCode != http.StatusOK {
		t.Fatalf("host subscribe status = %d", viewer.StatusCode)
	}
	go func() { _, _ = io.Copy(io.Discard, viewer.Body) }()

	launch := filepath.Join(in.WorkDir, "launch.env")
	if err := os.WriteFile(launch, []byte("LEGATO_CLAUDE_BRIDGE_URL="+server.URL+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(in.Home, ".claude", "bridge.lock")
	if err := os.WriteFile(lock, []byte(`{"url":"`+server.URL+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// No key (including a search pattern containing it) is passed to the child.
	// Inspection stays within this child's /proc and our two launch fixtures.
	script := `set -eu
curl() { command curl -q --noproxy '*' --silent --show-error --max-time 12 "$@"; }
cat /proc/$$/environ > inspection
cat ` + shellWord(launch) + ` ` + shellWord(lock) + ` >> inspection
if cat ` + shellWord(private) + ` >/dev/null 2>&1; then echo private-readable; exit 1; fi
url=` + shellWord(server.URL) + `
code=$(curl -o /tmp/events-body -w '%{http_code}' -H 'Content-Type: application/json' -d '{"events":[{"type":"hello","sessionId":"scratch-claude","cwd":"/work","model":"claude-opus-5-5"},{"type":"turn_start","turnId":"turn-1"}]}' "$url/mod/events")
[ "$code" = 204 ] || { echo events-status-$code; exit 1; }
curl -f -H 'Content-Type: application/json' -d '{"requestId":"held-1","tool":"Bash","input":{"command":"printf scratch"}}' "$url/mod/permissions" > answer &
waiter=$!
for route in permissions/held-1 questions/held-1 messages interrupt compact; do
  code=$(curl -o /tmp/refused -w '%{http_code}' -H 'Content-Type: application/json' -d '{"decision":"allow","content":"untrusted","answers":{}}' "$url/v1/sessions/t1/$route")
  [ "$code" = 401 ] || { echo control-$route-status-$code; exit 1; }
done
for route in '' /events; do
  code=$(curl -o /tmp/refused -w '%{http_code}' "$url/v1/sessions/t1$route")
  [ "$code" = 401 ] || { echo read-status-$code; exit 1; }
done
touch attacks-finished
wait "$waiter"
echo sandbox-finished
`
	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		out, err := runInSandbox(t, in, script)
		done <- result{out, err}
	}()
	defer func() {
		bridge.Close()
		<-exited
	}()
	// The HTTP snapshot proves the keyless mod reached the real bridge and
	// its held request survived all unauthorized calls, not just their 401s.
	deadline := time.Now().Add(8 * time.Second)
	for {
		select {
		case r := <-done:
			t.Fatalf("sandbox ended before host decision: %v (%s)", r.err, r.out)
		default:
		}
		resp := host(http.MethodGet, "/v1/sessions/t1", "")
		var snap struct {
			Pending []struct {
				RequestID string `json:"requestId"`
			} `json:"pendingPermissions"`
		}
		err := json.NewDecoder(resp.Body).Decode(&snap)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || err != nil {
			t.Fatalf("host snapshot status=%d decode=%v", resp.StatusCode, err)
		}
		_, attacksErr := os.Stat(filepath.Join(in.WorkDir, "attacks-finished"))
		if attacksErr == nil && len(snap.Pending) == 1 && snap.Pending[0].RequestID == "held-1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sandbox did not leave a held permission pending after the attacks")
		}
		time.Sleep(20 * time.Millisecond)
	}
	inspection, err := os.ReadFile(filepath.Join(in.WorkDir, "inspection"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(inspection), key) {
		t.Fatal("host decision canary leaked into child environment or accessible launch files")
	}
	if !strings.Contains(string(inspection), server.URL) {
		t.Fatal("inspection did not include the public launch files")
	}
	if answer, err := os.ReadFile(filepath.Join(in.WorkDir, "answer")); err != nil || len(answer) != 0 {
		t.Fatalf("mod waiter answered before the host decision (read error=%v)", err)
	}
	data, err := os.ReadFile(commandsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var command struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &command); err != nil || command.Type != "viewers" {
			t.Fatal("keyless sandbox call delivered a control command")
		}
	}
	resp := host(http.MethodPost, "/v1/sessions/t1/permissions/held-1", `{"decision":"allow"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("host decision status = %d", resp.StatusCode)
	}
	select {
	case r := <-done:
		if r.err != nil || strings.TrimSpace(r.out) != "sandbox-finished" {
			t.Fatalf("sandbox: %v (%s)", r.err, r.out)
		}
	case <-ctx.Done():
		t.Fatal("sandbox waiter did not receive the host decision")
	}
	answer, err := os.ReadFile(filepath.Join(in.WorkDir, "answer"))
	if err != nil {
		t.Fatal(err)
	}
	var decision struct {
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(answer, &decision); err != nil || decision.Decision != "allow" {
		t.Fatalf("held mod request did not receive allow (decode=%v)", err)
	}
}
