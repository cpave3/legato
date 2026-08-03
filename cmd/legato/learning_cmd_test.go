package main

import (
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
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

func TestLearningSearchDefaultsToCurrentRepositoryAcceptedStatusesAndRelevantTopTen(t *testing.T) {
	parsed := parsedArgs{Values: map[string]string{}, Present: map[string]bool{}}
	filter, err := learningSearchFilter(parsed, func() (service.ProjectIdentity, error) {
		return service.ProjectIdentity{Key: "repo:github.com/acme/widget"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if filter.ProjectKey != "repo:github.com/acme/widget" || strings.Join(filter.Statuses, ",") != "validated,incorporated" || filter.Limit != 10 || filter.MinScore != 0.55 {
		t.Fatalf("filter = %+v", filter)
	}
}

func TestLearningSearchAllowsExplicitCrossRepositoryAndStatusOverrides(t *testing.T) {
	parsed := parsedArgs{Values: map[string]string{"status": "candidate", "limit": "25", "min-score": "0.7"}, Present: map[string]bool{"status": true, "limit": true, "min-score": true, "all-projects": true}}
	filter, err := learningSearchFilter(parsed, func() (service.ProjectIdentity, error) {
		t.Fatal("repository resolution should be skipped")
		return service.ProjectIdentity{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if filter.ProjectKey != "" || strings.Join(filter.Statuses, ",") != "candidate" || filter.Limit != 25 || filter.MinScore != 0.7 {
		t.Fatalf("filter = %+v", filter)
	}
	parsed.Values["project"] = "repo:key"
	parsed.Present["project"] = true
	if _, err := learningSearchFilter(parsed, func() (service.ProjectIdentity, error) { return service.ProjectIdentity{}, nil }); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestLearningSearchHelpDocumentsSafeDefaultsAndOverrides(t *testing.T) {
	help := helpText([]string{"learn", "search"})
	for _, want := range []string{"--project", "current repository", "--all-projects", "--status", "validated and incorporated", "--min-score", "--limit", "default 10", "--category", "--tags"} {
		if !strings.Contains(help, want) {
			t.Errorf("search help missing %q:\n%s", want, help)
		}
	}
	search, _ := findCommand(commandTree(), []string{"learn", "search"})
	if !hasCommandFlag(search, "all-projects") || !hasCommandFlag(search, "min-score") {
		t.Fatalf("search flags = %#v", search.Flags)
	}
	list, _ := findCommand(commandTree(), []string{"learn", "list"})
	if hasCommandFlag(list, "all-projects") || hasCommandFlag(list, "min-score") {
		t.Fatalf("search-only flags leaked into list: %#v", list.Flags)
	}
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
