package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const commented = `# My Legato.
theme: default # the look

agents:
  # Ask for review tours.
  reviews: false
  peer_awareness: true

jira:
  api_token: ${JIRA_TOKEN}

workspaces:
  - name: Work
    color: "#ff0000"
  - name: Home
    color: "#00ff00"
`

func TestEditingKeepsCommentsOrderAndReferencesAndLeavesABackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte(commented), 0o600)

	backup, err := EditFile(path, func(doc *yaml.Node) error {
		RemoveKey(doc, "agents.reviews")
		RemoveListEntry(doc, "workspaces", "name", "home")
		return SetKey(doc, "icons", "nerdfonts")
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	for _, want := range []string{"# My Legato.", "theme: default # the look", "peer_awareness: true", "api_token: ${JIRA_TOKEN}", "name: Work", "icons: nerdfonts"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("lost %q:\n%s", want, got)
		}
	}
	for _, gone := range []string{"reviews:", "name: Home"} {
		if strings.Contains(string(got), gone) {
			t.Errorf("still has %q:\n%s", gone, got)
		}
	}
	if strings.Index(string(got), "theme:") > strings.Index(string(got), "agents:") {
		t.Errorf("order changed:\n%s", got)
	}
	old, _ := os.ReadFile(backup)
	if string(old) != commented || !strings.HasPrefix(filepath.Base(backup), "config.yaml.") || !strings.HasSuffix(backup, ".bak") {
		t.Fatalf("backup %s = %q", backup, old)
	}
}

func TestRemovingTheLastKeyOfASectionRemovesTheSection(t *testing.T) {
	out, err := EditBytes([]byte("agents:\n  reviews: false\ntheme: x\n"), func(doc *yaml.Node) error {
		if !RemoveKey(doc, "agents.reviews") {
			t.Fatal("not removed")
		}
		return nil
	})
	if err != nil || strings.Contains(string(out), "agents") {
		t.Fatalf("out = %q, %v", out, err)
	}
}
