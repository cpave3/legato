package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/claudecode"
	"github.com/cpave3/legato/internal/service"
)

// cliClaudeReleases is a release server whose channel says latest.
type cliClaudeReleases struct{ latest string }

func (r *cliClaudeReleases) Latest(context.Context, string) (string, error) { return r.latest, nil }

func (r *cliClaudeReleases) Fetch(_ context.Context, version, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, version)
	return path, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755)
}

// withCLITools makes legato tools act on a Claude Code that Legato took
// from a native 2.1.326, with 2.1.330 on its channel.
func withCLITools(t *testing.T) claudecode.Folder {
	t.Helper()
	native := filepath.Join(t.TempDir(), "versions", "2.1.326")
	os.MkdirAll(filepath.Dir(native), 0o755)
	if err := os.WriteFile(native, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	folder := claudecode.Folder{Dir: filepath.Join(t.TempDir(), "tools", "claude")}
	tool := service.NewClaudeCodeTool(service.ClaudeCodeToolOptions{
		Folder: folder, Releases: &cliClaudeReleases{latest: "2.1.330"},
		Installed: func() (string, string, bool) { return claudecode.InstalledVersion(native) },
	})
	old := cliTools
	cliTools = func() ([]service.ManagedTool, func(), error) { return []service.ManagedTool{tool}, func() {}, nil }
	t.Cleanup(func() { cliTools = old })
	return folder
}

func TestLegatoToolsListsUpdatesAndRollsBackClaudeCode(t *testing.T) {
	folder := withCLITools(t)

	out, code := runCapturingStdout(t, func() int { return runToolsCmd("list", []string{"--json"}) })
	var listed struct {
		OK   bool                 `json:"ok"`
		Data []service.ToolStatus `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil || code != 0 || len(listed.Data) != 1 || listed.Data[0].Name != "claude" || listed.Data[0].Latest != "2.1.330" {
		t.Fatalf("list: %d %s", code, out)
	}

	out, code = runCapturingStdout(t, func() int { return runToolsCmd("update", []string{"claude"}) })
	if code != 0 || !strings.Contains(out, "2.1.330") {
		t.Fatalf("update: %d %s", code, out)
	}
	if cur, _ := folder.Current(); cur != "2.1.330" {
		t.Fatalf("current after update = %s", cur)
	}

	out, code = runCapturingStdout(t, func() int { return runToolsCmd("rollback", []string{"claude"}) })
	if code != 0 || !strings.Contains(out, "2.1.326") {
		t.Fatalf("rollback: %d %s", code, out)
	}
	if cur, _ := folder.Current(); cur != "2.1.326" {
		t.Fatalf("current after rollback = %s", cur)
	}

	if code := runToolsCmd("rollback", []string{"claude"}); code == 0 {
		t.Fatal("a second rollback succeeded with nothing to go back to")
	}
	if code := runToolsCmd("update", []string{"nope"}); code == 0 {
		t.Fatal("an unknown tool was updated")
	}
}

func TestLegatoToolsRunsOnTheHostOnly(t *testing.T) {
	withCLITools(t)
	t.Setenv("LEGATO_SANDBOXED", "1")
	if code := runToolsCmd("list", nil); code != exitForbidden {
		t.Fatalf("exit %d in a sandbox", code)
	}
}
