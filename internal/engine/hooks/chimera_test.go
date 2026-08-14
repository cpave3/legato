package hooks_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

// chimeraEvents lists the (event-dir, script-name, expected --activity) tuples
// for every hook the adapter installs. Kept here so tests verify the behavior
// the adapter exposes, not the constants it uses internally.
var chimeraEvents = []struct {
	dir      string
	script   string
	activity string // "" means clear
}{
	{"SessionCreated", "legato-session-created.sh", "capture"},
	{"UserPromptSubmit", "legato-prompt-submit.sh", "working"},
	{"PermissionRequest", "legato-permission.sh", "waiting"},
	{"PermissionResolved", "legato-permission-resolved.sh", "payload"},
	{"UserInputRequired", "legato-user-input-required.sh", "waiting"},
	{"Stop", "legato-stop.sh", ""},
	{"OnError", "legato-on-error.sh", "error"},
	{"SessionEnd", "legato-session-end.sh", ""},
	{"Interrupt", "legato-interrupt.sh", ""},
	{"Timeout", "legato-timeout.sh", ""},
}

func TestChimeraAdapter_Name(t *testing.T) {
	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	if adapter.Name() != "chimera" {
		t.Errorf("Name() = %q, want %q", adapter.Name(), "chimera")
	}
}

func TestChimeraAdapter_EnvVars(t *testing.T) {
	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	vars := adapter.EnvVars("task123", "/tmp/legato.sock")
	// Chimera must set LEGATO_TASK_ID so the ~/.chimera/hooks scripts
	// don't exit early on the empty-env-var gate.
	if got := vars["LEGATO_TASK_ID"]; got != "task123" {
		t.Errorf("LEGATO_TASK_ID = %q, want task123", got)
	}
}

func TestChimeraAdapter_RolePromptPreambleMentionsTaskShow(t *testing.T) {
	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	preamble := adapter.RolePromptPreamble()
	for _, want := range []string{
		"legato task show $LEGATO_TASK_ID",
		"--format full",
		"host mode",
	} {
		if !strings.Contains(preamble, want) {
			t.Errorf("preamble missing %q:\n%s", want, preamble)
		}
	}
}

func TestChimeraAdapter_InstallHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	if err := adapter.InstallHooks(""); err != nil {
		t.Fatal(err)
	}

	for _, ev := range chimeraEvents {
		path := filepath.Join(home, ".chimera", "hooks", ev.dir, ev.script)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("hook script not found for %s: %v", ev.dir, err)
		}
		content := string(data)

		if !strings.HasPrefix(content, "#!/bin/sh\n") {
			t.Errorf("%s: missing shebang", ev.dir)
		}
		if !strings.Contains(content, "[ -z \"$LEGATO_TASK_ID\" ] && exit 0") {
			t.Errorf("%s: missing LEGATO_TASK_ID guard", ev.dir)
		}
		if !strings.Contains(content, "[ -z \"$CHIMERA_SESSION_ID\" ] && exit 0") {
			t.Errorf("%s: missing CHIMERA_SESSION_ID guard", ev.dir)
		}
		wantCmd := fmt.Sprintf("%s agent state \"$LEGATO_TASK_ID\" --execution-id \"$LEGATO_EXECUTION_ID\" --activity %q --working-dir \"$PWD\" --session-id \"$CHIMERA_SESSION_ID\" --event %q", "/usr/bin/legato", ev.activity, ev.dir)
		if ev.dir == "SessionCreated" {
			wantCmd = `/usr/bin/legato agent session-created "$LEGATO_TASK_ID" "$CHIMERA_SESSION_ID"`
		} else if ev.dir == "PermissionResolved" {
			wantCmd = `/usr/bin/legato agent permission-resolved "$LEGATO_TASK_ID" --execution-id "$LEGATO_EXECUTION_ID" --working-dir "$PWD" --session-id "$CHIMERA_SESSION_ID"`
		}
		if !strings.Contains(content, wantCmd) {
			t.Errorf("%s: expected %q in script, got:\n%s", ev.dir, wantCmd, content)
		}

		info, _ := os.Stat(path)
		if info.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s: script should be executable, mode=%v", ev.dir, info.Mode())
		}
	}
}

func TestChimeraAdapter_InstallHooksRemovesManagedPostToolUseScript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stale := filepath.Join(home, ".chimera", "hooks", "PostToolUse", "legato-post-tool-use.sh")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("stale"), 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	if err := adapter.InstallHooks(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale managed PostToolUse hook should be removed, err=%v", err)
	}
}

