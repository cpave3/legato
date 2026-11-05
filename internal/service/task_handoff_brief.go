package service

import (
	"strings"
)

// HandoffBriefInput is what a handed-off agent's brief is made from.
type HandoffBriefInput struct {
	MainName   string
	MainTaskID string
	Title      string
	Details    string
	// Notes are the main's notes, oldest first.
	Notes   []string
	Purpose string
	Kind    string
	// Branch is the branch the work is on: legato/<main task id> for a new
	// branch, else the pushed branch it starts from.
	Branch string
	// Base is the repository's default branch, which a pushed branch's
	// commits are read against.
	Base string
}

// HandoffBrief is the assignment a worker gives a handed-off task's agent:
// it works alone, records its decisions in notes, ends in Done with an
// outcome note, and, for a make-a-change handoff, pushes and opens draft PRs
// only on its own branches. The text was settled in Burnlite LEG-7; the
// golden files in testdata/handoff_brief show each form.
func HandoffBrief(in HandoffBriefInput) string {
	var b strings.Builder
	line := func(s string) { b.WriteString(s + "\n") }
	findOut := in.Purpose == HandoffFindOut
	ownBranches := "legato/" + strings.ToLower(in.MainTaskID) + "-<a few words>"
	given := in.Kind == HandoffPushedBranch
	base := in.Base
	if base == "" {
		base = "the default branch"
	}

	line("# Handed-off task")
	line("")
	line(in.MainName + " handed this task to this Legato. Nobody is watching: work on your own until it's finished.")
	purpose := "make a change"
	if findOut {
		purpose = "find out"
	}
	startsFrom := "a new branch"
	if given {
		startsFrom = "the pushed branch " + in.Branch
	}
	line("Purpose: " + purpose + ". Starts from: " + startsFrom + ".")
	line("")
	line("## The task")
	line("")
	line(in.Title)
	if details := strings.TrimSpace(in.Details); details != "" {
		line("")
		line("### Details")
		line("")
		line(details)
	}
	if len(in.Notes) > 0 {
		line("")
		line("### Notes from " + in.MainName)
		for i, note := range in.Notes {
			if i > 0 {
				line("")
				line("---")
			}
			line("")
			line(strings.TrimSpace(note))
		}
	}
	line("")
	line("## How to work")
	line("")
	line("- When the task leaves a choice open, pick what you can best defend, record it with add_task_note (what you chose, why, the alternative) and carry on. Stop only when going on would destroy something, needs access you lack, or the whole task hinges on it.")
	line("- Also note findings the person would want. No progress notes.")
	switch {
	case findOut && given:
		line("- You are on " + in.Branch + ", which already has commits: read them against " + base + " first.")
		line("- Answer in notes. Change files to test and debug if useful; don't push or open a PR.")
	case findOut:
		line("- Answer in notes. Change files to test and debug if useful; don't push or open a PR.")
	case given:
		line("- Work on " + in.Branch + ". It already has commits; read them against " + base + " first, then add commits on top. Never rebase, amend or force-push it; reuse its PR.")
		line("- You may stack: name new branches " + ownBranches + ", on top of " + in.Branch + ".")
		line("- Pushing these branches and opening draft PRs is already allowed. Open PRs as drafts; never merge.")
	default:
		line("- Work on " + in.Branch + ".")
		line("- You may stack: name new branches " + ownBranches + ".")
		line("- Pushing these branches and opening draft PRs is already allowed. Open PRs as drafts; never merge.")
	}
	line("")
	line("## When you finish")
	line("")
	if findOut {
		line("- A final note: the answer, and how it ended (done, partly done, or stopped and why).")
	} else {
		line("- A final note: how it ended (done, partly done, or stopped and why), the PRs, what to check first.")
	}
	line("- Move the task to Done (`legato task update --status Done`) even if you stopped, then end your turn and stay: the person may talk to you here before taking the task back.")
	return b.String()
}
