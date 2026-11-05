package cli_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

var granted = service.SessionBranches{Base: "legato/k7q2", Granted: true}

// A make-a-change handoff's session names its branches after the main's
// task, and is told its pushes and draft pull requests are already allowed.
func TestAHandedOffSessionIsToldItsBranchesAreAlreadyAllowed(t *testing.T) {
	for _, stacks := range []bool{false, true} {
		text := cli.GuidanceFor([]service.CredentialSummary{githubSummary}, nil, stacks, granted)
		for _, want := range []string{"legato/k7q2-<a few words>", "Work on legato/k7q2", "already allowed", "gh pr create --draft", "never merge"} {
			if !strings.Contains(text, want) {
				t.Errorf("stacks %v: the guidance does not say %q:\n%s", stacks, want, text)
			}
		}
		for _, unwanted := range []string{"$LEGATO_TASK_ID", "Before your first push, ask"} {
			if strings.Contains(text, unwanted) {
				t.Errorf("stacks %v: the guidance says %q:\n%s", stacks, unwanted, text)
			}
		}
	}
}

// A find-out handoff's session names branches after the main's task too,
// but is granted nothing, so it is told to ask as any session is.
func TestAFindOutSessionNamesTheMainTasksBranchesAndAsksForPushes(t *testing.T) {
	text := cli.GuidanceFor([]service.CredentialSummary{githubSummary}, nil, true, service.SessionBranches{Base: "legato/k7q2"})
	for _, want := range []string{"legato/k7q2-<a few words>", "github:cpave3/legato@refs/heads/legato/k7q2-*"} {
		if !strings.Contains(text, want) {
			t.Errorf("the guidance does not say %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "$LEGATO_TASK_ID") || strings.Contains(text, "already allowed") {
		t.Errorf("the guidance:\n%s", text)
	}
}

func TestAnOrdinarySessionsGuidanceIsUnchanged(t *testing.T) {
	for _, stacks := range []bool{false, true} {
		text := cli.GuidanceFor([]service.CredentialSummary{githubSummary}, nil, stacks, service.SessionBranches{})
		if text != cli.Guidance([]service.CredentialSummary{githubSummary}, nil, stacks) {
			t.Errorf("stacks %v: an ordinary session's guidance differs:\n%s", stacks, text)
		}
		if !strings.Contains(text, "legato/$LEGATO_TASK_ID-<a few words>") || strings.Contains(text, "already allowed") {
			t.Errorf("stacks %v: an ordinary session's guidance:\n%s", stacks, text)
		}
	}
}

// The MCP server of a handed-off task's session reads its handoff from the
// store, so Claude Code's agent is told the same.
func TestTheMCPServerOfAHandedOffTaskNamesTheMainTasksBranches(t *testing.T) {
	ctx := context.Background()
	db, err := store.New(filepath.Join(t.TempDir(), "worker.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := "2026-11-05T09:00:00Z"
	task := store.Task{ID: "wtask1", Title: "Fix", Status: "Doing", CreatedAt: now, UpdatedAt: now}
	h := store.Handoff{MainInstanceID: "srv-main", HandoffID: "hf-1", MainName: "workstation", MainTaskID: "K7Q2", TaskID: "wtask1",
		Purpose: service.HandoffMakeChange, Kind: service.HandoffNewBranch, Repo: "cpave3/legato", Branch: "legato/k7q2",
		State: store.HandoffPending, RequestHash: "x", CreatedAt: now, UpdatedAt: now}
	if err := db.CreateHandoffCard(ctx, task, nil, h); err != nil {
		t.Fatal(err)
	}
	credentials := &namedCredentials{list: []service.CredentialSummary{githubSummary}}
	handed := cli.MCPServerOptions(cli.MCPOptions{Store: db, TaskID: "wtask1", Stacks: true, Credentials: credentials}).Instructions
	if !strings.Contains(handed, "Name every branch legato/k7q2-<a few words>") || !strings.Contains(handed, "already allowed") || strings.Contains(handed, "$LEGATO_TASK_ID") {
		t.Errorf("a handed-off session's instructions:\n%s", handed)
	}
	ordinary := cli.MCPServerOptions(cli.MCPOptions{Store: db, TaskID: "other", Stacks: true, Credentials: credentials}).Instructions
	if !strings.Contains(ordinary, "legato/$LEGATO_TASK_ID-<a few words>") {
		t.Errorf("an ordinary session's instructions:\n%s", ordinary)
	}
}

// A handoff from a pushed branch: the session works on that branch, only
// adds commits to it, and stacks new branches on top of it; pushing it is
// allowed with its own.
func TestASessionStartedFromAPushedBranchWorksOnIt(t *testing.T) {
	given := service.SessionBranches{Base: "legato/k7q2", Given: "fix/login-timeout", Granted: true}
	for _, stacks := range []bool{false, true} {
		text := cli.GuidanceFor([]service.CredentialSummary{githubSummary}, nil, stacks, given)
		for _, want := range []string{"Work on fix/login-timeout", "never rebase, amend or force-push it", "legato/k7q2-<a few words>",
			"Pushing fix/login-timeout, legato/k7q2 and branches named legato/k7q2-<a few words>"} {
			if !strings.Contains(text, want) {
				t.Errorf("stacks %v: the guidance does not say %q:\n%s", stacks, want, text)
			}
		}
		if stacks && !strings.Contains(text, "on top of fix/login-timeout") {
			t.Errorf("the stack guidance does not stack on the given branch:\n%s", text)
		}
	}
	// A find-out session is granted nothing: it asks, and still works on the
	// given branch.
	text := cli.GuidanceFor([]service.CredentialSummary{githubSummary}, nil, true, service.SessionBranches{Base: "legato/k7q2", Given: "fix/login-timeout"})
	if !strings.Contains(text, "Work on fix/login-timeout") || strings.Contains(text, "already allowed") {
		t.Errorf("a find-out session's guidance:\n%s", text)
	}
}
