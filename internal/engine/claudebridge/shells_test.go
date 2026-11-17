package claudebridge

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// shellHarness is a harness whose bridge polls background shells' output
// quickly, so a test sees it within its event timeout.
func shellHarness(t *testing.T) *harness {
	t.Helper()
	commands := &commandLog{lines: make(chan map[string]any, 64)}
	bridge := New(Options{
		TaskID:    "task-1",
		Commands:  commands,
		Now:       func() time.Time { return time.UnixMilli(1_700_000_000_000) },
		ShellPoll: 10 * time.Millisecond,
	})
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})
	return &harness{t: t, bridge: bridge, server: server, commands: commands}
}

// backgroundResult is Claude Code's result for a Bash call it ran in the
// background, as of 2.1.
func backgroundResult(id, path string) string {
	return "Command running in background with ID: " + id + ". Output is being written to: " + path + ". You will be notified when it completes — do not poll that file or sleep while waiting."
}

// taskNotification is the prompt Claude Code wakes a session with when a
// background task ends.
func taskNotification(id, path, status, summary string) string {
	return "<task-notification>\n<task-id>" + id + "</task-id>\n<tool-use-id>toolu_x</tool-use-id>\n<output-file>" + path +
		"</output-file>\n<status>" + status + "</status>\n<summary>" + summary + "</summary>\n</task-notification>"
}

