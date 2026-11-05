package service

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateBriefs = flag.Bool("update", false, "rewrite the handed-off agents' briefs in testdata/handoff_brief")

// TestTheHandoffBriefSaysWhatTheSpecSays compares each form of a handed-off
// agent's brief with its golden file. Read a changed file as the agent will:
// go test ./internal/service -run TestTheHandoffBrief -update rewrites them.
func TestTheHandoffBriefSaysWhatTheSpecSays(t *testing.T) {
	task := HandoffBriefInput{
		MainName: "workstation", MainTaskID: "K7Q2M9XA", Title: "Fix the flaky login test",
		Details: "The login test fails about one run in ten on CI.\n\nIt started after the session cookie change.",
		Notes:   []string{"Seen on CI since Tuesday.", "Retrying the job usually passes.\nThe failure is always a timeout."},
		Kind:    HandoffNewBranch, Branch: "legato/k7q2m9xa",
	}
	// Started from a branch the person pushed: it has commits already.
	pushed := task
	pushed.Kind, pushed.Branch, pushed.Base = HandoffPushedBranch, "fix/login-timeout", "main"
	// A Jira-backed task: Jira's text, read only, then the Legato details.
	jira, jiraPushed := task, pushed
	for _, in := range []*HandoffBriefInput{&jira, &jiraPushed} {
		in.MainTaskID, in.Title = "ACME-12", "Checkout times out on large carts"
		in.JiraKey, in.JiraText = "ACME-12", "Carts over 50 items time out at checkout.\n\nAcceptance: checkout under 2s."
		in.Details = "Start with the cart serializer."
	}
	jira.Branch = "legato/acme-12"
	for name, form := range map[string]struct {
		in      HandoffBriefInput
		purpose string
	}{
		"make-change-new-branch-local.md":    {task, HandoffMakeChange},
		"find-out-new-branch-local.md":       {task, HandoffFindOut},
		"make-change-pushed-branch-local.md": {pushed, HandoffMakeChange},
		"find-out-pushed-branch-local.md":    {pushed, HandoffFindOut},
		"make-change-new-branch-jira.md":     {jira, HandoffMakeChange},
		"find-out-new-branch-jira.md":        {jira, HandoffFindOut},
		"make-change-pushed-branch-jira.md":  {jiraPushed, HandoffMakeChange},
		"find-out-pushed-branch-jira.md":     {jiraPushed, HandoffFindOut},
	} {
		t.Run(name, func(t *testing.T) {
			in := form.in
			in.Purpose = form.purpose
			got := HandoffBrief(in)
			path := filepath.Join("testdata", "handoff_brief", name)
			if *updateBriefs {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update to write it)", err)
			}
			if got != string(want) {
				t.Fatalf("the brief differs from %s (run with -update to rewrite it):\n%s", path, got)
			}
		})
	}
}
