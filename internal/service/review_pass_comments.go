package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// countedAssociations are the authors whose comments count as feedback,
// with the bots of review_passes.counted_bots.
var countedAssociations = map[string]bool{"OWNER": true, "MEMBER": true, "COLLABORATOR": true}

// storePollPages keeps a pull request's polled pages in the store.
type storePollPages struct {
	db     *store.Store
	taskID string
	pr     int
}

func (p storePollPages) PollPage(ctx context.Context, list string, page int) (string, string, bool) {
	return p.db.HandoffReviewPollPage(ctx, p.taskID, p.pr, list, page)
}

func (p storePollPages) SavePollPage(ctx context.Context, list string, page int, etag, body string) error {
	return p.db.SaveHandoffReviewPollPage(ctx, p.taskID, p.pr, list, page, etag, body)
}

// pollComments polls a handoff's pull requests for comments when it is
// due, and turns the counted ones into items: new, edited (a new version
// to handle again), resolved by a person (handled), outdated (marked) or
// deleted (dropped, the author told when it had it). Call with s.mu held.
func (s *ReviewPassService) pollComments(ctx context.Context, h *store.Handoff) {
	cfg := s.config()
	loop, err := s.db.GetHandoffReview(ctx, h.TaskID)
	if err != nil {
		return
	}
	if loop == nil {
		loop = &store.HandoffReview{TaskID: h.TaskID, Settings: "{}"}
	}
	interval := cfg.PollInterval
	if interval <= 0 {
		interval = time.Minute
	}
	if polled, err := time.Parse(time.RFC3339, loop.PolledAt); err == nil && s.opts.Now().Before(polled.Add(interval)) {
		return
	}
	prs, err := s.pollTargets(ctx, h, loop)
	if err != nil || len(prs) == 0 {
		return
	}
	items, err := s.db.ListHandoffReviewItems(ctx, h.TaskID)
	if err != nil {
		return
	}
	byID := map[string]store.HandoffReviewItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	seen := map[string]bool{}
	skipped, fresh := 0, 0
	var gone []string
	for _, pr := range prs {
		poll, err := s.opts.GitHub.PollComments(ctx, pr.Repo, pr.Number, storePollPages{db: s.db, taskID: h.TaskID, pr: pr.Number})
		if err != nil {
			var gh *GitHubError
			if errors.As(err, &gh) && gh.RateLimited {
				loop.PolledAt = gh.RetryAt.UTC().Add(-interval).Format(time.RFC3339)
				_ = s.save(ctx, loop)
				return
			}
			log.Printf("review passes: polling %s#%d: %v", pr.Repo, pr.Number, err)
			continue
		}
		legato := map[string]bool{}
		if pr.Author != "" {
			legato[strings.ToLower(pr.Author)] = true
		}
		for _, c := range poll.Comments {
			if c.Kind == "review" && strings.Contains(c.Body, "<!-- legato-review-pass ") {
				legato[strings.ToLower(c.Author)] = true
			}
		}
		for _, c := range poll.Comments {
			if legato[strings.ToLower(c.Author)] || (c.Kind == "review" && (strings.TrimSpace(c.Body) == "" || c.State == "PENDING")) {
				continue
			}
			if !countedAssociations[c.Association] && !s.countedBot(c.Author) {
				skipped++
				continue
			}
			id := commentItemID(c)
			seen[id] = true
			item, known := byID[id]
			if !known {
				item = store.HandoffReviewItem{ID: id, TaskID: h.TaskID, Kind: ItemComment, Pass: loop.Pass, Repo: pr.Repo, PR: pr.Number,
					Version: 1, CreatedAt: s.now(), SourceID: fmt.Sprint(c.ID)}
				fresh++
			} else if item.Body != c.Body && !item.Closed {
				// An edit is a new version, to be handled again.
				item.Version++
				item.Answer, item.AnswerNote, item.AnsweredAt, item.Posted, item.Delivered = "", "", "", false, false
				item.Pass = loop.Pass
				fresh++
			}
			item.Author, item.Title, item.Body, item.URL = c.Author, commentTitle(c), c.Body, c.URL
			item.Path, item.Line, item.Outdated = c.Path, c.Line, c.Outdated
			if c.Kind == "inline" {
				item.ThreadCommentID = c.ThreadRoot
			}
			if c.Resolved && item.Answer == "" {
				// A person resolved the thread: handled.
				item.Answer, item.AnsweredAt, item.Posted = "resolved", s.now(), true
			}
			if err := s.db.UpsertHandoffReviewItem(ctx, item); err != nil {
				log.Printf("review passes: %v", err)
			}
		}
	}
	for _, item := range items {
		if item.Kind == ItemComment && !item.Closed && !seen[item.ID] {
			item.Closed = true
			_ = s.db.UpsertHandoffReviewItem(ctx, item)
			if item.Delivered && item.Answer == "" {
				gone = append(gone, item.ID)
			}
		}
	}
	loop.PolledAt, loop.SkippedComments = s.now(), skipped
	if fresh > 0 {
		loop.LastNewAt = s.now()
		if loop.Phase == ReviewPhaseClean {
			// A comment after a clean pass: the handoff is not done.
			loop.Phase = ReviewPhaseWaitingForAuthor
			if err := s.opts.Board.MoveCard(ctx, h.TaskID, cfg.WorkColumn); err != nil {
				log.Printf("review passes: moving %s to %s: %v", h.TaskID, cfg.WorkColumn, err)
			}
		}
	}
	if len(gone) > 0 {
		_ = s.opts.Agents.QueueMessage(ctx, h.TaskID, fmt.Sprintf("%s %s was deleted on GitHub; you need not answer it.", ReviewTag, strings.Join(gone, ", ")))
	}
	if err := s.saveChanged(ctx, loop); err != nil {
		log.Printf("review passes: %v", err)
	}
}

