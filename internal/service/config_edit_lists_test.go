package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const githubCredential = `# My Legato.
credentials:
  # GitHub, as the App.
  - name: github
    profile: github
    secret: legato://github-app-key
    settings:
      client_id: Iv1.abc
      app_id: "12345"
      installation_id: "678"
      repositories: [cpave3/legato]
    policy:
      - { operation: git.push, resource: "github:{repositories}@refs/heads/legato/**", approve: session }
      - { operation: pr.create, resource: "github:{repositories}@refs/heads/legato/**", approve: session }
theme: default
`

// One rule inside the github credential changes, the edited file is
// checked to load, and no other line of the file changes.
func TestAConfigEditChangesOneRuleOfACredential(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(githubCredential), 0o600); err != nil {
		t.Fatal(err)
	}
	edits := []ConfigEdit{{Key: "credentials[name=github].policy[1].operation", Value: "pr.create-draft"}}
	diff, loadErr, err := CheckConfigEdit(path, edits)
	if err != nil || loadErr != "" {
		t.Fatalf("check: %v, load: %s", err, loadErr)
	}
	var removed, added []string
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "- "):
			removed = append(removed, line)
		case strings.HasPrefix(line, "+ "):
			added = append(added, line)
		}
	}
	if len(removed) != 1 || len(added) != 1 || !strings.Contains(added[0], "operation: pr.create-draft,") ||
		removed[0] != "- "+strings.Replace(strings.TrimPrefix(added[0], "+ "), "pr.create-draft,", "pr.create,", 1) {
		t.Fatalf("removed %q, added %q: want one line changed, pr.create to pr.create-draft\n%s", removed, added, diff)
	}
	if _, err := WriteConfigEdit(path, edits); err != nil {
		t.Fatal(err)
	}
	written, _ := os.ReadFile(path)
	if !strings.Contains(string(written), "# GitHub, as the App.") || !strings.Contains(string(written), "pr.create-draft") {
		t.Fatalf("written:\n%s", written)
	}
}

// An edit to a list entry that leaves the file unloadable is caught.
func TestAConfigEditOfAListEntryThatDoesNotLoadIsCaught(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte(githubCredential), 0o600)
	_, loadErr, err := CheckConfigEdit(path, []ConfigEdit{{Key: "credentials[name=github].policy[0].approve", Value: "whenever"}})
	if err != nil || loadErr == "" {
		t.Fatalf("an unloadable edit: err %v, load %q", err, loadErr)
	}
}

// Reaching into a list of the Reeve's own rules is editing them.
func TestAConfigEditInsideTheReevesRulesNeedsASignature(t *testing.T) {
	f := newChangeSetFixture(t)
	os.WriteFile(os.Getenv("LEGATO_CONFIG"), []byte("reeve:\n  rules:\n    - { pattern: \"GET *\", level: approve }\n"), 0o600)
	_, err := f.svc.Propose(context.Background(), f.reeve, "x", []ChangeCall{{Kind: CallConfigEdit, Edits: []ConfigEdit{{Key: "reeve.rules[0].level", Value: "auto"}}}})
	if !errors.Is(err, ErrNoDeviceEnrolled) {
		t.Fatalf("an edit inside reeve.rules with no device: %v", err)
	}
}
