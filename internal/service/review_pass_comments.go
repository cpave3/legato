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
// with the bots of review_passes.counted_bots. They are the repository's
// insiders: every other association (CONTRIBUTOR, FIRST_TIME_CONTRIBUTOR,
// FIRST_TIMER, NONE, or none read) is outside (spec LEG-559 X2).
var countedAssociations = map[string]bool{"OWNER": true, "MEMBER": true, "COLLABORATOR": true}

// insideRepository says whether an author association is the
// repository's own: OWNER, MEMBER or COLLABORATOR.
func insideRepository(association string) bool {
	return countedAssociations[strings.ToUpper(strings.TrimSpace(association))]
}

// sameLogin says whether two GitHub logins, as REST gives them, name one
// account: equal ignoring case, the "[bot]" suffix kept, as postedBy
// compares them. REST names an App "<slug>[bot]", and a person can never
// have the suffix, so a person whose login is an App's slug is not the
// App. Every login compared here is REST's (comment authors, pull request
// authors, the logins GitHub answered Legato's posts with) or a
// configured bot name given the suffix (botLogins: github.app_logins,
// review_passes.counted_bots); none is GraphQL's, which drops the suffix
// (LEG-604).
func sameLogin(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	return a != "" && strings.EqualFold(a, b)
}

// botLogins are configured bot names (github.app_logins,
// review_passes.counted_bots) as REST names the bots: each name without
// the "[bot]" suffix gets it, one that has it (in any case) is kept, and
// blank ones are dropped. A configured name always names a bot, so
// "cubic-dev-ai" is the bot cubic-dev-ai[bot], never a person whose login
// is cubic-dev-ai.
func botLogins(names []string) []string {
	var out []string
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), "[bot]") {
			name += "[bot]"
		}
		out = append(out, name)
	}
	return out
}

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

// pollComments polls a loop's pull requests for comments when it is
// due, and turns the counted ones into items: new, edited (a new version
// to handle again), resolved by a person (handled), outdated (marked) or
// deleted (dropped, the author told when it had it). Only a read that
// succeeded closes an item: a pull request whose poll failed (a 5xx, the
// network) keeps its items as they are, and a rate limit ends the poll
// with nothing closed and the next one after the limit. Call with s.mu
// held.
func (s *ReviewPassService) pollComments(ctx context.Context, l ReviewLoop) {
	cfg := s.config()
	loop, err := s.db.GetHandoffReview(ctx, l.TaskID)
	if err != nil {
		return
	}
	if loop == nil {
		loop = &store.HandoffReview{TaskID: l.TaskID, Settings: "{}"}
	}
	interval := cfg.PollInterval
	if interval <= 0 {
		interval = time.Minute
	}
	if polled, err := time.Parse(time.RFC3339, loop.PolledAt); err == nil && s.opts.Now().Before(polled.Add(interval)) {
		return
	}
	prs, err := s.pollTargets(ctx, l, loop)
	if err != nil || len(prs) == 0 {
		return
	}
	items, err := s.db.ListHandoffReviewItems(ctx, l.TaskID)
	if err != nil {
		return
	}
	byID := map[string]store.HandoffReviewItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	seen := map[string]bool{}
	failed := map[string]bool{} // repo#number of the pull requests not read
	skipped, fresh := 0, 0
	for _, pr := range prs {
		poll, err := s.opts.GitHub.PollComments(ctx, pr.Repo, pr.Number, storePollPages{db: s.db, taskID: l.TaskID, pr: pr.Number})
		if err != nil {
			var limit *GitHubRateLimit
			if errors.As(err, &limit) {
				loop.PolledAt = limit.RetryAt.UTC().Add(-interval).Format(time.RFC3339)
				_ = s.save(ctx, loop)
				return
			}
			log.Printf("review passes: polling %s#%d: %v", pr.Repo, pr.Number, err)
			failed[fmt.Sprintf("%s#%d", pr.Repo, pr.Number)] = true
			continue
		}
		f, sk := s.countPolled(ctx, l, loop, pr.Repo, pr.Number, pr.Author, poll, byID, seen)
		fresh, skipped = fresh+f, skipped+sk
	}
	gone := s.closeUnseen(ctx, items, seen, func(item store.HandoffReviewItem) bool {
		return !failed[fmt.Sprintf("%s#%d", item.Repo, item.PR)]
	})
	loop.PolledAt, loop.SkippedComments = s.now(), skipped
	if fresh > 0 {
		loop.LastNewAt = s.now()
		if loop.Phase == ReviewPhaseClean {
			// A comment after a clean pass: the loop is not done.
			loop.Phase = ReviewPhaseWaitingForAuthor
			l.stage(ctx, ReviewStageAuthor)
		}
	}
	if len(gone) > 0 {
		_ = s.tellAuthor(ctx, l, fmt.Sprintf("%s %s was deleted on GitHub; you need not answer it.", ReviewTag, strings.Join(gone, ", ")))
	}
	if err := s.saveChanged(ctx, loop); err != nil {
		log.Printf("review passes: %v", err)
	}
}

