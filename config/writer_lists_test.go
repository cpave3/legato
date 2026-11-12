package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const withCredentials = `# Legato.
credentials:
  # GitHub, through the App.
  - name: github
    profile: github
    policy:
      - { operation: git.push, approve: session } # my branches
      - { operation: pr.create, approve: request }
  - name: elevenlabs
    secret: legato://elevenlabs-key
workspaces:
  - name: Work
    color: "#ff0000"
`

func edit(t *testing.T, src string, fn func(doc *yaml.Node) error) (string, error) {
	t.Helper()
	out, err := EditBytes([]byte(src), fn)
	return string(out), err
}

// A key reaches into a list by an entry's field or its index, and only the
// value it names changes.
func TestAKeyReachesAListEntryByFieldOrIndex(t *testing.T) {
	got, err := edit(t, withCredentials, func(doc *yaml.Node) error {
		return SetKey(doc, "credentials[name=github].policy[1].operation", "pr.create-draft")
	})
	if err != nil {
		t.Fatal(err)
	}
	// The writer's own formatting of the file, edited by nothing.
	same, err := edit(t, withCredentials, func(*yaml.Node) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(same, "operation: pr.create,", "operation: pr.create-draft,", 1)
	if got != want || want == same {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	got, err = edit(t, withCredentials, func(doc *yaml.Node) error {
		return SetKey(doc, "workspaces[0].color", "#0000ff")
	})
	if err != nil || !strings.Contains(got, `color: '#0000ff'`) && !strings.Contains(got, `color: "#0000ff"`) {
		t.Fatalf("by index: %v\n%s", err, got)
	}
}

// [+] adds an entry at the end of a list.
func TestAKeyAppendsAListEntry(t *testing.T) {
	got, err := edit(t, withCredentials, func(doc *yaml.Node) error {
		return SetKey(doc, "credentials[name=github].policy[+]", map[string]any{"operation": "pr.create-draft", "approve": "session"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Credentials []struct {
			Name   string
			Policy []map[string]string
		}
	}
	if err := yaml.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatal(err)
	}
	policy := parsed.Credentials[0].Policy
	if len(policy) != 3 || policy[2]["operation"] != "pr.create-draft" || policy[2]["approve"] != "session" {
		t.Fatalf("policy = %v\n%s", policy, got)
	}
	for _, kept := range []string{"# GitHub, through the App.", "# my branches", "name: elevenlabs"} {
		if !strings.Contains(got, kept) {
			t.Errorf("lost %q:\n%s", kept, got)
		}
	}
}

// Removing reaches list entries too: a whole entry, or a key in one.
func TestRemovingReachesListEntries(t *testing.T) {
	got, err := edit(t, withCredentials, func(doc *yaml.Node) error {
		if !RemoveKey(doc, "credentials[name=github].policy[0]") {
			t.Error("the first rule was not there")
		}
		if !RemoveKey(doc, "credentials[name=elevenlabs].secret") {
			t.Error("elevenlabs's secret was not there")
		}
		if RemoveKey(doc, "credentials[name=nobody].secret") {
			t.Error("removed from an entry that does not exist")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "git.push") || strings.Contains(got, "elevenlabs-key") || !strings.Contains(got, "pr.create") || !strings.Contains(got, "name: elevenlabs") {
		t.Fatalf("got:\n%s", got)
	}
}

// A key that names no entry, or more than one, or reaches into what is not
// a list, is an error that says so; nothing is made up.
func TestAKeyThatNamesNoSingleEntryIsAnError(t *testing.T) {
	for key, want := range map[string]string{
		"credentials[name=nobody].secret":  "no entry",
		"credentials[5].secret":            "no entry 5",
		"workspaces[name=Work].color[0]":   "not a list",
		"credentials[name=github.policy":   "unclosed",
		"credentials[profile].secret":      "field=value",
		"credentials[name=github][0].name": "not a list",
	} {
		_, err := edit(t, withCredentials, func(doc *yaml.Node) error { return SetKey(doc, key, "x") })
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want an error saying %q", key, err, want)
		}
	}
	twice := withCredentials + "  - name: Work\n"
	if _, err := edit(t, twice, func(doc *yaml.Node) error { return SetKey(doc, "workspaces[name=Work].color", "x") }); err == nil || !strings.Contains(err.Error(), "2 entries") {
		t.Errorf("two matches: %v", err)
	}
}

func TestPlainKeyLeavesOutTheSelectors(t *testing.T) {
	if got := PlainKey("reeve.rules[0].level"); got != "reeve.rules.level" {
		t.Errorf("PlainKey = %q", got)
	}
	if got := PlainKey("credentials[name=a.b].policy[+]"); got != "credentials.policy" {
		t.Errorf("PlainKey = %q", got)
	}
}
