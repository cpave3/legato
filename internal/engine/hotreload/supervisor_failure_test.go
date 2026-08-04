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

func TestRunDoesNotOverlapWhenGracefulShutdownTimesOut(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "app")
	logPath := filepath.Join(dir, "events")
	pidPath := filepath.Join(dir, "pid")
	writeHelper(t, binary, fmt.Sprintf("echo $$ > %q\necho old:start >> %q\ntrap '' TERM INT\nwhile :; do sleep 0.05; done\n", pidPath, logPath))
	var stderr bytes.Buffer
	signals := make(chan os.Signal, 1)
	done := make(chan int, 1)
	go func() {
		done <- Run(Options{Path: binary, Env: os.Environ(), Dir: dir, Stderr: &stderr, PollInterval: 15 * time.Millisecond, StablePolls: 2, ShutdownTimeout: 60 * time.Millisecond, Signals: signals})
	}()
	waitForText(t, logPath, "old:start")
	replacement := filepath.Join(dir, "replacement")
	writeLongRunningHelper(t, replacement, "new", logPath)
	if err := os.Rename(replacement, binary); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(stderr.String(), "graceful shutdown timeout") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Contains(readString(logPath), "new:start") {
		t.Fatalf("replacement overlapped timed-out child:\n%s", readString(logPath))
	}
	pid, err := strconv.Atoi(strings.TrimSpace(readString(pidPath)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not observe helper exit")
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
