package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerCreateInvokesYggdrasilAndReturnsPath(t *testing.T) {
	bin := writeFakeYG(t, `#!/bin/sh
printf '%s\n' "$PWD" > "$CALL_FILE"
printf '%s\n' "$@" >> "$CALL_FILE"
printf 'warning: using default template\n  /tmp/legato.feature-one  \n'
`)
	callFile := filepath.Join(t.TempDir(), "call")
	t.Setenv("CALL_FILE", callFile)

	primaryDir := t.TempDir()
	runner := NewRunner(bin)
	path, err := runner.Create(context.Background(), "task-1", primaryDir, "feature-one", "main")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/tmp/legato.feature-one" {
		t.Fatalf("path = %q, want %q", path, "/tmp/legato.feature-one")
	}
	call, err := os.ReadFile(callFile)
	if err != nil {
		t.Fatal(err)
	}
	assertInvocation(t, call, primaryDir, "new\nfeature-one\n--agent-owned\n--print-path\n--base\nmain\n")
}

func TestRunnerRemoveInvokesYggdrasil(t *testing.T) {
	bin := writeFakeYG(t, `#!/bin/sh
printf '%s\n' "$PWD" > "$CALL_FILE"
printf '%s\n' "$@" >> "$CALL_FILE"
`)
	callFile := filepath.Join(t.TempDir(), "call")
	t.Setenv("CALL_FILE", callFile)
	primaryDir := t.TempDir()

	if err := NewRunner(bin).Remove(context.Background(), primaryDir, "feature-one"); err != nil {
		t.Fatal(err)
	}
	call, err := os.ReadFile(callFile)
	if err != nil {
		t.Fatal(err)
	}
	assertInvocation(t, call, primaryDir, "remove\nfeature-one\n")
}

func TestRunnerCreateReturnsLastNonEmptyLine(t *testing.T) {
	script := "#!/bin/sh\nprintf 'prelude\\nnoise\\n  /real/path  \\n'\n"
	bin := writeFakeYG(t, script)
	path, err := NewRunner(bin).Create(context.Background(), "task-1", t.TempDir(), "feature", "main")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/real/path" {
		t.Fatalf("path = %q, want %q", path, "/real/path")
	}
}

func TestRunnerCreateSurfacesYggdrasilFailure(t *testing.T) {
	bin := writeFakeYG(t, "#!/bin/sh\necho 'branch already checked out' >&2\nexit 7\n")

	_, err := NewRunner(bin).Create(context.Background(), "task-1", t.TempDir(), "feature", "main")
	if err == nil || !strings.Contains(err.Error(), "branch already checked out") {
		t.Fatalf("error = %v, want yggdrasil stderr", err)
	}
}

func writeFakeYG(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yg")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertInvocation(t *testing.T, call []byte, primaryDir, wantArgs string) {
	t.Helper()
	parts := strings.SplitN(string(call), "\n", 2)
	if len(parts) != 2 {
		t.Fatalf("malformed invocation: %q", call)
	}
	gotDir, err := filepath.EvalSymlinks(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	wantDir, err := filepath.EvalSymlinks(primaryDir)
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != wantDir || parts[1] != wantArgs {
		t.Fatalf("invocation:\n%s\nwant directory %q and args:\n%s", call, primaryDir, wantArgs)
	}
}
