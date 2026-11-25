package store

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// openBeforeReviewLoopConversion seeds a database at the migration before
// the loop record's conversion, then opens it, which converts.
func openBeforeReviewLoopConversion(t *testing.T, seed string) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "before-loop-conversion.db")
	at := slices.IndexFunc(migrations, func(name string) bool { return strings.HasSuffix(name, "_review_loop_conversion.sql") })
	if at < 1 {
		t.Fatal("no review_loop_conversion migration")
	}
	before := newStoreAtMigration(t, path, migrations[at-1])
	if _, err := before.db.Exec(seed); err != nil {
		t.Fatal(err)
	}
	if err := before.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// convertedLoop is the loop converted from the review loop kept on a task.
func convertedLoop(t *testing.T, s *Store, task string) *ReviewLoop {
	t.Helper()
	var id string
	if err := s.db.Get(&id, `SELECT id FROM review_loops WHERE host_task = ?`, task); err != nil {
		t.Fatalf("the loop of %s: %v", task, err)
	}
	l, err := s.GetReviewLoop(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// Two workflow runs review acme/widget#12: run A's (failed, so its loop is
// kept, and it also reviews #13) and run B's, newer. A finished run C
// approved #12 before both. A worker has an open handoff on acme/other and
// a closed one.
const reviewLoopsBeforeConversion = `
	INSERT INTO tasks (id, title, status, metadata) VALUES
		('run-a', 'Review loop on acme/widget#12', '', '{"legato":{"review_loop":{"run_id":"wr-a","step_id":"review","attempt_id":"att-a1","repo":"acme/widget","pr":12,"branch":"legato/review-12","clone_id":7,"dir":"/jobs/att-a1/widget","linked_task":"board","scope":"stack","stack":[{"number":12,"branch":"legato/review-12"},{"number":13,"branch":"legato/review-13"}],"seen_head":"ccc","seen_at":"2026-11-01T10:05:00Z","replies":[101,102],"confirm_clean":true}}}'),
		('run-b', 'Review loop on acme/widget#12', '', '{"legato":{"review_loop":{"run_id":"wr-b","step_id":"loop/review","attempt_id":"att-b1","repo":"Acme/Widget","pr":12,"branch":"legato/review-12-b","clone_id":8,"dir":"/jobs/att-b1/widget","linked_task":"board","author_task":"board","scope":"pr"}}}'),
		('run-c', 'Review loop on acme/widget#12', '', '{"legato":{"review_loop":{"run_id":"wr-c","step_id":"review","attempt_id":"att-c1","repo":"acme/widget","pr":12,"branch":"legato/review-12-c","linked_task":"board"}}}'),
		('board', 'Widget work', 'Doing', NULL),
		('card-h', 'Handed off', 'Review', '{"other":1}'),
		('card-old', 'Handed off before', 'Done', NULL);
	INSERT INTO workflow_runs (id, workflow_name, workflow_title, definition_source, definition_digest, definition_json, status, task_id, created_at) VALUES
		('wr-a', 'review-pr-passes', 'Review', 'test', 'sha', '{}', 'failed', 'run-a', '2026-11-01 10:00:00'),
		('wr-b', 'review-pr-passes', 'Review', 'test', 'sha', '{}', 'waiting', 'run-b', '2026-11-02 10:00:00'),
		('wr-c', 'review-pr-passes', 'Review', 'test', 'sha', '{}', 'succeeded', 'run-c', '2026-10-01 10:00:00');
	INSERT INTO workflow_steps (run_id, path, step_id, step_order, status) VALUES
		('wr-a', 'review', 'review', 0, 'failed'),
		('wr-b', 'loop/review', 'review', 0, 'waiting'),
		('wr-c', 'review', 'review', 0, 'succeeded');
	INSERT INTO handoffs (main_instance_id, handoff_id, main_name, main_task_id, task_id, purpose, kind, repo, branch, agent_kind, state, request_hash, created_at, updated_at, closed_at) VALUES
		('main-1', 'h-1', 'main', 'mt-1', 'card-h', 'make_change', 'local', 'acme/other', 'feat-5', 'codex', 'in_progress', 'x', '2026-11-03T09:00:00Z', '2026-11-03T09:00:00Z', NULL),
		('main-1', 'h-0', 'main', 'mt-0', 'card-old', 'make_change', 'local', 'acme/other', 'feat-7', 'codex', 'done', 'x', '2026-10-03T09:00:00Z', '2026-10-04T09:00:00Z', '2026-10-04T09:00:00Z');
	INSERT INTO handoff_reviews (task_id, settings, phase, pass, mismatches, nudged_at, author_busy_since_nudge, reason, queued_at, updated_at,
			delivered_pass, polled_at, last_new_at, skipped_comments, checks_polled_at, checks_heads, checks_pending) VALUES
		('run-a', '{"enabled":true,"agent":"claude-code","model":"opus","tier":"deep","skill":"review","max_passes":4,"skill_managed":true}',
			'reviewing', 2, 1, '2026-11-01T10:10:00Z', 1, '', '2026-11-01T10:20:00Z', '2026-11-01T10:30:00Z',
			1, '2026-11-01T10:25:00Z', '2026-11-01T10:15:00Z', 3, '2026-11-01T10:26:00Z', '12:ccc,13:ddd', 1),
		('run-b', '{}', 'waiting_for_author', 1, 0, '', 0, '', '', '2026-11-02T10:30:00Z', 0, '', '', 0, '', '', 0),
		('run-c', '{}', 'clean', 1, 0, '', 0, '', '', '2026-10-01T10:30:00Z', 1, '', '', 0, '', '', 0),
		('card-h', '{"enabled":false,"agent":"codex","max_passes":2}', 'waiting_for_author', 1, 0, '', 0, '', '', '2026-11-03T10:30:00Z', 1, '', '', 0, '', '', 0),
		('card-old', '{}', 'ended', 1, 0, '', 0, 'closed', '', '2026-10-04T09:00:00Z', 1, '', '', 0, '', '', 0);
	INSERT INTO handoff_review_passes (task_id, pass, state, verdict, summary, reason, execution_id, checkout, head_sha, started_at, ended_at, attempt, posting_started_at, post_attempts) VALUES
		('run-a', 1, 'posted', 'changes', 'Two problems', '', 'ex-a1', '/jobs/att-a1/widget', 'bbb', '2026-11-01T10:01:00Z', '2026-11-01T10:09:00Z', 1, '2026-11-01T10:08:00Z', 2),
		('run-a', 2, 'reviewing', '', '', '', 'ex-a2', '/jobs/att-a1/widget', 'ddd', '2026-11-01T10:20:00Z', '', 0, '', 0),
		('run-b', 1, 'posted', 'changes', 'One problem', '', 'ex-b1', '/jobs/att-b1/widget', 'ccc', '2026-11-02T10:01:00Z', '2026-11-02T10:09:00Z', 0, '2026-11-02T10:08:00Z', 0),
		('run-c', 1, 'posted', 'clean', 'Looks good', '', 'ex-c1', '/jobs/att-c1/widget', 'aaa', '2026-10-01T10:01:00Z', '2026-10-01T10:09:00Z', 0, '2026-10-01T10:08:00Z', 0),
		('card-h', 1, 'posted', 'changes', 'Fix it', '', 'ex-h1', '/jobs/h/other', 'eee', '2026-11-03T10:01:00Z', '2026-11-03T10:09:00Z', 0, '2026-11-03T10:08:00Z', 0),
		('card-old', 1, 'posted', 'clean', 'Fine', '', 'ex-o1', '/jobs/o/other', 'ggg', '2026-10-03T10:01:00Z', '2026-10-03T10:09:00Z', 0, '2026-10-03T10:08:00Z', 0);
	INSERT INTO handoff_review_prs (task_id, pass, repo, number, branch, base, title, body, url, head_sha, review_id, review_url, author, event, local_branch, head_repo) VALUES
		('run-a', 1, 'acme/widget', 12, 'feat-12', 'main', 'Widgets', 'Adds widgets', 'https://github.com/acme/widget/pull/12', 'aaa', 9001, 'https://github.com/acme/widget/pull/12#pullrequestreview-9001', 'author[bot]', 'COMMENT', 'legato/review-12', 'acme/widget'),
		('run-a', 1, 'acme/widget', 13, 'feat-13', 'feat-12', 'More widgets', '', 'https://github.com/acme/widget/pull/13', 'bbb', 9002, 'https://github.com/acme/widget/pull/13#pullrequestreview-9002', 'author[bot]', 'COMMENT', 'legato/review-13', 'fork/widget'),
		('run-a', 2, 'acme/widget', 12, 'feat-12', 'main', 'Widgets', 'Adds widgets', 'https://github.com/acme/widget/pull/12', 'ccc', 0, '', 'author[bot]', '', 'legato/review-12', 'acme/widget'),
		('run-a', 2, 'acme/widget', 13, 'feat-13', 'feat-12', 'More widgets', '', 'https://github.com/acme/widget/pull/13', 'ddd', 0, '', 'author[bot]', '', 'legato/review-13', 'fork/widget'),
		('run-b', 1, 'acme/widget', 12, 'feat-12', 'main', 'Widgets', '', 'https://github.com/acme/widget/pull/12', 'ccc', 9101, '', 'author[bot]', 'COMMENT', '', 'acme/widget'),
		('run-c', 1, 'acme/widget', 12, 'feat-12', 'main', 'Widgets', '', 'https://github.com/acme/widget/pull/12', 'aaa', 8001, '', 'author[bot]', 'APPROVE', '', 'acme/widget'),
		('card-h', 1, 'acme/other', 5, 'feat-5', 'main', 'Five', '', '', 'eee', 7001, '', 'author[bot]', 'COMMENT', '', 'acme/other'),
		('card-h', 1, 'acme/other', 6, 'feat-6', 'feat-5', 'Six', '', '', 'fff', 7002, '', 'author[bot]', 'COMMENT', '', 'acme/other'),
		('card-old', 1, 'acme/other', 7, 'feat-7', 'main', 'Seven', '', '', 'ggg', 6001, '', 'author[bot]', 'APPROVE', '', 'acme/other');
	INSERT INTO handoff_review_findings (id, task_id, pass, pr, severity, title, body, path, line, start_line, side, comment_id, status, ruled_pass, ruling) VALUES
		('f-1', 'run-a', 1, 12, 'blocking', 'No bound', 'loops forever', 'widget.go', 40, 38, 'RIGHT', 501, 'open', 0, ''),
		('f-2', 'run-a', 1, 13, 'nit', 'Name', 'a better name', 'more.go', 3, 0, 'RIGHT', 502, 'resolved', 2, 'renamed'),
		('f-1', 'run-b', 1, 12, 'blocking', 'Leak', 'closes nothing', 'widget.go', 9, 0, 'RIGHT', 601, 'open', 0, '');
	INSERT INTO handoff_review_items (id, task_id, kind, pass, repo, pr, path, line, url, version, outdated, author, title, body, thread_comment_id, source_id,
			answer, answer_note, answered_at, posted, closed, created_at, delivered, answer_marker) VALUES
		('f-1', 'run-a', 'blocking', 1, 'acme/widget', 12, 'widget.go', 40, 'https://github.com/acme/widget/pull/12#discussion_r501', 2, 1, 'legato-reviews[bot]', 'No bound', 'loops forever', 501, 'f-1',
			'fixed', 'bounded at 10', '2026-11-01T10:12:00Z', 1, 0, '2026-11-01T10:09:00Z', 1, 'm-a1'),
		('k-12-build', 'run-a', 'check', 1, 'acme/widget', 12, '', 0, '', 1, 0, '', 'Check build: failure', '', 0, '', '', '', '', 0, 0, '2026-11-01T10:09:00Z', 0, ''),
		('f-1', 'run-b', 'blocking', 1, 'acme/widget', 12, 'widget.go', 9, '', 1, 0, 'legato-reviews[bot]', 'Leak', 'closes nothing', 601, 'f-1',
			'disputed', 'it is closed by the caller', '2026-11-02T10:12:00Z', 0, 0, '2026-11-02T10:09:00Z', 1, 'm-b1');
	INSERT INTO handoff_review_poll_pages (task_id, pr, list, page, etag, body) VALUES
		('run-a', 12, 'comments', 1, 'W/"etag-a"', '[]'),
		('card-h', 5, 'reviews', 1, 'W/"etag-h"', '[{"id":1}]');
`

// AC 44: the newest open loop on a pull request keeps its claim; an older
// one converts as stuck, naming the loop that holds it, and keeps its
// claims no other loop wanted.
func TestConvertingReviewLoopsKeepsTheNewestClaimPerPRAndMakesTheOthersStuckNamingIt(t *testing.T) {
	s := openBeforeReviewLoopConversion(t, reviewLoopsBeforeConversion)
	ctx := context.Background()
	a, b := convertedLoop(t, s, "run-a"), convertedLoop(t, s, "run-b")

	holder, err := s.ActiveReviewLoop(ctx, "acme/widget", 12)
	if err != nil {
		t.Fatal(err)
	}
	if holder == nil || holder.ID != b.ID {
		t.Fatalf("acme/widget#12 is held by %+v, want run B's loop %s", holder, b.ID)
	}
	if b.State != ReviewLoopOpenState || b.Phase != "waiting_for_author" {
		t.Fatalf("run B's loop = %s/%s, want open and waiting for the author", b.State, b.Phase)
	}
	if a.State != ReviewLoopOpenState || a.Phase != "stuck" {
		t.Fatalf("run A's loop = %s/%s, want open and stuck", a.State, a.Phase)
	}
	if !strings.Contains(a.Reason, b.ID) || !strings.Contains(a.Reason, "#12") {
		t.Fatalf("run A's stuck reason %q does not name %s and #12", a.Reason, b.ID)
	}
	if holder, err := s.ActiveReviewLoop(ctx, "acme/widget", 13); err != nil || holder == nil || holder.ID != a.ID {
		t.Fatalf("acme/widget#13 is held by %+v (%v), want run A's loop", holder, err)
	}
	// Run A's pass in flight is set aside: a stuck loop holds no slot.
	pass, err := s.GetReviewLoopPass(ctx, a.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if pass.State != ReviewLoopPassDiscarded {
		t.Fatalf("run A's pass 2 is %s, want discarded", pass.State)
	}
	if first, err := s.GetReviewLoopPass(ctx, a.ID, 1); err != nil || first.State != ReviewLoopPassPosted {
		t.Fatalf("run A's pass 1 = %+v (%v), want posted", first, err)
	}
	// The run that finished approved closed its loop: it holds nothing.
	c := convertedLoop(t, s, "run-c")
	if c.State != ReviewLoopClosedState || c.Outcome != "approved" {
		t.Fatalf("run C's loop = %s/%s, want closed approved", c.State, c.Outcome)
	}
}

// AC 44: an answer posted before the upgrade converts as posted, with its
// marker, so it is never posted again; one not yet posted keeps its marker
// for the search before its post. Items with one ID in two loops stay apart.
func TestConvertedAnswersPostedBeforeTheUpgradeStayPosted(t *testing.T) {
	s := openBeforeReviewLoopConversion(t, reviewLoopsBeforeConversion)
	ctx := context.Background()
	a, b := convertedLoop(t, s, "run-a"), convertedLoop(t, s, "run-b")

	posted, err := s.GetReviewLoopItem(ctx, a.ID, "f-1")
	if err != nil {
		t.Fatal(err)
	}
	if !posted.Posted || posted.Answer != "fixed" || posted.AnswerMarker != "m-a1" || posted.AnswerNote != "bounded at 10" ||
		posted.Version != 2 || !posted.Outdated || posted.ThreadCommentID != 501 || !posted.Delivered || posted.Kind != "blocking" {
		t.Fatalf("run A's answered item = %+v, want posted with its marker", posted)
	}
	waiting, err := s.GetReviewLoopItem(ctx, b.ID, "f-1")
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Posted || waiting.Answer != "disputed" || waiting.AnswerMarker != "m-b1" || waiting.ThreadCommentID != 601 {
		t.Fatalf("run B's answered item = %+v, want unposted with its marker", waiting)
	}
	if check, err := s.GetReviewLoopItem(ctx, a.ID, "k-12-build"); err != nil || check.Kind != "check" || check.Answer != "" {
		t.Fatalf("run A's check item = %+v (%v)", check, err)
	}
}

// Markers and pin refs of a converted loop keep the old task ID, so the
// reviews, answers and refs already on GitHub still match.
func TestConvertedLoopsKeepTheOldTaskIDAsTheirMarkerKey(t *testing.T) {
	s := openBeforeReviewLoopConversion(t, reviewLoopsBeforeConversion)
	for _, task := range []string{"run-a", "run-b", "run-c", "card-h", "card-old"} {
		l := convertedLoop(t, s, task)
		if l.MarkerKey != task || l.HostTask != task {
			t.Fatalf("the loop of %s has marker key %q and host task %q", task, l.MarkerKey, l.HostTask)
		}
		if l.Rev != 1 || !strings.HasPrefix(l.ID, "rl-") || len(l.ID) != 15 {
			t.Fatalf("the loop of %s is %s at rev %d", task, l.ID, l.Rev)
		}
	}
}

// What a run's loop kept on its task's metadata and in the old rows lands
// on the record: owner, settings, counters, clone, layers, passes with their
// pins and post attempts, findings and poll pages.
func TestAConvertedRunLoopCarriesEverythingItsRowsAndMetadataHeld(t *testing.T) {
	s := openBeforeReviewLoopConversion(t, reviewLoopsBeforeConversion)
	ctx := context.Background()
	a := convertedLoop(t, s, "run-a")

	if a.OwnerKind != ReviewLoopOwnerRun || a.RunID != "wr-a" || a.StepPath != "review" || a.AttemptID != "att-a1" || a.HandoffID != "" {
		t.Fatalf("owner = %s run %s step %s attempt %s handoff %q", a.OwnerKind, a.RunID, a.StepPath, a.AttemptID, a.HandoffID)
	}
	if a.Repo != "acme/widget" || a.SubjectPR != 12 || a.Scope != "stack" || a.AgentAuthor || a.AuthorTask != "" || a.LinkedTask != "board" {
		t.Fatalf("subject = %+v", a)
	}
	if a.PassesOff || a.Reviewer != "claude-code" || a.ReviewerModel != "opus" || a.ReviewerTier != "deep" || a.Skill != "review" ||
		!a.SkillManaged || a.MaxPasses != 4 {
		t.Fatalf("settings = %+v", a)
	}
	if a.Pass != 2 || a.Mismatches != 1 || a.NudgedAt != "2026-11-01T10:10:00Z" || !a.AuthorBusySinceNudge || a.DeliveredPass != 1 ||
		a.PolledAt != "2026-11-01T10:25:00Z" || a.LastNewAt != "2026-11-01T10:15:00Z" || a.SkippedComments != 3 ||
		a.ChecksPolledAt != "2026-11-01T10:26:00Z" || a.ChecksHeads != "12:ccc,13:ddd" || a.ChecksPending != 1 || a.UpdatedAt != "2026-11-01T10:30:00Z" {
		t.Fatalf("counters = %+v", a)
	}
	if a.SeenHead != "ccc" || a.SeenAt != "2026-11-01T10:05:00Z" || a.Replies != "[101,102]" || !a.ConfirmClean ||
		a.CloneID != 7 || a.CloneDir != "/jobs/att-a1/widget" || a.QueuedAt != "" || a.CreatedAt != "2026-11-01T10:00:00Z" {
		t.Fatalf("run state = %+v", a)
	}

	prs, err := s.ListReviewLoopPRs(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 || prs[0].Number != 12 || prs[1].Number != 13 {
		t.Fatalf("layers = %+v", prs)
	}
	top := prs[1]
	if top.Position != 1 || top.Branch != "feat-13" || top.Base != "feat-12" || top.LocalBranch != "legato/review-13" || top.HeadRepo != "fork/widget" ||
		top.PinnedHead != "ddd" || top.ReviewID != 9002 || top.Author != "author[bot]" || top.State != "open" || top.ClaimedAt == "" {
		t.Fatalf("layer #13 = %+v", top)
	}
	if prs[0].ClaimedAt != "" {
		t.Fatalf("layer #12, held by run B's loop, has a claim time %q", prs[0].ClaimedAt)
	}

	first, err := s.GetReviewLoopPass(ctx, a.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Verdict != "changes" || first.Summary != "Two problems" || first.ExecutionID != "ex-a1" || first.Checkout != "/jobs/att-a1/widget" ||
		first.HeadSHA != "bbb" || first.Attempt != 1 || first.PostingStartedAt != "2026-11-01T10:08:00Z" || first.Attempts != 2 ||
		first.StepPath != "review" || first.StartedAt != "2026-11-01T10:01:00Z" || first.EndedAt != "2026-11-01T10:09:00Z" {
		t.Fatalf("pass 1 = %+v", first)
	}
	if len(first.Pins) != 2 {
		t.Fatalf("pass 1 pins = %+v", first.Pins)
	}
	pin := first.Pins[0]
	if pin.Number != 12 || pin.Repo != "acme/widget" || pin.Branch != "feat-12" || pin.Base != "main" || pin.Title != "Widgets" || pin.Body != "Adds widgets" ||
		pin.URL != "https://github.com/acme/widget/pull/12" || pin.HeadSHA != "aaa" || pin.ReviewID != 9001 || pin.Event != "COMMENT" ||
		pin.ReviewURL != "https://github.com/acme/widget/pull/12#pullrequestreview-9001" || pin.Author != "author[bot]" ||
		pin.LocalBranch != "legato/review-12" || pin.HeadRepo != "acme/widget" {
		t.Fatalf("pass 1 pin #12 = %+v", pin)
	}

	findings, err := s.ListReviewLoopFindings(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 || findings[0].ID != "f-1" || findings[0].Path != "widget.go" || findings[0].Line != 40 || findings[0].StartLine != 38 ||
		findings[0].Side != "RIGHT" || findings[0].CommentID != 501 || findings[1].Status != "resolved" || findings[1].RuledPass != 2 || findings[1].Ruling != "renamed" {
		t.Fatalf("findings = %+v", findings)
	}
	if etag, body, ok := s.ReviewLoopPollPage(ctx, a.ID, 12, "comments", 1); !ok || etag != `W/"etag-a"` || body != "[]" {
		t.Fatalf("poll page = %q %q %v", etag, body, ok)
	}
}

// A handoff's loop is owned by the handoff, its card the author; a closed
// handoff's loop is closed history holding nothing.
func TestAConvertedHandoffLoopIsOwnedByItsHandoff(t *testing.T) {
	s := openBeforeReviewLoopConversion(t, reviewLoopsBeforeConversion)
	ctx := context.Background()
	h := convertedLoop(t, s, "card-h")
	if h.OwnerKind != ReviewLoopOwnerHandoff || h.HandoffID != "h-1" || h.MainInstanceID != "main-1" || h.RunID != "" {
		t.Fatalf("owner = %+v", h)
	}
	if h.State != ReviewLoopOpenState || h.Phase != "waiting_for_author" || h.Repo != "acme/other" || !h.AgentAuthor || h.AuthorTask != "card-h" {
		t.Fatalf("loop = %+v", h)
	}
	if !h.PassesOff || h.Reviewer != "codex" || h.MaxPasses != 2 {
		t.Fatalf("settings = %+v", h)
	}
	for _, number := range []int{5, 6} {
		if holder, err := s.ActiveReviewLoop(ctx, "acme/other", number); err != nil || holder == nil || holder.ID != h.ID {
			t.Fatalf("acme/other#%d is held by %+v (%v)", number, holder, err)
		}
	}
	if etag, _, ok := s.ReviewLoopPollPage(ctx, h.ID, 5, "reviews", 1); !ok || etag != `W/"etag-h"` {
		t.Fatalf("poll page %q %v", etag, ok)
	}

	old := convertedLoop(t, s, "card-old")
	if old.State != ReviewLoopClosedState || old.Outcome != "closed" || old.HandoffID != "h-0" || old.ClosedAt != "2026-10-04T09:00:00Z" {
		t.Fatalf("the closed handoff's loop = %+v", old)
	}
	if holder, err := s.ActiveReviewLoop(ctx, "acme/other", 7); err != nil || holder != nil {
		t.Fatalf("acme/other#7 is held by %+v (%v), want no one", holder, err)
	}
	open, err := s.ListOpenReviewLoops(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 3 {
		t.Fatalf("%d open loops, want run A's, run B's and the open handoff's", len(open))
	}
}

// Two open agent-authored loops of one author: the record allows one, so
// the newer stays open and the older closes stuck, naming it.
func TestConvertingTwoOpenLoopsOfOneAuthorKeepsTheNewer(t *testing.T) {
	s := openBeforeReviewLoopConversion(t, `
		INSERT INTO tasks (id, title, status, metadata) VALUES
			('run-d', 'D', '', '{"legato":{"review_loop":{"run_id":"wr-d","step_id":"review","attempt_id":"att-d","repo":"acme/x","pr":1,"author_task":"board"}}}'),
			('run-e', 'E', '', '{"legato":{"review_loop":{"run_id":"wr-e","step_id":"review","attempt_id":"att-e","repo":"acme/x","pr":2,"author_task":"board"}}}'),
			('board', 'Board', 'Doing', NULL);
		INSERT INTO workflow_runs (id, workflow_name, workflow_title, definition_source, definition_digest, definition_json, status, task_id, created_at) VALUES
			('wr-d', 'r', 'R', 'test', 'sha', '{}', 'waiting', 'run-d', '2026-11-01 10:00:00'),
			('wr-e', 'r', 'R', 'test', 'sha', '{}', 'waiting', 'run-e', '2026-11-02 10:00:00');
		INSERT INTO workflow_steps (run_id, path, step_id, step_order, status) VALUES ('wr-d', 'review', 'review', 0, 'waiting'), ('wr-e', 'review', 'review', 0, 'waiting');
		INSERT INTO handoff_reviews (task_id, phase, pass, updated_at) VALUES ('run-d', 'waiting_for_author', 1, '2026-11-01T11:00:00Z'), ('run-e', 'ready', 0, '2026-11-02T11:00:00Z');
	`)
	ctx := context.Background()
	d, e := convertedLoop(t, s, "run-d"), convertedLoop(t, s, "run-e")
	if e.State != ReviewLoopOpenState {
		t.Fatalf("the newer loop is %s", e.State)
	}
	if d.State != ReviewLoopClosedState || d.Outcome != "stuck" || !strings.Contains(d.Reason, e.ID) {
		t.Fatalf("the older loop = %s/%s %q, want closed stuck naming %s", d.State, d.Outcome, d.Reason, e.ID)
	}
	if holder, err := s.ActiveReviewLoop(ctx, "acme/x", 1); err != nil || holder != nil {
		t.Fatalf("acme/x#1 is held by %+v (%v), want no one", holder, err)
	}
	if holder, err := s.ActiveReviewLoop(ctx, "acme/x", 2); err != nil || holder == nil || holder.ID != e.ID {
		t.Fatalf("acme/x#2 is held by %+v (%v)", holder, err)
	}
}

// Invariant 9: a converted loop is approved only when it was: clean, not
// waiting for a confirm of GitHub's heads, and ended by its own success.
// Run ok succeeded after its confirm. Run cancel was cancelled after its
// clean pass posted, before the confirm read GitHub's heads; run stopped
// was stopped after its confirm, so it did not end approved either. Run
// open still waits for its confirm, and the handoff's card went Done after
// a clean pass that nothing confirmed (the older Legato never did).
const cleanLoopsBeforeConversion = `
	INSERT INTO tasks (id, title, status, metadata) VALUES
		('run-ok', 'OK', '', '{"legato":{"review_loop":{"run_id":"wr-ok","step_id":"review","attempt_id":"att-ok","repo":"acme/clean","pr":1}}}'),
		('run-cancel', 'Cancel', '', '{"legato":{"review_loop":{"run_id":"wr-cancel","step_id":"review","attempt_id":"att-cancel","repo":"acme/clean","pr":2,"confirm_clean":true}}}'),
		('run-stopped', 'Stopped', '', '{"legato":{"review_loop":{"run_id":"wr-stopped","step_id":"review","attempt_id":"att-stopped","repo":"acme/clean","pr":3}}}'),
		('run-open', 'Open', '', '{"legato":{"review_loop":{"run_id":"wr-open","step_id":"review","attempt_id":"att-open","repo":"acme/clean","pr":4,"confirm_clean":true}}}'),
		('card-done', 'Handed off', 'Done', NULL);
	INSERT INTO workflow_runs (id, workflow_name, workflow_title, definition_source, definition_digest, definition_json, status, task_id, created_at, completed_at) VALUES
		('wr-ok', 'review-pr-passes', 'Review', 'test', 'sha', '{}', 'succeeded', 'run-ok', '2026-10-05 10:00:00', '2026-10-05 10:40:00'),
		('wr-cancel', 'review-pr-passes', 'Review', 'test', 'sha', '{}', 'cancelled', 'run-cancel', '2026-10-06 10:00:00', '2026-10-06 10:40:00'),
		('wr-stopped', 'review-pr-passes', 'Review', 'test', 'sha', '{}', 'stopped', 'run-stopped', '2026-10-07 10:00:00', '2026-10-07 10:40:00'),
		('wr-open', 'review-pr-passes', 'Review', 'test', 'sha', '{}', 'waiting', 'run-open', '2026-10-08 10:00:00', NULL);
	INSERT INTO workflow_steps (run_id, path, step_id, step_order, status) VALUES
		('wr-ok', 'review', 'review', 0, 'succeeded'),
		('wr-cancel', 'review', 'review', 0, 'cancelled'),
		('wr-stopped', 'review', 'review', 0, 'cancelled'),
		('wr-open', 'review', 'review', 0, 'waiting');
	INSERT INTO handoffs (main_instance_id, handoff_id, main_name, main_task_id, task_id, purpose, kind, repo, branch, agent_kind, state, request_hash, created_at, updated_at, closed_at) VALUES
		('main-1', 'h-9', 'main', 'mt-9', 'card-done', 'make_change', 'local', 'acme/clean', 'feat-9', 'codex', 'done', 'x', '2026-10-09T09:00:00Z', '2026-10-09T11:00:00Z', '2026-10-09T11:00:00Z');
	INSERT INTO handoff_reviews (task_id, phase, pass, updated_at) VALUES
		('run-ok', 'clean', 1, '2026-10-05T10:30:00Z'),
		('run-cancel', 'clean', 1, '2026-10-06T10:30:00Z'),
		('run-stopped', 'clean', 1, '2026-10-07T10:30:00Z'),
		('run-open', 'clean', 1, '2026-10-08T10:30:00Z'),
		('card-done', 'clean', 1, '2026-10-09T10:30:00Z');
	INSERT INTO handoff_review_passes (task_id, pass, state, verdict, summary, execution_id, head_sha, started_at, ended_at) VALUES
		('run-ok', 1, 'posted', 'clean', 'Good', 'ex-ok', 'h1', '2026-10-05T10:01:00Z', '2026-10-05T10:09:00Z'),
		('run-cancel', 1, 'posted', 'clean', 'Good', 'ex-cancel', 'h2', '2026-10-06T10:01:00Z', '2026-10-06T10:09:00Z'),
		('run-stopped', 1, 'posted', 'clean', 'Good', 'ex-stopped', 'h3', '2026-10-07T10:01:00Z', '2026-10-07T10:09:00Z'),
		('run-open', 1, 'posted', 'clean', 'Good', 'ex-open', 'h4', '2026-10-08T10:01:00Z', '2026-10-08T10:09:00Z'),
		('card-done', 1, 'posted', 'clean', 'Good', 'ex-done', 'h9', '2026-10-09T10:01:00Z', '2026-10-09T10:09:00Z');
	INSERT INTO handoff_review_prs (task_id, pass, repo, number, branch, base, head_sha, review_id, event) VALUES
		('run-ok', 1, 'acme/clean', 1, 'feat-1', 'main', 'h1', 501, 'APPROVE'),
		('run-cancel', 1, 'acme/clean', 2, 'feat-2', 'main', 'h2', 502, 'APPROVE'),
		('run-stopped', 1, 'acme/clean', 3, 'feat-3', 'main', 'h3', 503, 'APPROVE'),
		('run-open', 1, 'acme/clean', 4, 'feat-4', 'main', 'h4', 504, 'APPROVE'),
		('card-done', 1, 'acme/clean', 9, 'feat-9', 'main', 'h9', 509, 'APPROVE');
`

func TestAConvertedLoopIsApprovedOnlyWhenItsCleanPassWasConfirmed(t *testing.T) {
	s := openBeforeReviewLoopConversion(t, cleanLoopsBeforeConversion)
	ctx := context.Background()
	cleanHeads := func(l *ReviewLoop) []string {
		t.Helper()
		prs, err := s.ListReviewLoopPRs(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		var heads []string
		for _, pr := range prs {
			heads = append(heads, pr.CleanHead)
		}
		return heads
	}

	ok := convertedLoop(t, s, "run-ok")
	if ok.State != ReviewLoopClosedState || ok.Outcome != "approved" || ok.ConfirmedAt != "2026-10-05T10:40:00Z" || ok.CleanPass != 1 ||
		ok.WindowStart != 1 || ok.ConfirmClean {
		t.Fatalf("the succeeded run's loop = %+v; want closed approved, confirmed at pass 1", ok)
	}
	if heads := cleanHeads(ok); len(heads) != 1 || heads[0] != "h1" {
		t.Fatalf("the succeeded run's clean heads = %v", heads)
	}

	for task, outcome := range map[string]string{"run-cancel": "cancelled", "run-stopped": "cancelled", "card-done": "ended"} {
		l := convertedLoop(t, s, task)
		if l.State != ReviewLoopClosedState || l.Outcome != outcome || l.ConfirmedAt != "" || l.CleanPass != 0 {
			t.Fatalf("the loop of %s = %s/%s confirmed %q at pass %d; want closed %s, never confirmed", task, l.State, l.Outcome, l.ConfirmedAt, l.CleanPass, outcome)
		}
		if heads := cleanHeads(l); len(heads) != 1 || heads[0] != "" {
			t.Fatalf("the loop of %s has clean heads %v; want none", task, heads)
		}
	}

	open := convertedLoop(t, s, "run-open")
	if open.State != ReviewLoopOpenState || open.Phase != "clean" || open.Outcome != "" || open.ConfirmedAt != "" || open.CleanPass != 0 || !open.ConfirmClean {
		t.Fatalf("the waiting run's loop = %+v; want open, clean and waiting for its confirm", open)
	}
	if heads := cleanHeads(open); len(heads) != 1 || heads[0] != "" {
		t.Fatalf("the waiting run's clean heads = %v; want none before the confirm", heads)
	}
	if holder, err := s.ActiveReviewLoop(ctx, "acme/clean", 4); err != nil || holder == nil || holder.ID != open.ID {
		t.Fatalf("acme/clean#4 is held by %+v (%v)", holder, err)
	}
}
