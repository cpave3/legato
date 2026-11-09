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
