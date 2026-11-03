package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/service"
)

// fakeST puts an st on a PATH of its own that runs script for every call.
func fakeST(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "st"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestStacksAreSupportedOnlyWhenSTCanImportAGraph(t *testing.T) {
	t.Run("st with graph import", func(t *testing.T) {
		fakeST(t, `[ "$1 $2 $3" = "graph import --help" ] && exit 0; exit 1`)
		if !cli.StackSupport(context.Background()) {
			t.Error("an st that imports graphs was not supported")
		}
	})
	t.Run("an older st", func(t *testing.T) {
		fakeST(t, `echo 'unknown command "import" for "st graph"' >&2; exit 1`)
		if cli.StackSupport(context.Background()) {
			t.Error("an st without graph import was supported")
		}
	})
	t.Run("no st", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if cli.StackSupport(context.Background()) {
			t.Error("stacks were supported with no st")
		}
	})
	t.Run("an st that hangs", func(t *testing.T) {
		fakeST(t, `exec sleep 30`)
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		start := time.Now()
		if cli.StackSupport(ctx) {
			t.Error("an st that never answered was supported")
		}
		if waited := time.Since(start); waited > 5*time.Second {
			t.Errorf("waited %s for st", waited)
		}
	})
}

var githubSummary = service.CredentialSummary{Name: "github", Profile: "github", Channels: []string{"http-proxy"},
	Env: []string{"LEGATO_GITHUB_TOKEN", "LEGATO_GITHUB_API_URL"}, Git: "https://github.com", Repositories: []string{"cpave3/legato"}}

func TestWithStacksAgentsAreToldHowToPushAWholeStack(t *testing.T) {
	text := cli.Guidance([]service.CredentialSummary{githubSummary}, nil, true)
	for _, want := range []string{
		"st sync", "legato/$LEGATO_TASK_ID-", "failed_pushes", "pending ap-", "[legato approvals]", "st pr",
		"github:cpave3/legato@refs/heads/legato/$LEGATO_TASK_ID-*",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the stack guidance does not say %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "not a pattern") {
		t.Errorf("the guidance still says to ask for one branch only:\n%s", text)
	}
}

func TestWithoutStacksTheGuidanceIsAsBefore(t *testing.T) {
	text := cli.Guidance([]service.CredentialSummary{githubSummary}, nil, false)
	if text != cli.CredentialGuidance([]service.CredentialSummary{githubSummary}) {
		t.Errorf("the guidance changed without stacks:\n%s", text)
	}
	if strings.Contains(text, "st sync") || strings.Contains(text, "Staccato") {
		t.Errorf("Staccato was mentioned without stacks:\n%s", text)
	}
}

func TestStacksWithoutAGitHubCredentialAskForNoGrant(t *testing.T) {
	text := cli.Guidance(nil, nil, true)
	if !strings.Contains(text, "st sync") {
		t.Fatalf("no stack guidance:\n%s", text)
	}
	if strings.Contains(text, "request_access") {
		t.Errorf("the agent was told to ask for a grant with no GitHub credential:\n%s", text)
	}
}

func TestTheMCPServerTellsAgentsAboutStacksWhenSTCanImport(t *testing.T) {
	with := cli.MCPServerOptions(cli.MCPOptions{TaskID: "t1", Stacks: true}).Instructions
	if !strings.Contains(with, "st sync") {
		t.Errorf("the server's instructions say nothing about stacks:\n%s", with)
	}
	without := cli.MCPServerOptions(cli.MCPOptions{TaskID: "t1"}).Instructions
	if strings.Contains(without, "st sync") {
		t.Errorf("the server spoke of stacks without st:\n%s", without)
	}
}

func TestTheGitHubGuidanceSaysHowToUseActions(t *testing.T) {
	text := cli.CredentialGuidance([]service.CredentialSummary{githubSummary})
	for _, want := range []string{"gh run list, view, watch and download", "gh run rerun", "gh workflow run", "actions.write"} {
		if !strings.Contains(text, want) {
			t.Errorf("the GitHub guidance does not say %q:\n%s", want, text)
		}
	}
}
