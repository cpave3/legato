package service

import (
	"github.com/cpave3/legato/internal/engine/sandbox"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamedClaudeAccountCannotReadOtherLoginsInsideBwrap(t *testing.T) {
	in := bwrapFixture(t)
	parent := filepath.Join(filepath.Dir(in.Home), "accounts")
	selected, other := filepath.Join(parent, "work"), filepath.Join(parent, "other")
	for _, root := range []string{selected, other, filepath.Join(in.Home, ".claude")} {
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".credentials.json"), []byte("synthetic-only"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(other, ".credentials.json"), filepath.Join(selected, "leak")); err != nil {
		t.Fatal(err)
	}
	in.ClaudeConfigDir = selected
	in.ClaudeConfigExplicit = true
	in.ToolState = []string{selected}
	in.ProtectedClaudeAccounts = []string{other, filepath.Join(in.Home, ".claude"), filepath.Join(in.Home, ".claude.json")}
	in.Extra = []SandboxPath{{Path: parent, Access: "read"}}
	out, err := runInSandbox(t, in, `test -r `+shellWord(filepath.Join(selected, ".credentials.json"))+` && touch `+shellWord(filepath.Join(selected, "child-write"))+` && test ! -r `+shellWord(filepath.Join(other, ".credentials.json"))+` && test ! -r "$HOME/.claude/.credentials.json" && test ! -r `+shellWord(filepath.Join(selected, "leak"))+` && echo isolated`)
	if err != nil || !strings.Contains(out, "isolated") {
		t.Fatalf("named isolation: %v: %s", err, out)
	}
	if _, err := os.Stat(filepath.Join(selected, "child-write")); err != nil {
		t.Fatal("selected account was not writable:", err)
	}
}

func TestSandboxMountsOnlySelectedClaudeStateAndCoversOtherAccounts(t *testing.T) {
	in := baseInput()
	fs := in.FS.(fakeFS)
	fs["/accounts"] = "dir"
	fs["/accounts/work"] = "dir"
	fs["/accounts/other"] = "dir"
	fs["/var/home/me/.local/share/claude"] = "dir"
	in.ClaudeConfigDir = "/accounts/work"
	in.ClaudeConfigExplicit = true
	in.ProtectedClaudeAccounts = []string{"/accounts/other", "/var/home/me/.claude", "/var/home/me/.claude.json"}
	in.ToolState = []string{"/accounts/work"}
	in.Extra = []SandboxPath{{Path: "/accounts", Access: "read"}}
	plan, err := BuildSandboxPlan(in)
	if err != nil {
		t.Fatal(err)
	}
	mounts := mounted(plan)
	if mounts["/accounts/work"].Kind != sandbox.ReadWrite {
		t.Fatal("selected account not writable")
	}
	for _, root := range []string{"/var/home/me/.claude", "/var/home/me/.claude.json", "/accounts/other"} {
		if m, ok := mounts[root]; ok && binds(m.Kind) {
			t.Fatalf("other account exposed: %+v", m)
		}
	}
	if mounts["/accounts/other"].Kind != sandbox.HideDir {
		t.Fatal("parent resource exposed other account")
	}
	if mounts["/var/home/me/.local/share/claude"].Kind != sandbox.ReadOnly {
		t.Fatal("CLI installation is not independent/read-only")
	}
	in.WorkDir = "/accounts/other"
	if _, err := BuildSandboxPlan(in); err == nil {
		t.Fatal("other account accepted as workdir")
	}
}
