package hooks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/hooks"
)

func TestYggdrasilInstallPreservesAndDeduplicates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".yggdrasil.toml")
	before := `title = "keep"
[hooks]
post_create = [
  'echo keep',
  'legato task worktree set "$LEGATO_TASK_ID" --path "$YG_WORKTREE"',
  '/old/legato task worktree set "$LEGATO_TASK_ID"',
]
post_remove = ['echo untouched']
[other]
value = 7
`
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter := hooks.NewYggdrasilAdapter("/ignored/legato")
	if err := adapter.InstallHooks(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Count(got, "task worktree set") != 1 {
		t.Fatalf("managed hooks = %d, want 1\n%s", strings.Count(got, "task worktree set"), got)
	}
	for _, want := range []string{"title = \"keep\"", "'echo keep'", "post_remove = ['echo untouched']", "[other]\nvalue = 7"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing preserved content %q\n%s", want, got)
		}
	}
	for _, want := range []string{"command -v legato", "[ -z \"$LEGATO_TASK_ID\" ]", "legato task worktree set", "--path \"$YG_WORKTREE\"", "--primary-dir \"$YG_PRIMARY\"", "--branch \"$YG_BRANCH\"", "--base-branch \"$YG_BASE\""} {
		if !strings.Contains(got, want) {
			t.Errorf("canonical hook missing %q\n%s", want, got)
		}
	}
}

func TestYggdrasilInstallRejectsMissingOrMalformedConfig(t *testing.T) {
	adapter := hooks.NewYggdrasilAdapter("legato")
	if err := adapter.InstallHooks(t.TempDir()); err == nil {
		t.Fatal("expected missing config error")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".yggdrasil.toml"), []byte("[hooks\npost_create = ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := adapter.InstallHooks(dir); err == nil {
		t.Fatal("expected malformed config error")
	}
}

func TestYggdrasilUninstallOnlyRecognizedHooks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".yggdrasil.toml")
	before := `[hooks]
post_create = ['echo keep', 'command -v legato && legato task worktree set "$LEGATO_TASK_ID"', 'echo legato']
[other]
x = 1
`
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := hooks.NewYggdrasilAdapter("legato").UninstallHooks(dir); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	got := string(data)
	if strings.Contains(got, "task worktree set") {
		t.Fatalf("recognized hook remains: %s", got)
	}
	for _, want := range []string{"echo keep", "echo legato", "[other]\nx = 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
}
