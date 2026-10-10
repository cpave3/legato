package main

import (
	"strings"
	"testing"
)

func TestCommandTreeIsCompleteAndConsistent(t *testing.T) {
	root := commandTree()
	var walk func(*commandDef, []string)
	walk = func(node *commandDef, path []string) {
		t.Helper()
		label := strings.Join(append([]string{"legato"}, path...), " ")
		if node.Summary == "" {
			t.Errorf("%s has no summary", label)
		}
		childNames := map[string]bool{}
		for _, child := range node.Children {
			if childNames[child.Name] {
				t.Errorf("%s registers %q more than once", label, child.Name)
			}
			childNames[child.Name] = true
			walk(child, append(path, child.Name))
		}
		if len(node.Children) == 0 && node.Run == nil {
			t.Errorf("%s has neither children nor a handler", label)
		}
		if len(node.Examples) == 0 {
			t.Errorf("%s has no example", label)
		}
		for _, example := range node.Examples {
			trimmed := strings.TrimSpace(example)
			if strings.HasSuffix(trimmed, " --help") || strings.HasSuffix(trimmed, " -h") {
				t.Errorf("%s has self-referential example %q", label, example)
			}
		}
		flagNames := map[string]bool{}
		for _, flag := range node.Flags {
			if flagNames[flag.Name] {
				t.Errorf("%s registers --%s more than once", label, flag.Name)
			}
			flagNames[flag.Name] = true
			if !flag.Boolean && flag.Value == "" {
				t.Errorf("%s --%s has no value name", label, flag.Name)
			}
		}
	}
	walk(root, nil)
}

func TestRequiredPreviouslyMissingCommandsAreRegistered(t *testing.T) {
	root := commandTree()
	for _, path := range [][]string{
		{"swarm", "create"},
		{"swarm", "next-step"},
		{"agent", "session-created"},
		{"agent", "bridge"},
		{"agent", "codex"},
		{"agent", "codex-bridge"},
		{"review", "chapter", "edit"},
		{"review", "chapter", "remove"},
		{"review", "annotation", "edit"},
		{"review", "annotation", "remove"},
		{"workflow", "step", "complete"},
		{"workflow", "step", "fail"},
	} {
		node, ok := findCommand(root, path)
		if !ok || node.Run == nil {
			t.Errorf("%s is not a registered leaf command", strings.Join(path, " "))
		}
	}
}

func TestEveryCommandLevelHasGeneratedHelp(t *testing.T) {
	root := commandTree()
	var walk func(*commandDef, []string)
	walk = func(node *commandDef, path []string) {
		t.Helper()
		got := helpTextFor(root, path)
		if !strings.Contains(got, node.Summary) || !strings.Contains(got, "Usage:") {
			t.Errorf("incomplete help for %q:\n%s", strings.Join(path, " "), got)
		}
		for _, child := range node.Children {
			if !strings.Contains(got, child.Name) {
				t.Errorf("help for %q omits child %q", strings.Join(path, " "), child.Name)
			}
			walk(child, append(path, child.Name))
		}
		for _, flag := range node.Flags {
			if !strings.Contains(got, "--"+flag.Name) {
				t.Errorf("help for %q omits --%s", strings.Join(path, " "), flag.Name)
			}
		}
		if len(node.Children) == 0 {
			for _, example := range node.Examples {
				if !strings.Contains(got, example) {
					t.Errorf("help for %q omits example %q", strings.Join(path, " "), example)
				}
			}
		}
		for _, child := range node.Children {
			if !strings.Contains(got, child.Examples[0]) {
				t.Errorf("help for %q omits first example from child %q", strings.Join(path, " "), child.Name)
			}
		}
	}
	walk(root, nil)
}

func TestInstalledHookCommandLinesResolve(t *testing.T) {
	root := commandTree()
	for _, args := range [][]string{
		{"agent", "state", "task-id", "--activity", "working", "--working-dir", "/tmp/work", "--session-id", "chimera-child", "--event", "Stop"},
		{"agent", "session-created", "task-id", "session-id", "--hook-payload"},
		{"agent", "bridge", "task-id", "--execution-id", "ex-1"},
		{"task", "link", "task-id", "--repo", "owner/repo", "--sha", "abc"},
		{"task", "worktree", "set", "task-id", "--path", "/tmp/work"},
	} {
		node, _, remaining, err := resolveCommand(root, args)
		if err != nil {
			t.Fatalf("%v did not resolve: %v", args, err)
		}
		parsed, parseErr := parseCommandArgs(remaining, commandFlagSpecs(args[:len(args)-len(remaining)]...))
		if parseErr != nil {
			t.Fatalf("%v did not parse: %v", args, parseErr)
		}
		if node.Run == nil || len(parsed.Positionals) == 0 {
			t.Fatalf("%v did not resolve to a usable leaf", args)
		}
	}
}
