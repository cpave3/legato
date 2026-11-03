package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupClone is a git repository with the given .legato.toml, made the
// working directory, as the pane's shell is in the clone.
func setupClone(t *testing.T, legatoToml string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "c"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if legatoToml != "" {
		if err := os.WriteFile(filepath.Join(dir, ".legato.toml"), []byte(legatoToml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	return dir
}

// notedFailures replaces the task note a failed setup leaves.
func notedFailures(t *testing.T) *[]string {
	t.Helper()
	var notes []string
	old := noteSetupFailure
	noteSetupFailure = func(taskID, text string) error {
		notes = append(notes, taskID+": "+text)
		return nil
	}
	t.Cleanup(func() { noteSetupFailure = old })
	return &notes
}

func TestCloneSetupRunsTheRepositorysCommandsAndKeepsALog(t *testing.T) {
	dir := setupClone(t, "[setup]\nrun = [\"echo installing\", \"echo \\\"$YG_BASE\\\" > base.txt\"]\n")
	notes := notedFailures(t)

	if code := runCloneSetupCmd([]string{"--base", "release/2"}); code != exitOK {
		t.Fatalf("exit %d", code)
	}

	if got, _ := os.ReadFile(filepath.Join(dir, "base.txt")); strings.TrimSpace(string(got)) != "release/2" {
		t.Errorf("base.txt = %q", got)
	}
	log, err := os.ReadFile(filepath.Join(dir, ".git", "legato-setup.log"))
	if err != nil || !strings.Contains(string(log), "installing") {
		t.Errorf("log = %q (%v)", log, err)
	}
	if len(*notes) != 0 {
		t.Errorf("noted %v", *notes)
	}
}

func TestAFailedCloneSetupIsNotedOnTheTask(t *testing.T) {
	setupClone(t, "[setup]\nrun = [\"echo resolving\", \"exit 7\"]\n")
	notes := notedFailures(t)
	t.Setenv("LEGATO_TASK_ID", "task-1")

	code := runCloneSetupCmd(nil)

	if code == exitOK {
		t.Fatal("a failed setup succeeded")
	}
	if len(*notes) != 1 || !strings.HasPrefix((*notes)[0], "task-1: ") || !strings.Contains((*notes)[0], "legato-setup.log") || !strings.Contains((*notes)[0], "exit 7") {
		t.Errorf("notes = %v", *notes)
	}
}

func TestCloneSetupWithNothingToDo(t *testing.T) {
	dir := setupClone(t, "")
	notes := notedFailures(t)

	if code := runCloneSetupCmd(nil); code != exitOK {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "legato-setup.log")); err == nil {
		t.Error("a log for nothing")
	}
	if len(*notes) != 0 {
		t.Errorf("noted %v", *notes)
	}
}
