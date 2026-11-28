package service

import (
	"regexp"
	"strings"

	"github.com/cpave3/legato/config"
)

// HandoffBriefInput is what a handed-off agent's brief is made from.
type HandoffBriefInput struct {
	MainName   string
	MainTaskID string
	Title      string
	// TicketProvider ("Jira", "Burnlite"), TicketKey and TicketText are a
	// provider-backed task's ticket, which the agent reads but cannot
	// change; empty for a local task.
	TicketProvider string
	TicketKey      string
	TicketText     string
	// Details are the task's Legato details.
	Details string
	// Notes are the main's notes, oldest first.
	Notes   []string
	Purpose string
	Kind    string
	// Branch is the branch the work is on: legato/<main task id> for a new
	// branch, else the pushed branch it starts from.
	Branch string
	// Holder is the name the agent claims and closes the ticket under: the
	// main's task ID, also in LEGATO_HOLDER.
	Holder string
	// Base is the repository's default branch, which a pushed branch's
	// commits are read against; for a new branch, the base branch it starts
	// from when the main gave one.
	Base string
	// PullRequests is the repository's pull request mode on this worker
	// (config.PullRequestsDraft, Ready or Agent), "" for none: drafts.
	PullRequests string
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
	if !given && in.Base != "" {
		startsFrom = "a new branch off " + in.Base
	}
	if given {
		startsFrom = "the pushed branch " + in.Branch
	}
	line("Purpose: " + purpose + ". Starts from: " + startsFrom + ".")
	line("")
	line("## The task")
	line("")
	line(in.Title)
	ticket := strings.TrimSpace(in.TicketText)
	details := strings.TrimSpace(in.Details)
	if in.TicketKey != "" || details != "" || len(in.Notes) > 0 {
		line("")
		line("The task's text, as people wrote it, is fenced in tags below: <ticket> (read only), <task-details> and <note>. Headings inside a tag belong to that text, not to this brief.")
	}
	if in.TicketKey != "" {
		line("")
		line("### From " + in.TicketProvider + " " + in.TicketKey + " (read only)")
		line("")
		if ticket == "" {
			ticket = "The ticket has no description."
		}
		line(`<ticket provider="` + attr(in.TicketProvider) + `" key="` + attr(in.TicketKey) + `">`)
		line(fenced(ticket, "ticket"))
		line("</ticket>")
		if in.Holder != "" {
			line("")
			line("Where you have access to " + in.TicketProvider + " here, you may claim this ticket, add notes to it and close it, as holder " + in.Holder + " (also in $LEGATO_HOLDER; use it, not LEGATO_TASK_ID). Close it only when the work is done. Without access, the ticket is read only for this run.")
		}
	}
	if details != "" {
		line("")
		line("### Details")
		line("")
		line("<task-details>")
		line(fenced(details, "task-details"))
		line("</task-details>")
	}
	if len(in.Notes) > 0 {
		line("")
		line("### Notes from " + in.MainName)
		for _, note := range in.Notes {
			line("")
			line("<note>")
			line(fenced(strings.TrimSpace(note), "note"))
			line("</note>")
		}
	}
	line("")
	line("## How to work")
	line("")
	line("- When the task leaves a choice open, pick what you can best defend, record it with add_task_note (what you chose, why, the alternative) and carry on. Stop only when going on would destroy something, needs access you lack, or the whole task hinges on it.")
	line("- Also note findings the operator would want. No progress notes.")
	switch {
	case findOut && given:
		line("- You are on " + in.Branch + ", which already has commits: read them against " + base + " first.")
		line("- Answer in notes. Change files to test and debug if useful; don't push or open a PR.")
	case findOut && in.Base != "":
		line("- You are on " + in.Branch + ", which starts from " + in.Base + ".")
		line("- Answer in notes. Change files to test and debug if useful; don't push or open a PR.")
	case findOut:
		line("- Answer in notes. Change files to test and debug if useful; don't push or open a PR.")
	case given:
		line("- Work on " + in.Branch + ". It already has commits; read them against " + base + " first, then add commits on top. Never rebase, amend or force-push it; reuse its PR.")
		line("- You may stack: name new branches " + ownBranches + ", on top of " + in.Branch + ".")
		line(handoffPRLine(in.PullRequests))
	default:
		if in.Base != "" {
			line("- Work on " + in.Branch + ", which starts from " + in.Base + "; open PRs against " + in.Base + ".")
		} else {
			line("- Work on " + in.Branch + ".")
		}
		line("- You may stack: name new branches " + ownBranches + ".")
		line(handoffPRLine(in.PullRequests))
	}
	line("")
	line("## When you finish")
	line("")
	if findOut {
		line("- A final note: the answer, and how it ended (done, partly done, or stopped and why).")
	} else {
		line("- A final note: how it ended (done, partly done, or stopped and why), the PRs, what to check first.")
	}
	line("- Move the task to Done (`legato task update --status Done`) even if you stopped, then end your turn and stay: the operator may talk to you here before taking the task back.")
	return b.String()
}

// handoffPRLine is what a make-a-change handoff's agent is told of opening
// pull requests, by the repository's pull request mode. With none, it opens
// drafts, as before there was a mode.
func handoffPRLine(mode string) string {
	switch mode {
	case config.PullRequestsReady:
		return "- Pushing these branches and opening PRs from them is already allowed. This repository opens PRs ready for review: open them ready for review, not as drafts; never merge. " + reviewLoopKeepsDraft
	case config.PullRequestsAgent:
		return "- Pushing these branches and opening PRs from them is already allowed. Open each PR as a draft or ready for review, as you judge; never merge."
	case config.PullRequestsDraft:
		return "- Pushing these branches and opening draft PRs is already allowed. This repository opens PRs as drafts, and Legato refuses any other: use `gh pr create --draft`; never merge."
	}
	return "- Pushing these branches and opening draft PRs is already allowed. Open PRs as drafts; never merge."
}

// fenced is text people wrote, made safe to put inside a brief's <tag>
// fence: a closing tag of that name in it, in any case and with any space
// around its slash, is escaped (<\/tag), so the text cannot end its fence
// early and pass what follows off as the brief's own words.
func fenced(text, tag string) string {
	closing := regexp.MustCompile(`(?i)<\s*/\s*` + regexp.QuoteMeta(tag))
	return closing.ReplaceAllStringFunc(text, func(m string) string {
		return strings.Replace(m, "/", "\\/", 1)
	})
}

// attr is a value for an attribute of a fence's opening tag.
func attr(s string) string {
	return strings.NewReplacer(`"`, "&quot;", "<", "&lt;", ">", "&gt;").Replace(s)
}