// pollTargets are the pull requests to poll: those of the last pass, else
// the handoff's open ones.
func (s *ReviewPassService) pollTargets(ctx context.Context, h *store.Handoff, loop *store.HandoffReview) ([]store.HandoffReviewPR, error) {
	if loop.Pass > 0 {
		return s.db.ListHandoffReviewPRs(ctx, h.TaskID, loop.Pass)
	}
	_, branches, err := s.opts.Checkout.Branches(ctx, h.TaskID, h.Repo)
	if err != nil {
		return nil, err
	}
	prs, err := s.opts.GitHub.PullRequests(ctx, h.Repo, branches)
	if err != nil {
		return nil, err
	}
	var out []store.HandoffReviewPR
	for _, pr := range prs {
		out = append(out, store.HandoffReviewPR{TaskID: h.TaskID, Repo: h.Repo, Number: pr.Number, Branch: pr.Branch, HeadSHA: pr.HeadSHA, Author: pr.Author})
	}
	return out, nil
}

func (s *ReviewPassService) countedBot(login string) bool {
	for _, bot := range s.config().CountedBots {
		if strings.EqualFold(bot, login) {
			return true
		}
	}
	return false
}

func commentItemID(c PRComment) string {
	prefix := map[string]string{"review": "c-r-", "inline": "c-i-", "issue": "c-c-"}[c.Kind]
	return prefix + fmt.Sprint(c.ID)
}

func commentTitle(c PRComment) string {
	switch c.Kind {
	case "review":
		return "Review by " + c.Author
	case "inline":
		return fmt.Sprintf("Comment by %s on %s:%d", c.Author, c.Path, c.Line)
	}
	return "Comment by " + c.Author
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// deliverComments tells the author of new comments and failing checks: after quiet_time with
// nothing new, never during a pass, once the author is idle. Each item is
// told once, whatever restarts in between.
func (s *ReviewPassService) deliverComments(ctx context.Context, h *store.Handoff, loop *store.HandoffReview) error {
	items, err := currentItems(ctx, s.db, loop)
	if err != nil {
		return err
	}
	var ids []string
	comments, checks := 0, 0
	for _, item := range items {
		if (item.Kind == ItemComment || item.Kind == ItemCheck) && !item.Delivered && item.Answer == "" {
			ids = append(ids, item.ID)
			if item.Kind == ItemCheck {
				checks++
			} else {
				comments++
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if last, err := time.Parse(time.RFC3339, loop.LastNewAt); err == nil && s.opts.Now().Sub(last) < s.config().QuietTime {
		return nil
	}
	if _, err := s.opts.Agents.GetAgentSession(ctx, h.TaskID); err != nil && s.opts.ResumeAuthor != nil {
		if err := s.opts.ResumeAuthor(ctx, h.TaskID); err != nil {
			return s.stuck(ctx, h, loop, err.Error())
		}
	}
	if !s.authorIdle(ctx, h.TaskID) {
		return nil
	}
	var parts []string
	if comments > 0 {
		parts = append(parts, fmt.Sprintf("%d new %s", comments, plural(comments, "comment", "comments")))
	}
	if checks > 0 {
		parts = append(parts, fmt.Sprintf("%d failing %s", checks, plural(checks, "check", "checks")))
	}
	message := fmt.Sprintf("%s %s on your pull requests. Run legato review-pass feedback --json to read them.", ReviewTag, joinAnd(parts))
	if err := s.opts.Agents.QueueMessage(ctx, h.TaskID, message); err != nil {
		return s.stuck(ctx, h, loop, "the author could not be told of its feedback: "+err.Error())
	}
	return s.db.MarkHandoffReviewItemsDelivered(ctx, h.TaskID, ids)
}
