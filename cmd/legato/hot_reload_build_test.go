package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHotReloadCheckoutRootRequiresChildTaskfileAndResolvesSymlinks(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "legato")
	if err := os.WriteFile(executable, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := hotReloadCheckoutRoot(false, executable); ok {
		t.Fatal("checkout detected outside hot reload child")
	}
	if _, ok := hotReloadCheckoutRoot(true, executable); ok {
		t.Fatal("checkout detected without Taskfile.yml")
	}
	if err := os.Mkdir(filepath.Join(root, "Taskfile.yml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := hotReloadCheckoutRoot(true, executable); ok {
		t.Fatal("checkout detected with Taskfile.yml directory")
	}
	if err := os.Remove(filepath.Join(root, "Taskfile.yml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Taskfile.yml"), []byte("version: '3'"), 0o644); err != nil {
		t.Fatal(err)
	}
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "legato")
	if err := os.Symlink(executable, link); err != nil {
		t.Fatal(err)
	}
	got, ok := hotReloadCheckoutRoot(true, link)
	if !ok || got != root {
		t.Fatalf("checkout root = %q, %v", got, ok)
	}
}

func TestHotReloadBuildTriggerStartsFullBuildFromRootAndRejectsConcurrentBuild(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(root, "calls")
	script := "#!/bin/sh\nprintf '%s|%s' \"$PWD\" \"$*\" > " + calls + "\necho building\nsleep 0.2\n"
	if err := os.WriteFile(filepath.Join(bin, "task"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	trigger := newHotReloadBuildTrigger(root)

	start := make(chan struct{})
	started, err := trigger(start)
	if err != nil || !started {
		t.Fatalf("start = %v, %v", started, err)
	}
	started, err = trigger(make(chan struct{}))
	if err != nil || started {
		t.Fatalf("concurrent start = %v, %v", started, err)
	}
	close(start)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		data, readErr := os.ReadFile(calls)
		if readErr == nil {
			if got := string(data); got != root+"|build:full" {
				t.Fatalf("call=%q", got)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	logData, err := os.ReadFile(filepath.Join(root, ".legato", "run", "hot-reload-build.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "building") {
		t.Fatalf("log=%q", logData)
	}

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		start = make(chan struct{})
		started, err = trigger(start)
		if err != nil {
			t.Fatal(err)
		}
		if started {
			close(start)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("build trigger did not become available after command exit")
}
