package main

import (
	"strings"
	"testing"
)

// Every leaf of the CLI has a class for the Reeve's legato tool.
func TestEveryLegatoSubcommandHasAClass(t *testing.T) {
	leaves := map[string]bool{}
	var walk func(prefix []string, c *commandDef)
	walk = func(prefix []string, c *commandDef) {
		path := append(append([]string{}, prefix...), c.Name)
		if len(c.Children) == 0 {
			leaves[strings.Join(path[1:], " ")] = true
			return
		}
		for _, child := range c.Children {
			walk(path, child)
		}
	}
	walk(nil, commandTree())
	for leaf := range leaves {
		entry, ok := legatoCommandClasses[leaf]
		if !ok {
			t.Errorf("legato %s has no class: add it to legatoCommandClasses", leaf)
			continue
		}
		switch entry.Class {
		case "read", "change", "risky", "never":
		default:
			t.Errorf("legato %s: class %q", leaf, entry.Class)
		}
		if entry.Description == "" {
			t.Errorf("legato %s has no description", leaf)
		}
	}
	for path := range legatoCommandClasses {
		if !leaves[path] {
			t.Errorf("legato %s is classed but is not a command", path)
		}
	}
}

func TestALegatoCommandIsClassedByItsPath(t *testing.T) {
	if class, _ := classifyLegatoCommand([]string{"task", "list", "--json"}); class != "read" {
		t.Errorf("task list = %s", class)
	}
	if class, _ := classifyLegatoCommand([]string{"restart"}); class != "risky" {
		t.Errorf("restart = %s", class)
	}
	if class, _ := classifyLegatoCommand([]string{"serve"}); class != "never" {
		t.Errorf("serve = %s", class)
	}
	if class, _ := classifyLegatoCommand([]string{"no-such-command"}); class != "never" {
		t.Errorf("an unknown command = %s", class)
	}
	if class, _ := classifyLegatoCommand([]string{"task"}); class != "never" {
		t.Errorf("a group = %s", class)
	}
}

// A refused command says what to use instead.
func TestARefusedCommandNamesItsAlternative(t *testing.T) {
	for args, want := range map[string][]string{
		"credential show":     {"legato credential list", "GET /api/credentials"},
		"audit":               {"legato audit verify", "GET /api/credentials/audit"},
		"audit log":           {"GET /api/credentials/audit"},
		"credential get x":    {"legato://", "request_secret"},
		"serve":               {"restart_legato"},
		"auth token":          {"never"},
		"task":                {"task list (read)", "task create (change)"},
		"no-such-command":     {"legato_api", "reference/api"},
		"agent state working": {"hooks"},
	} {
		class, description := classifyLegatoCommand(strings.Fields(args))
		if class != "never" {
			t.Errorf("legato %s = %s", args, class)
		}
		for _, w := range want {
			if !strings.Contains(description, w) {
				t.Errorf("legato %s: %q does not name %q", args, description, w)
			}
		}
	}
}

// Every command the Reeve may never run names what to do instead.
func TestEveryNeverCommandHasAnAlternative(t *testing.T) {
	for path, entry := range legatoCommandClasses {
		if entry.Class == "never" && legatoCommandAlternatives[path] == "" {
			t.Errorf("legato %s is never run: say what to do instead in legatoCommandAlternatives", path)
		}
	}
	for path := range legatoCommandAlternatives {
		if _, ok := legatoCommandClasses[path]; ok && legatoCommandClasses[path].Class != "never" {
			t.Errorf("legato %s runs, so needs no alternative", path)
		}
	}
}

// The tool's description lists the commands the Reeve may run, by class,
// and none it may not.
func TestTheLegatoToolListsTheCommandsItRuns(t *testing.T) {
	var list []string
	for _, c := range (legatoCommands{}).Commands() {
		list = append(list, c.Path+" "+c.Class)
	}
	got := strings.Join(list, ",")
	for _, want := range []string{"task list read", "task create change", "restart risky", "config show read"} {
		if !strings.Contains(got, want) {
			t.Errorf("the list lacks %q: %s", want, got)
		}
	}
	for _, never := range []string{"serve never", "auth token", "credential get"} {
		if strings.Contains(got, never) {
			t.Errorf("the list has %q", never)
		}
	}
}
