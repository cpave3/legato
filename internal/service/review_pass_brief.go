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
	b.WriteString("You are the reviewer of a handed-off task's pull requests. Review the whole stack, every pull request in full, each against its own base. You may only look: do not change, commit or push anything, and do not post to GitHub. Legato posts your review.\n\n")
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
	b.WriteString("## What to do\n\n")
	if settings.Skill != "" {
		fmt.Fprintf(&b, "1. Review these pull requests with the %s skill.\n", settings.Skill)
	} else {
		b.WriteString("1. Review these pull requests.\n")
	}
	b.WriteString("2. Turn each problem into a finding: `blocking` when it must be fixed before merging, `nit` when it is optional. Give each the pull request's number, a one-line title, a body, and a place (path, line, optional start_line, side RIGHT for the head or LEFT for a deleted line) when it is about particular lines.\n")
	if pass > 1 {
		b.WriteString("3. Rule on every earlier blocking finding listed above: `resolved` when it was fixed or you accept the author's answer, `open` with why when it was not.\n4. Call the `submit_review` tool from the legato MCP server with your summary, findings and rulings. If it lists problems, fix them and call it again.\n")
	} else {
		b.WriteString("3. Call the `submit_review` tool from the legato MCP server with your summary and findings. If it lists problems, fix them and call it again.\n")
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
		}
		b.WriteString("\n")
	}
	return nil
}
