package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// brief is a reviewer's assignment: the pull requests at their pinned
// heads, the task, and on a later pass what earlier passes found. It wraps
// the configured skill and ends by asking for submit_review.
func (s *ReviewPassService) brief(ctx context.Context, h *store.Handoff, pass int, prs []store.HandoffReviewPR, settings ReviewPassSettings) (string, error) {
	task, err := s.db.GetTask(ctx, h.TaskID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Review pass %d of %s\n\n", pass, h.Repo)
	b.WriteString("You are the reviewer of a handed-off task's pull requests. ")
	if pass == 1 {
		b.WriteString("Review the whole stack, every pull request in full, each against its own base. ")
	} else {
		fmt.Fprintf(&b, "This is a later pass: review what changed since pass %d, and rule on the earlier blocking findings listed below. Earlier passes reviewed the rest; do not review it again. ", pass-1)
	}
	b.WriteString("You may only look: do not change, commit or push anything, and do not post to GitHub. Legato posts your review.\n\n")
	if s.opts.Overlay != nil && s.opts.Overlay() {
		b.WriteString("You can build and run the tests: what you write in the checkout is thrown away when you end.\n\n")
	} else {
		b.WriteString("The checkout is read-only: builds and tests that write to the tree will fail. Review by reading, and run only what writes elsewhere.\n\n")
	}
	b.WriteString("## Pull requests\n\n")
	for _, pr := range prs {
		fmt.Fprintf(&b, "### #%d %s\n\n- branch `%s`, base `%s`\n- pinned head `%s` (also at `%s`)\n\n", pr.Number, pr.Title, pr.Branch, pr.Base, pr.HeadSHA, pinRef(h.TaskID, pass, pr.Number))
		if body := strings.TrimSpace(pr.Body); body != "" {
			b.WriteString("<pr-description>\n" + body + "\n</pr-description>\n\n")
		}
	}
	b.WriteString("## The task\n\n<task-details>\n# " + task.Title + "\n\n" + strings.TrimSpace(task.DescriptionMD) + "\n</task-details>\n\n")
	if pass > 1 {
		if err := s.laterPassBrief(ctx, &b, h, pass); err != nil {
			return "", err
		}
	}
	b.WriteString(blockingBar)
	b.WriteString("## What to do\n\n")
	place := "Give each the pull request's number, a one-line title, a body, a `failure_scenario` when it is blocking, and a place (path, line, optional start_line, side RIGHT for the head or LEFT for a deleted line) when it is about particular lines."
	if pass == 1 {
		if settings.Skill != "" {
			fmt.Fprintf(&b, "1. Review these pull requests with the %s skill.\n", settings.Skill)
		} else {
			b.WriteString("1. Review these pull requests.\n")
		}
		b.WriteString("2. Turn each problem into a finding: `blocking` when it meets the bar above, `nit` otherwise. " + place + "\n")
		b.WriteString("3. Call the `submit_review` tool from the legato MCP server with your summary and findings. If it lists problems, fix them and call it again.\n")
	} else {
		fmt.Fprintf(&b, "1. Read what changed since pass %d: the diff from the heads listed above to the pinned heads.", pass-1)
		if settings.Skill != "" {
			fmt.Fprintf(&b, " If you use the %s skill, point it at that diff only.", settings.Skill)
		}
		b.WriteString("\n")
		b.WriteString("2. Rule on every earlier blocking finding listed above: `resolved` when it was fixed or you accept the author's answer, `open` with why when it was not. Accept an answer that a finding is below the bar, or is a follow-up outside this task, when that is so.\n")
		fmt.Fprintf(&b, "3. Turn each new problem into a finding. A new `blocking` finding must meet the bar above and be on what changed since pass %d: its place on a changed line. A problem elsewhere is a `nit`, a follow-up for another change, unless it is critical: then give `critical` with why it cannot wait. Legato refuses a new blocking finding that is neither. "+place+"\n", pass-1)
		b.WriteString("4. Call the `submit_review` tool from the legato MCP server with your summary, findings and rulings. If it lists problems, fix them and call it again.\n")
	}
	b.WriteString("\nIf you cannot review (the code does not build for reasons outside it, the pull requests are not what the task is), call `submit_review` with `unable` and the reason. Then end your turn.\n")
	return b.String(), nil
}

// laterPassBrief adds what earlier passes found: their pinned heads, for a
// diff since, and the blocking findings still open.
func (s *ReviewPassService) laterPassBrief(ctx context.Context, b *strings.Builder, h *store.Handoff, pass int) error {
	previous, err := s.db.ListHandoffReviewPRs(ctx, h.TaskID, pass-1)
	if err != nil {
		return err
	}
	b.WriteString("## Since the last pass\n\nThe last pass reviewed these heads; compare with `git range-diff` or `git diff` against them:\n\n")
	for _, pr := range previous {
		fmt.Fprintf(b, "- #%d at `%s` (`%s`)\n", pr.Number, pr.HeadSHA, pinRef(h.TaskID, pass-1, pr.Number))
	}
	if items, err := s.db.ListHandoffReviewItems(ctx, h.TaskID); err == nil {
		var comments []store.HandoffReviewItem
		for _, item := range items {
			if item.Kind == ItemComment && !item.Closed {
				comments = append(comments, item)
			}
		}
		if len(comments) > 0 {
			b.WriteString("\n## Comments from people and bots\n\nThe author settles these itself; give them no ruling. They are context:\n\n")
			for _, c := range comments {
				fmt.Fprintf(b, "- #%d %s\n  <comment>%s</comment>\n", c.PR, c.Title, c.Body)
				if c.Answer != "" {
					fmt.Fprintf(b, "  <author-answer>%s: %s</author-answer>\n", c.Answer, c.AnswerNote)
				}
			}
		}
	}
	open, err := s.openBlocking(ctx, h.TaskID, pass)
	if err != nil {
		return err
	}
	if len(open) > 0 {
		b.WriteString("\n## Earlier blocking findings to rule on\n\n")
		for _, f := range open {
			where := ""
			if f.Path != "" {
				where = fmt.Sprintf(" at `%s:%d`", f.Path, f.Line)
			}
			fmt.Fprintf(b, "- `%s` on #%d%s: %s\n", f.ID, f.PR, where, f.Title)
			if item, err := s.db.GetHandoffReviewItem(ctx, h.TaskID, f.ID); err == nil && item.Answer != "" {
				answer := item.Answer
				if item.AnswerNote != "" {
					answer += ": " + item.AnswerNote
				}
				fmt.Fprintf(b, "  <author-answer>%s</author-answer>\n", answer)
			}
		}
		b.WriteString("\n")
	}
	return nil
}

// blockingBar is what makes a finding blocking, for every pass.
const blockingBar = `## What is blocking

A finding is ` + "`blocking`" + ` only when it must be fixed before this merges, and you can say how it fails: its ` + "`failure_scenario`" + `, what happens and what goes wrong. Blocking is:

- a bug that ordinary use reaches, or that an agent or untrusted input can trigger;
- data lost or corrupted;
- a security or sandbox boundary an agent can cross;
- the task not done, or done otherwise than it asks;
- the build or the tests broken.

Not blocking (make these nits, or leave them out):

- A setting only the operator can choose: a problem they would meet only by configuring it so on purpose.
- Hardening beyond what the task asks, and races with no realistic order of events.
- Style, naming, and how you would have written it.

Prefer the smallest fix that would do, such as refusing a bad setting over machinery that copes with it. Name a pattern of edge cases once, in one finding, not each case in turn.

`
