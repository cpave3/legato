//go:build unix

package hotreload

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunLaunchesChildWithInheritedContext(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "app")
	record := filepath.Join(dir, "record")
	writeHelper(t, binary, `printf '%s|%s|%s' "$1" "$HOT_RELOAD_TEST" "$PWD" > "$2"`+"\n")
	var stdout bytes.Buffer
	code := Run(Options{
		Path: binary, Args: []string{"argument", record}, Env: append(os.Environ(), "HOT_RELOAD_TEST=value"), Dir: dir,
		Stdout: &stdout, Stderr: &stdout, PollInterval: 10 * time.Millisecond,
	})
	if code != 0 {
		t.Fatalf("Run = %d, output = %s", code, stdout.String())
	}
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("argument|value|%s", dir)
	if string(got) != want {
		t.Fatalf("record = %q, want %q", got, want)
	}
}

func TestRunReplacesChildOnlyAfterOldChildExits(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "app")
	logPath := filepath.Join(dir, "events")
	writeLongRunningHelper(t, binary, "old", logPath)
	signals := make(chan os.Signal, 1)
	done := make(chan int, 1)
	go func() {
		done <- Run(Options{Path: binary, Env: os.Environ(), Dir: dir, PollInterval: 15 * time.Millisecond, StablePolls: 2, ShutdownTimeout: time.Second, StartupWindow: 40 * time.Millisecond, Signals: signals})
	}()
	waitForText(t, logPath, "old:start")

	replacement := filepath.Join(dir, "replacement")
	writeLongRunningHelper(t, replacement, "new", logPath)
	if err := os.Rename(replacement, binary); err != nil {
		t.Fatal(err)
	}
	waitForText(t, logPath, "new:start")
	signals <- syscall.SIGTERM
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("Run = %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not stop")
	}

	events, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(events)
	oldStop, newStart := strings.Index(got, "old:stop"), strings.Index(got, "new:start")
	if oldStop < 0 || newStart < 0 || oldStop > newStart {
		t.Fatalf("children overlapped or events missing:\n%s", got)
	}
	if strings.Count(got, "new:start") != 1 {
		t.Fatalf("replacement started more than once:\n%s", got)
	}
}

func writeHelper(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeLongRunningHelper(t *testing.T, path, name, logPath string) {
	t.Helper()
	body := fmt.Sprintf("echo '%s:start' >> %q\ntrap \"echo '%s:stop' >> %q; exit 0\" TERM INT\nwhile :; do sleep 0.05; done\n", name, logPath, name, logPath)
	writeHelper(t, path, body)
}

func waitForText(t *testing.T, path, text string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), text) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q", text)
}
