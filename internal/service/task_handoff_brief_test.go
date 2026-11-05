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
	for name, purpose := range map[string]string{
		"make-change-new-branch-local.md": HandoffMakeChange,
		"find-out-new-branch-local.md":    HandoffFindOut,
	} {
		t.Run(name, func(t *testing.T) {
			in := task
			in.Purpose = purpose
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