// shellOutputFile makes a task output file as Claude Code lays them out.
func shellOutputFile(t *testing.T, id string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "tasks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".output")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func appendFile(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

// nextOfType skips events until one of the type comes.
func nextOfType(t *testing.T, events <-chan map[string]any, kind string) map[string]any {
	t.Helper()
	for {
		event := nextEvent(t, events)
		if event["type"] == kind {
			return event
		}
	}
}

type shellSnapshot struct {
	BackgroundShells []struct {
		ShellID     string `json:"shellId"`
		CallID      string `json:"callId"`
		Command     string `json:"command"`
		Description string `json:"description"`
		Status      string `json:"status"`
		ExitCode    *int   `json:"exitCode"`
		StartedAt   int64  `json:"startedAt"`
		EndedAt     int64  `json:"endedAt"`
		Output      *struct {
			Available bool   `json:"available"`
			Text      string `json:"text"`
			Truncated bool   `json:"truncated"`
		} `json:"output"`
	} `json:"backgroundShells"`
}

func (h *harness) shells() shellSnapshot {
	h.t.Helper()
	var snap shellSnapshot
	if code := h.getJSON("/v1/sessions/task-1", &snap); code != http.StatusOK {
		h.t.Fatalf("GET snapshot = %d", code)
	}
	return snap
}

func startBackgroundShell(h *harness, callID, shellID, path string) {
	h.t.Helper()
	h.mod(assistantRow("row-"+callID, toolUse(callID, "Bash", map[string]any{
		"command": "sleep 2; echo hi; sleep 30", "description": "Say hi later", "run_in_background": true,
	})))
	h.mod(toolResultRow("res-"+callID, callID, backgroundResult(shellID, path), false))
}

func TestABackgroundShellIsRecordedWhenItsCallReturns(t *testing.T) {
	h := shellHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")
	path := shellOutputFile(t, "b1")

	startBackgroundShell(h, "toolu_1", "b1", path)

	started := nextOfType(t, events, "background_shell")
	shell, _ := started["shell"].(map[string]any)
	if shell["shellId"] != "b1" || shell["callId"] != "toolu_1" || shell["status"] != "running" ||
		shell["command"] != "sleep 2; echo hi; sleep 30" || shell["description"] != "Say hi later" {
		t.Fatalf("background_shell = %v", started)
	}
	snap := h.shells()
	if len(snap.BackgroundShells) != 1 || snap.BackgroundShells[0].ShellID != "b1" || snap.BackgroundShells[0].Status != "running" ||
		snap.BackgroundShells[0].StartedAt == 0 || snap.BackgroundShells[0].Output == nil || !snap.BackgroundShells[0].Output.Available {
		t.Fatalf("snapshot shells = %+v", snap.BackgroundShells)
	}
}

func TestAForegroundShellIsNoBackgroundShell(t *testing.T) {
	h := shellHarness(t)
	h.mod(hello(nil), assistantRow("row-a", toolUse("toolu_1", "Bash", map[string]any{"command": "echo hi"})))
	h.mod(toolResultRow("row-r", "toolu_1", "hi", false))
	if shells := h.shells().BackgroundShells; len(shells) != 0 {
		t.Fatalf("shells = %+v", shells)
	}
}

func TestAShellMovedToTheBackgroundIsRecordedToo(t *testing.T) {
	h := shellHarness(t)
	path := shellOutputFile(t, "b2")
	h.mod(hello(nil), assistantRow("row-a", toolUse("toolu_1", "Bash", map[string]any{"command": "go test ./..."})))
	h.mod(toolResultRow("row-r", "toolu_1", "Command did not complete within its 120s timeout and was moved to the background (ID: b2). Output is being written to: "+path+". You will be notified when it completes.", false))
	shells := h.shells().BackgroundShells
	if len(shells) != 1 || shells[0].ShellID != "b2" || shells[0].Command != "go test ./..." || shells[0].Status != "running" {
		t.Fatalf("shells = %+v", shells)
	}
}

func TestABackgroundShellsOutputIsFollowed(t *testing.T) {
	h := shellHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")
	path := shellOutputFile(t, "b1")
	startBackgroundShell(h, "toolu_1", "b1", path)

	appendFile(t, path, "hi\n")
	first := nextOfType(t, events, "background_shell_output")
	if first["shellId"] != "b1" || first["chunk"] != "hi\n" {
		t.Fatalf("first output = %v", first)
	}
	appendFile(t, path, "there\n")
	second := nextOfType(t, events, "background_shell_output")
	if second["chunk"] != "there\n" {
		t.Fatalf("second output = %v", second)
	}
	if out := h.shells().BackgroundShells[0].Output; out == nil || out.Text != "hi\nthere\n" || out.Truncated {
		t.Fatalf("snapshot output = %+v", out)
	}
}

func TestABackgroundShellsExitIsRecordedFromItsNotification(t *testing.T) {
	h := shellHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")
	path := shellOutputFile(t, "b1")
	startBackgroundShell(h, "toolu_1", "b1", path)
	nextOfType(t, events, "background_shell")
	appendFile(t, path, "hi\nbye\n")

	h.mod(promptRow("note-1", "task-notification", taskNotification("b1", path, "failed", `Background command "Say hi later" failed with exit code 3`), nil))

	ended := nextOfType(t, events, "background_shell")
	shell, _ := ended["shell"].(map[string]any)
	if shell["status"] != "exited" || shell["exitCode"] != float64(3) || shell["endedAt"] == nil {
		t.Fatalf("ended = %v", ended)
	}
	// Its last output is read once more after it ended.
	deadline := time.Now().Add(2 * time.Second)
	for {
		shells := h.shells().BackgroundShells
		if shells[0].Output != nil && shells[0].Output.Text == "hi\nbye\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("output = %+v", shells[0].Output)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestACompletedShellExitedWithZero(t *testing.T) {
	h := shellHarness(t)
	path := shellOutputFile(t, "b1")
	h.mod(hello(nil))
	startBackgroundShell(h, "toolu_1", "b1", path)
	h.mod(promptRow("note-1", "task-notification", taskNotification("b1", path, "completed", `Background command "Say hi later" completed (exit code 0)`), nil))
	shell := h.shells().BackgroundShells[0]
	if shell.Status != "exited" || shell.ExitCode == nil || *shell.ExitCode != 0 {
		t.Fatalf("shell = %+v", shell)
	}
}

func TestAStoppedShellIsKilled(t *testing.T) {
	h := shellHarness(t)
	path := shellOutputFile(t, "b1")
	h.mod(hello(nil))
	startBackgroundShell(h, "toolu_1", "b1", path)
	h.mod(assistantRow("row-stop", toolUse("toolu_2", "TaskStop", map[string]any{"task_id": "b1"})))
	h.mod(toolResultRow("res-stop", "toolu_2", `{"message":"Successfully stopped task: b1"}`, false))
	shell := h.shells().BackgroundShells[0]
	if shell.Status != "killed" || shell.ExitCode != nil || shell.EndedAt == 0 {
		t.Fatalf("shell = %+v", shell)
	}
	// Its notification, which follows, keeps it killed.
	h.mod(promptRow("note-1", "task-notification", taskNotification("b1", path, "killed", `Background command "Say hi later" was stopped`), nil))
	if shell := h.shells().BackgroundShells[0]; shell.Status != "killed" {
		t.Fatalf("after its notification = %+v", shell)
	}
}

func TestTwoBackgroundShellsAreTwoRecords(t *testing.T) {
	h := shellHarness(t)
	h.mod(hello(nil))
	startBackgroundShell(h, "toolu_1", "b1", shellOutputFile(t, "b1"))
	startBackgroundShell(h, "toolu_2", "b2", shellOutputFile(t, "b2"))
	h.mod(promptRow("note-1", "task-notification",
		taskNotification("b1", "x", "completed", "done")+"\n"+taskNotification("b2", "y", "failed", "failed with exit code 1"), nil))
	shells := h.shells().BackgroundShells
	if len(shells) != 2 || shells[0].ShellID != "b1" || shells[1].ShellID != "b2" || *shells[0].ExitCode != 0 || *shells[1].ExitCode != 1 {
		t.Fatalf("shells = %+v", shells)
	}
}

func TestOnlyTheNewestOutputIsKept(t *testing.T) {
	h := shellHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")
	path := shellOutputFile(t, "b1")
	startBackgroundShell(h, "toolu_1", "b1", path)
	nextOfType(t, events, "background_shell")

	big := strings.Repeat("x", shellChunkLimit+100) + "\nlast line\n"
	appendFile(t, path, big)
	chunk := nextOfType(t, events, "background_shell_output")
	if chunk["gap"] != true || len(chunk["chunk"].(string)) > shellChunkLimit || !strings.HasSuffix(chunk["chunk"].(string), "last line\n") {
		t.Fatalf("chunk gap=%v len=%d", chunk["gap"], len(chunk["chunk"].(string)))
	}
	appendFile(t, path, strings.Repeat("y\n", shellTailLimit))
	deadline := time.Now().Add(2 * time.Second)
	for {
		out := h.shells().BackgroundShells[0].Output
		if out != nil && out.Truncated && len(out.Text) <= shellTailLimit && strings.HasSuffix(out.Text, "y\n") && !strings.Contains(out.Text, "last line") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("output len=%d truncated=%v", len(out.Text), out.Truncated)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAFileThatIsNotTheShellsOwnIsNotRead(t *testing.T) {
	h := shellHarness(t)
	h.mod(hello(nil))
	other := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(other, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	startBackgroundShell(h, "toolu_1", "b1", other)
	time.Sleep(50 * time.Millisecond)
	shell := h.shells().BackgroundShells[0]
	if shell.Status != "running" || shell.Output == nil || shell.Output.Available || shell.Output.Text != "" {
		t.Fatalf("shell = %+v", shell)
	}
}

func TestAResumedSessionsBackgroundShellsAreRecordedWithTheirEnds(t *testing.T) {
	h := shellHarness(t)
	running := shellOutputFile(t, "b2")
	appendFile(t, running, "serving on :5173\n")
	result1 := backgroundResult("b1", "/gone/tasks/b1.output")
	result2 := backgroundResult("b2", running)
	h.mod(hello(map[string]any{"messages": []map[string]any{
		{"role": "assistant", "text": "", "toolUses": []map[string]any{
			{"tool_use_id": "toolu_1", "tool": "Bash", "input": map[string]any{"command": "make", "run_in_background": true}, "text": result1},
			{"tool_use_id": "toolu_2", "tool": "Bash", "input": map[string]any{"command": "npm run dev", "run_in_background": true}, "text": result2},
		}},
		{"role": "user", "text": taskNotification("b1", "/gone/tasks/b1.output", "completed", "completed (exit code 0)")},
	}}))
	shells := h.shells().BackgroundShells
	if len(shells) != 2 || shells[0].Status != "exited" || shells[1].Status != "running" || shells[1].Command != "npm run dev" {
		t.Fatalf("shells = %+v", shells)
	}
	deadline := time.Now().Add(2 * time.Second)
	for h.shells().BackgroundShells[1].Output.Text != "serving on :5173\n" {
		if time.Now().After(deadline) {
			t.Fatalf("output = %+v", h.shells().BackgroundShells[1].Output)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Claude Code ends a shell's file with its exit code, which comes before
// its notification when the session is busy with a long turn.
func TestAShellsExitIsReadFromTheEndOfItsFile(t *testing.T) {
	h := shellHarness(t)
	h.mod(hello(nil))
	events := h.subscribe("")
	path := shellOutputFile(t, "b1")
	startBackgroundShell(h, "toolu_1", "b1", path)
	nextOfType(t, events, "background_shell")

	appendFile(t, path, "tick 1\n[exited with code 7]\nnot the end\n")
	nextOfType(t, events, "background_shell_output")
	if shell := h.shells().BackgroundShells[0]; shell.Status != "running" {
		t.Fatalf("a line in the middle ended it: %+v", shell)
	}

	appendFile(t, path, "\n[exited with code 4]\n")
	ended := nextOfType(t, events, "background_shell")
	if shell, _ := ended["shell"].(map[string]any); shell["status"] != "exited" || shell["exitCode"] != float64(4) {
		t.Fatalf("ended = %v", ended)
	}
	// Its notification, later, changes nothing.
	h.mod(promptRow("note-1", "task-notification", taskNotification("b1", path, "failed", "failed with exit code 4"), nil))
	if shell := h.shells().BackgroundShells[0]; *shell.ExitCode != 4 {
		t.Fatalf("shell = %+v", shell)
	}
}
