package service

import (
	"context"
	"fmt"
	"strings"
)

// ReviewLoopBlock is a review loop in brief, as a handoff's summary row and a
// run's review step say it: the
// pass, its phase (reviewing, waiting_for_reviewer, waiting_for_author,
// clean, stuck), the items open, the comments skipped, why it is stuck,
// and each pull request's latest Legato review. A main that predates it
// ignores it.
type ReviewLoopBlock struct {
	Pass         int    `json:"pass"`
	Phase        string `json:"phase"`
	BlockingOpen int    `json:"blocking_open"`
	CommentsOpen int    `json:"comments_open"`
	ChecksOpen   int    `json:"checks_open"`
	// ChecksPending are counted checks not finished on the reviewed
	// heads; ChecksReplied failing ones the author answered by a reply.
	ChecksPending   int          `json:"checks_pending,omitempty"`
	ChecksReplied   int          `json:"checks_replied,omitempty"`
	SkippedComments int          `json:"skipped_comments"`
	Reason          string       `json:"reason,omitempty"`
	Links           []ReviewLink `json:"links,omitempty"`
	// WaitingOn is what a pass that has not started waits on
	// (ReviewWaitingOn*): the author, GitHub, a reviewer slot. Phase then
	// stays reviewing or waiting_for_reviewer, as a main that predates the
	// field reads it.
	WaitingOn string `json:"waiting_on,omitempty"`
}

// ReviewLink is a pull request's latest review by Legato.
type ReviewLink struct {
	PR  int    `json:"pr"`
	URL string `json:"url"`
}

// InReview says the review is under way: a pass runs, waits for a
// reviewer, waits for the author, or is clean and waits for checks.
func (b *ReviewLoopBlock) InReview() bool {
	return b != nil && (b.Phase == ReviewPhaseReviewing || b.Phase == ReviewPhaseWaitingForReviewer || b.Phase == ReviewPhaseWaitingForAuthor ||
		(b.Phase == ReviewPhaseClean && b.ChecksPending > 0))
}

// Stuck says the review needs the person.
func (b *ReviewLoopBlock) Stuck() bool { return b != nil && b.Phase == ReviewPhaseStuck }

// Summary is the block in a few words: "Reviewing · pass 2", "Waiting for
// the author · working", "Author fixing · 3 blocking, 1 comment", "Done ·
// approved".
func (b *ReviewLoopBlock) Summary() string {
	if b == nil {
		return ""
	}
	switch b.WaitingOn {
	case ReviewWaitingOnAuthorWorking:
		return "Waiting for the author · working"
	case ReviewWaitingOnAuthorWaiting:
		return "Waiting for the author · it waits for an answer"
	case ReviewWaitingOnAuthorPush:
		return "Waiting for the author to push"
	case ReviewWaitingOnGitHub:
		return "Waiting for GitHub"
	case ReviewWaitingOnStart:
		return "Starting the next pass"
	}
	switch b.Phase {
	case ReviewPhaseReviewing:
		return fmt.Sprintf("Reviewing · pass %d", b.Pass)
	case ReviewPhaseWaitingForReviewer:
		return fmt.Sprintf("Waiting for a reviewer · pass %d", b.Pass+1)
	case ReviewPhaseWaitingForAuthor:
		var parts []string
		for _, c := range []struct {
			n         int
			one, many string
		}{{b.BlockingOpen, "blocking", "blocking"}, {b.CommentsOpen, "comment", "comments"}, {b.ChecksOpen, "check", "checks"}} {
			if c.n > 0 {
				word := c.many
				if c.n == 1 {
					word = c.one
				}
				parts = append(parts, fmt.Sprintf("%d %s", c.n, word))
			}
		}
		if len(parts) == 0 {
			return "Author fixing"
		}
		return "Author fixing · " + strings.Join(parts, ", ")
	case ReviewPhaseClean:
		summary := "Done · approved"
		if b.ChecksPending > 0 {
			summary = fmt.Sprintf("Approved · waiting for %d %s", b.ChecksPending, plural(b.ChecksPending, "check", "checks"))
		}
		if b.ChecksReplied > 0 {
			summary += fmt.Sprintf(" · %d %s answered by reply", b.ChecksReplied, plural(b.ChecksReplied, "check", "checks"))
		}
		return summary
	case ReviewPhaseStuck:
		return "Review stuck: " + b.Reason
	}
	return ""
}

