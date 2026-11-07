package service

import (
	"context"
	"fmt"
	"strings"
)

// HandoffReviewBlock is what a summary row says of a handoff's review: the
// pass, its phase (reviewing, waiting_for_reviewer, waiting_for_author,
// clean, stuck), the items open, the comments skipped, why it is stuck,
// and each pull request's latest Legato review. A main that predates it
// ignores it.
type HandoffReviewBlock struct {
	Pass            int          `json:"pass"`
	Phase           string       `json:"phase"`
	BlockingOpen    int          `json:"blocking_open"`
	CommentsOpen    int          `json:"comments_open"`
	ChecksOpen      int          `json:"checks_open"`
	SkippedComments int          `json:"skipped_comments"`
	Reason          string       `json:"reason,omitempty"`
	Links           []ReviewLink `json:"links,omitempty"`
}

// ReviewLink is a pull request's latest review by Legato.
type ReviewLink struct {
	PR  int    `json:"pr"`
	URL string `json:"url"`
}

// InReview says the review is under way: a pass runs, waits for a
// reviewer, or waits for the author.
func (b *HandoffReviewBlock) InReview() bool {
	return b != nil && (b.Phase == ReviewPhaseReviewing || b.Phase == ReviewPhaseWaitingForReviewer || b.Phase == ReviewPhaseWaitingForAuthor)
}

// Stuck says the review needs the person.
func (b *HandoffReviewBlock) Stuck() bool { return b != nil && b.Phase == ReviewPhaseStuck }

// Summary is the block in a few words: "Reviewing · pass 2", "Author fixing
// · 3 blocking, 1 comment", "Done · approved".
func (b *HandoffReviewBlock) Summary() string {
	if b == nil {
		return ""
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
		return "Done · approved"
	case ReviewPhaseStuck:
		return "Review stuck: " + b.Reason
	}
	return ""
}

// ReviewBlock is a card's review as its summary row says it; nil when it
// has none (yet, or no pull request to review).
func (s *ReviewPassService) ReviewBlock(ctx context.Context, taskID string) *HandoffReviewBlock {
	loop, err := s.db.GetHandoffReview(ctx, taskID)
	if err != nil || loop == nil {
		return nil
	}
	block := &HandoffReviewBlock{Pass: loop.Pass, Phase: loop.Phase, Reason: loop.Reason}
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
	block.SkippedComments = s.skippedComments(ctx, taskID)
	for pass := loop.Pass; pass >= 1 && len(block.Links) == 0; pass-- {
		prs, _ := s.db.ListHandoffReviewPRs(ctx, taskID, pass)
		for _, pr := range prs {
			if pr.ReviewURL != "" {
				block.Links = append(block.Links, ReviewLink{PR: pr.Number, URL: pr.ReviewURL})
			}
		}
	}
	return block
}

// skippedComments counts the comments on the handoff's pull requests that
// do not count as feedback (LEG-72 fills it in).
func (s *ReviewPassService) skippedComments(context.Context, string) int { return 0 }

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
func reviewNoteText(block *HandoffReviewBlock, open []string) string {
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
func ReviewBlockNote(block *HandoffReviewBlock) string {
	if block == nil {
		return ""
	}
	return "Last heard of its review: " + reviewNoteText(block, nil)
}
