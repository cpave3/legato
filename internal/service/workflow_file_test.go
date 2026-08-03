package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileRemoveExecutorRejectsSymlinkOutsideWorkingDirectory(t *testing.T) {
	working := t.TempDir()
	outside := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(outside, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(working, "report.md")); err != nil {
		t.Fatal(err)
	}

	_, err := NewFileRemoveExecutor().Start(context.Background(), ResolvedWorkflowStep{With: map[string]any{
		"working_dir": working,
		"path":        "report.md",
	}})
	if err == nil || !strings.Contains(err.Error(), "escapes the working directory") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("outside file was removed: %v", err)
	}
}