// ReviewBlock is a card's review as its summary row says it; nil when it
// has none (yet, or no pull request to review).
func (s *ReviewPassService) ReviewBlock(ctx context.Context, taskID string) *ReviewLoopBlock {
	loop, err := s.db.GetHandoffReview(ctx, taskID)
	if err != nil || loop == nil {
		return nil
	}
	block := &ReviewLoopBlock{Pass: loop.Pass, Phase: loop.Phase, Reason: loop.Reason}
	switch loop.Phase {
	case ReviewPhaseAuthor, ReviewPhaseSkipped:
		if loop.Pass == 0 {
			return nil
		}
		block.Phase = ReviewPhaseWaitingForAuthor
	case ReviewPhaseReady:
		block.Phase = ReviewPhaseReviewing
		if loop.Pass == 0 {
			block.Pass = 1
		}
		block.WaitingOn = s.waitingOn(taskID)
		if loop.Mismatches > 0 {
			block.WaitingOn = ReviewWaitingOnAuthorPush
		}
		if block.WaitingOn == "" {
			block.WaitingOn = ReviewWaitingOnStart
		}
	case ReviewPhaseWaitingForReviewer:
		block.WaitingOn = ReviewWaitingOnReviewerSlot
	}
	if items, err := currentItems(ctx, s.db, loop); err == nil {
		for _, item := range items {
			if item.Answer != "" {
				continue
			}
			switch item.Kind {
			case ItemBlocking:
				block.BlockingOpen++
			case ItemComment:
				block.CommentsOpen++
			case ItemCheck:
				block.ChecksOpen++
			}
		}
	}
	block.SkippedComments, block.ChecksPending = loop.SkippedComments, loop.ChecksPending
	if all, err := s.db.ListHandoffReviewItems(ctx, taskID); err == nil {
		for _, item := range all {
			if item.Kind == ItemCheck && !item.Closed && item.Answer == AnswerReplied {
				block.ChecksReplied++
			}
		}
	}
	block.Links = s.latestReviews(ctx, taskID, loop.Pass)
	return block
}

// latestReviews are the latest Legato review of each pull request of a
// loop's last pass with one, in its order: a pass that posted nothing on a
// pull request (an approval kept) links the earlier review.
func (s *ReviewPassService) latestReviews(ctx context.Context, taskID string, last int) []ReviewLink {
	var order []int
	urls := map[int]string{}
	for pass := last; pass >= 1; pass-- {
		prs, _ := s.db.ListHandoffReviewPRs(ctx, taskID, pass)
		if order == nil {
			for _, pr := range prs {
				if pr.ReviewURL != "" || pr.Event == ReviewKept {
					order = append(order, pr.Number)
				}
			}
			if len(order) == 0 {
				continue
			}
		}
		for _, pr := range prs {
			if _, ok := urls[pr.Number]; !ok && pr.ReviewURL != "" {
				urls[pr.Number] = pr.ReviewURL
			}
		}
	}
	var out []ReviewLink
	for _, n := range order {
		if url := urls[n]; url != "" {
			out = append(out, ReviewLink{PR: n, URL: url})
		}
	}
	return out
}

// StopReview stops a card's reviewer: the handoff is closed (Drop, Leave,
// unpair) and its loop ends with it, posting nothing more.
func (s *ReviewPassService) StopReview(ctx context.Context, taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.opts.Agents.KillAgentInSlot(ctx, taskID, SlotReviewer)
}

// ReviewNote sums up a card's passes and open items for the notes a Return
// or Recall brings back; "" when it had no pass.
func (s *ReviewPassService) ReviewNote(ctx context.Context, taskID string) string {
	block := s.ReviewBlock(ctx, taskID)
	if block == nil {
		return ""
	}
	return reviewNoteText(block, s.openItemTitles(ctx, taskID))
}

func (s *ReviewPassService) openItemTitles(ctx context.Context, taskID string) []string {
	loop, err := s.db.GetHandoffReview(ctx, taskID)
	if err != nil || loop == nil {
		return nil
	}
	items, _ := currentItems(ctx, s.db, loop)
	var out []string
	for _, item := range items {
		if item.Answer == "" && item.Kind != ItemNit {
			out = append(out, fmt.Sprintf("%s on #%d: %s", item.Kind, item.PR, item.Title))
		}
	}
	return out
}

// reviewNoteText is a review block as a note.
func reviewNoteText(block *ReviewLoopBlock, open []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Review passes: %d pass(es); %s.", block.Pass, strings.TrimSuffix(block.Summary(), "."))
	for _, link := range block.Links {
		fmt.Fprintf(&b, "\n- Legato's review of #%d: %s", link.PR, link.URL)
	}
	if len(open) > 0 {
		b.WriteString("\n\nOpen items:")
		for _, item := range open {
			b.WriteString("\n- " + item)
		}
	}
	return b.String()
}

// ReviewBlockNote is a review block as the note a Take back leaves, from
// what the main last heard.
func ReviewBlockNote(block *ReviewLoopBlock) string {
	if block == nil {
		return ""
	}
	return "Last heard of its review: " + reviewNoteText(block, nil)
}
