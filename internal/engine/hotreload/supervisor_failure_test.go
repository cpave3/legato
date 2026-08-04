//go:build unix

package hotreload

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunForceStopsTimedOutChildBeforeReplacement(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "app")
	logPath := filepath.Join(dir, "events")
	pidPath := filepath.Join(dir, "pid")
	writeHelper(t, binary, fmt.Sprintf("trap '' TERM INT\necho $$ > %q\necho old:start >> %q\nwhile :; do sleep 0.05; done\n", pidPath, logPath))
	var stderr bytes.Buffer
	signals := make(chan os.Signal, 1)
	done := make(chan int, 1)
	go func() {
		done <- Run(Options{Path: binary, Env: os.Environ(), Dir: dir, Stderr: &stderr, PollInterval: 15 * time.Millisecond, StablePolls: 2, ShutdownTimeout: 60 * time.Millisecond, Signals: signals})
	}()
	waitForText(t, logPath, "old:start")
	replacement := filepath.Join(dir, "replacement")
	writeHelper(t, replacement, fmt.Sprintf("if kill -0 $(cat %q) 2>/dev/null; then echo overlap >> %q; fi\necho new:start >> %q\ntrap 'exit 0' TERM INT\nwhile :; do sleep 0.05; done\n", pidPath, logPath, logPath))
	if err := os.Rename(replacement, binary); err != nil {
		t.Fatal(err)
	}
	waitForText(t, logPath, "new:start")
	pid, err := strconv.Atoi(strings.TrimSpace(readString(pidPath)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("timed-out child %d is still running", pid)
	}
	if strings.Contains(readString(logPath), "overlap") {
		t.Fatalf("replacement started before old child exited:\n%s", readString(logPath))
	}
	signals <- syscall.SIGTERM
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("Run = %d, stderr = %s", code, stderr.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not stop")
	}
	if strings.Contains(stderr.String(), "keeping the current child") {
		t.Fatalf("supervisor retained the timed-out child: %s", stderr.String())
	}
}

func TestRunForceStopsChildWhenSupervisorIsTerminated(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "app")
	pidPath := filepath.Join(dir, "pid")
	writeHelper(t, binary, fmt.Sprintf("trap '' TERM INT\necho pid:$$ > %q\nwhile :; do sleep 0.05; done\n", pidPath))
	signals := make(chan os.Signal, 1)
	done := make(chan int, 1)
	go func() {
		done <- Run(Options{Path: binary, Env: os.Environ(), Dir: dir, PollInterval: time.Second, ShutdownTimeout: 40 * time.Millisecond, Signals: signals})
	}()
	waitForText(t, pidPath, "pid:")
	pid, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(readString(pidPath)), "pid:"))
	if err != nil {
		t.Fatal(err)
	}

	signals <- syscall.SIGTERM
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("supervisor remained blocked on signal-ignoring child")
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("child %d is still running after supervisor shutdown", pid)
	}
}

func TestRunWaitsForLaterBuildAfterReplacementFailsImmediately(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "app")
	logPath := filepath.Join(dir, "events")
	writeLongRunningHelper(t, binary, "old", logPath)
	var stderr bytes.Buffer
	signals := make(chan os.Signal, 1)
	done := make(chan int, 1)
	go func() {
		done <- Run(Options{Path: binary, Env: os.Environ(), Dir: dir, Stderr: &stderr, PollInterval: 15 * time.Millisecond, StablePolls: 2, ShutdownTimeout: time.Second, StartupWindow: 100 * time.Millisecond, Signals: signals})
	}()
	waitForText(t, logPath, "old:start")

	failed := filepath.Join(dir, "failed")
	writeHelper(t, failed, fmt.Sprintf("echo failed:start >> %q\nexit 7\n", logPath))
	if err := os.Rename(failed, binary); err != nil {
		t.Fatal(err)
	}
	waitForText(t, logPath, "failed:start")
	select {
	case code := <-done:
		t.Fatalf("supervisor exited after failed replacement with %d", code)
	case <-time.After(150 * time.Millisecond):
	}

	next := filepath.Join(dir, "next")
	writeLongRunningHelper(t, next, "next", logPath)
	if err := os.Rename(next, binary); err != nil {
		t.Fatal(err)
	}
	waitForText(t, logPath, "next:start")
	signals <- syscall.SIGTERM
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("Run = %d, stderr = %s", code, stderr.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not stop")
	}
}

func readString(path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}