// countPolled turns one pull request's polled comments into items: the
// counted ones (from the repository's insiders and counted bots, never
// Legato's own) new, or edited (a new version to handle again), a thread
// a person resolved handled. It marks each counted comment seen, and says
// how many items are new and how many comments did not count.
func (s *ReviewPassService) countPolled(ctx context.Context, l ReviewLoop, loop *store.HandoffReview, repo string, number int, prAuthor string,
	poll CommentPoll, byID map[string]store.HandoffReviewItem, seen map[string]bool) (fresh, skipped int) {
	// Legato's own comments are known by who posted them: the pull
	// request's author (the author App's on a handoff) and the Apps'
	// logins Legato knows. Never by a marker in a comment's text, which
	// anyone can copy.
	legato := append(s.legatoLogins(ctx), prAuthor)
	for _, c := range poll.Comments {
		if anyLogin(legato, c.Author) || (c.Kind == "review" && (strings.TrimSpace(c.Body) == "" || c.State == "PENDING")) {
			continue
		}
		if !countedAssociations[c.Association] && !s.countedBot(c.Author) {
			skipped++
			continue
		}
		id := commentItemID(c)
		seen[id] = true
		item, known := byID[id]
		before := item
		if !known {
			item = store.HandoffReviewItem{ID: id, TaskID: l.TaskID, Kind: ItemComment, Pass: loop.Pass, Repo: repo, PR: number,
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
		if known && item == before {
			continue
		}
		if err := s.db.UpsertHandoffReviewItem(ctx, item); err != nil {
			log.Printf("review passes: %v", err)
		}
	}
	return fresh, skipped
}

// closeUnseen closes the open comment items a poll did not see on a pull
// request it read (read says so): deleted on GitHub. It returns those the
// author was told of and had not answered.
func (s *ReviewPassService) closeUnseen(ctx context.Context, items []store.HandoffReviewItem, seen map[string]bool, read func(store.HandoffReviewItem) bool) []string {
	var gone []string
	for _, item := range items {
		if item.Kind == ItemComment && !item.Closed && !seen[item.ID] && read(item) {
			item.Closed = true
			_ = s.db.UpsertHandoffReviewItem(ctx, item)
			if item.Delivered && item.Answer == "" {
				gone = append(gone, item.ID)
			}
		}
	}
	return gone
}

// pollTargets are the pull requests to poll: those of the last pass, else
// the loop's open ones.
func (s *ReviewPassService) pollTargets(ctx context.Context, l ReviewLoop, loop *store.HandoffReview) ([]store.HandoffReviewPR, error) {
	if loop.Pass > 0 {
		return s.db.ListHandoffReviewPRs(ctx, l.TaskID, loop.Pass)
	}
	if l.caller == nil {
		return nil, nil
	}
	prs, err := l.caller.OpenPRs(ctx, l)
	if err != nil {
		return nil, err
	}
	var out []store.HandoffReviewPR
	for _, pr := range prs {
		out = append(out, store.HandoffReviewPR{TaskID: l.TaskID, Repo: l.Repo, Number: pr.Number, Branch: pr.Branch, HeadSHA: pr.HeadSHA, Author: pr.Author})
	}
	return out, nil
}

// legatoLogins are the logins of Legato's own GitHub Apps: those learned
// from Legato's own posts (the login GitHub answered each review it posted
// with) and those the configuration names (botLogins). A login is never
// learned from a comment's text.
func (s *ReviewPassService) legatoLogins(ctx context.Context) []string {
	learned, _ := s.db.GitHubAppLogins(ctx)
	return append(learned, botLogins(s.config().AppLogins)...)
}

// anyLogin says whether login is one of logins (sameLogin).
func anyLogin(logins []string, login string) bool {
	for _, l := range logins {
		if sameLogin(l, login) {
			return true
		}
	}
	return false
}

func (s *ReviewPassService) countedBot(login string) bool {
	for _, bot := range botLogins(s.config().CountedBots) {
		if sameLogin(bot, login) {
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
// told once, whatever restarts in between. While a steward watches, the
// line is the steward's ([legato steward]).
func (s *ReviewPassService) deliverComments(ctx context.Context, l ReviewLoop, loop *store.HandoffReview) error {
	items, err := currentItems(ctx, s.db, loop)
	if err != nil {
		return err
	}
	var ids []string
	counts := map[string]int{}
	for _, item := range items {
		if routedItem(item.Kind) && !item.Delivered && item.Answer == "" {
			ids = append(ids, item.ID)
			counts[item.Kind]++
		}
	}
	if len(ids) == 0 {
		return nil
	}
	if last, err := time.Parse(time.RFC3339, loop.LastNewAt); err == nil && s.opts.Now().Sub(last) < s.config().QuietTime {
		return nil
	}
	if err := s.authorRunning(ctx, l); err != nil && !errors.Is(err, ErrCannotResumeAuthor) {
		return s.stuck(ctx, l, loop, err.Error())
	}
	if !s.authorIdle(ctx, l.AuthorTaskID) {
		return nil
	}
	var parts []string
	for _, kind := range []struct{ key, one, many string }{
		{ItemComment, "new comment", "new comments"}, {ItemCheck, "failing check", "failing checks"},
		{ItemMergeability, "mergeability problem", "mergeability problems"}, {ItemMergedLayer, "merged layer", "merged layers"},
	} {
		if n := counts[kind.key]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, plural(n, kind.one, kind.many)))
		}
	}
	message := fmt.Sprintf("%s %s on your pull requests. Run legato review-pass feedback --json to read them.", ReviewTag, joinAnd(parts))
	if s.stewardRecord(ctx, l.TaskID) != nil {
		// After approval: the steward's, each item naming what a fix may
		// need.
		message = fmt.Sprintf("%s %s on your pull requests. Run legato review-pass feedback --json to read them: each item names the operation a fix may need, and nothing is granted ahead.", StewardTag, joinAnd(parts))
	}
	if err := s.tellAuthor(ctx, l, message); err != nil {
		return s.stuck(ctx, l, loop, "the author could not be told of its feedback: "+err.Error())
	}
	return s.db.MarkHandoffReviewItemsDelivered(ctx, l.TaskID, ids)
}
