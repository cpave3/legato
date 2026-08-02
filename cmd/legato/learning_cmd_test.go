package main

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
)

func TestLearningCommandsAreWiredWithSpecificShapes(t *testing.T) {
	root := commandTree()
	for _, path := range [][]string{{"learn", "record"}, {"learn", "show"}, {"learn", "list"}, {"learn", "search"}, {"learn", "context"}, {"learn", "curate"}, {"learn", "consolidate"}, {"learn", "accept"}, {"learn", "edit"}, {"learn", "reject"}, {"learn", "incorporate"}, {"learn", "incorporation", "submit"}, {"learn", "verify"}, {"learn", "reflection", "submit"}} {
		node, ok := findCommand(root, path)
		if !ok || node.Run == nil {
			t.Fatalf("%s is not wired", strings.Join(path, " "))
		}
	}
	curate, _ := findCommand(root, []string{"learn", "curate"})
	consolidate, _ := findCommand(root, []string{"learn", "consolidate"})
	if !hasCommandFlag(curate, "task") || !hasCommandFlag(consolidate, "scope") || !hasCommandFlag(consolidate, "project") {
		t.Fatalf("manual workflow flags: curate=%#v consolidate=%#v", curate.Flags, consolidate.Flags)
	}
	verify, _ := findCommand(root, []string{"learn", "verify"})
	if hasCommandFlag(verify, "pass") || hasCommandFlag(verify, "result") || !hasCommandFlag(verify, "repo-root") || !hasCommandFlag(verify, "confirm") {
		t.Fatalf("verify must expose deterministic target flags only: %#v", verify.Flags)
	}
	reflection, _ := findCommand(root, []string{"learn", "reflection", "submit"})
	flags := map[string]bool{}
	for _, flag := range reflection.Flags {
		flags[flag.Name] = true
	}
	if !flags["request"] || !flags["file"] || !flags["json"] {
		t.Fatalf("reflection flags = %#v", flags)
	}
}

func hasCommandFlag(command *commandDef, name string) bool {
	for _, flag := range command.Flags {
		if flag.Name == name {
			return true
		}
	}
	return false
}

func TestLearningWorkflowOptionsFallBackToSwarmDefaultAgent(t *testing.T) {
	cfg := &config.Config{Swarm: config.SwarmConfig{DefaultAgent: "chimera"}, Learning: config.LearningConfig{PacketMaxItems: 1, PacketMaxBytes: 1}}
	options := learningWorkflowOptions(cfg)
	if options.Adapter != "chimera" {
		t.Fatalf("adapter = %q, want chimera", options.Adapter)
	}
}

func TestReadLearningDocumentUsesStdinByDefault(t *testing.T) {
	got, err := readLearningDocument("", false, strings.NewReader(`{"schema_version":1}`))
	if err != nil || string(got) != `{"schema_version":1}` {
		t.Fatalf("document=%q error=%v", got, err)
	}
}