func TestChimeraAdapter_UninstallHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	if err := adapter.InstallHooks(""); err != nil {
		t.Fatal(err)
	}
	if err := adapter.UninstallHooks(""); err != nil {
		t.Fatal(err)
	}

	for _, ev := range chimeraEvents {
		path := filepath.Join(home, ".chimera", "hooks", ev.dir, ev.script)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("%s: script should have been removed", ev.dir)
		}
	}
}

func TestChimeraAdapter_UninstallNonExistent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	if err := adapter.UninstallHooks(""); err != nil {
		t.Errorf("uninstall of non-existent hooks should not error: %v", err)
	}
}

func TestChimeraAdapter_InterruptKeys(t *testing.T) {
	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	keys := adapter.InterruptKeys()
	if len(keys) != 1 || keys[0] != "Escape" {
		t.Errorf("InterruptKeys() = %v, want [Escape]", keys)
	}
}

func TestChimeraAdapter_ImplementsInterruptAdapter(t *testing.T) {
	// Compile-time check: ChimeraAdapter must implement service.InterruptAdapter.
	var _ interface{ InterruptKeys() []string } = hooks.NewChimeraAdapter("/usr/bin/legato")

	// Also verify via the concrete interface from the service package.
	// We do this with a type switch at runtime to keep the test self-contained.
	adapter := hooks.NewChimeraAdapter("/usr/bin/legato")
	if _, ok := interface{}(adapter).(interface{ InterruptKeys() []string }); !ok {
		t.Error("ChimeraAdapter does not implement InterruptAdapter")
	}
}

func TestChimeraLaunchCommand(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantSub []string // each must appear as a substring of the command
		notWant []string // none may appear as a substring of the command
	}{
		{
			name: "resume with real session ID",
			env: map[string]string{
				"LEGATO_CHIMERA_SESSION_EXISTS": "resume",
				"LEGATO_CHIMERA_SESSION_ID":     "01KZTTNR9GKBTSF9BSRXP4Y1MT",
				"LEGATO_CHIMERA_SESSION_NAME":   "TASK-42",
			},
			wantSub: []string{"--session 01KZTTNR9GKBTSF9BSRXP4Y1MT"},
			notWant: []string{"--session-id", "--session-name", "--session-exists"},
		},
		{
			name: "new session with prior history (collision-safe)",
			env: map[string]string{
				"LEGATO_CHIMERA_SESSION_EXISTS": "new",
				"LEGATO_CHIMERA_SESSION_NAME":   "TASK-42",
			},
			wantSub: []string{"--session-name TASK-42", "--session-exists new"},
			notWant: []string{"--session-id"},
		},
		{
			name: "first creation (no prior session, deterministic ID)",
			env: map[string]string{
				"LEGATO_CHIMERA_SESSION_EXISTS": "new",
				"LEGATO_CHIMERA_SESSION_ID":     "TASK-42",
				"LEGATO_CHIMERA_SESSION_NAME":   "TASK-42",
			},
			wantSub: []string{"--session-name TASK-42", "--session-id TASK-42", "--session-exists new"},
			notWant: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := hooks.NewChimeraAdapter("/usr/bin/legato")
			cmd := a.LaunchCommand(tt.env, "", "")
			for _, want := range tt.wantSub {
				if !strings.Contains(cmd, want) {
					t.Errorf("cmd missing %q: %s", want, cmd)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(cmd, notWant) {
					t.Errorf("cmd should not contain %q: %s", notWant, cmd)
				}
			}
		})
	}
}

func TestChimeraPermissionResolvedScriptForwardsPayloadAndExitStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	binDir := t.TempDir()
	argsPath := filepath.Join(binDir, "args")
	stdinPath := filepath.Join(binDir, "stdin")
	fakeLegato := filepath.Join(binDir, "legato")
	fake := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" > %s\ncat > %s\nexit 23\n", argsPath, stdinPath)
	if err := os.WriteFile(fakeLegato, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}

	adapter := hooks.NewChimeraAdapter(fakeLegato)
	if err := adapter.InstallHooks(""); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(home, ".chimera", "hooks", "PermissionResolved", "legato-permission-resolved.sh")
	command := exec.Command(script)
	command.Env = append(os.Environ(), "LEGATO_TASK_ID=task-1", "LEGATO_EXECUTION_ID=exec-1", "CHIMERA_SESSION_ID=session-1")
	command.Stdin = strings.NewReader(`{"session_status":"running","remaining_pending":0}`)
	err := command.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
		t.Fatalf("script error = %v, want exit 23", err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "agent permission-resolved task-1 --execution-id exec-1") {
		t.Fatalf("args = %q", args)
	}
	payload, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"session_status":"running","remaining_pending":0}` {
		t.Fatalf("stdin = %q", payload)
	}
}
