package store

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func twoReviewLoops(t *testing.T) *Store {
	t.Helper()
	s := newTestStore(t)
	for _, id := range []string{"widget-card", "gadget-card"} {
		if err := s.CreateTask(context.Background(), Task{ID: id, Title: id, Status: "Doing", CreatedAt: "2024-01-01", UpdatedAt: "2024-01-01"}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// The same pull request number and check name in two repositories give two
// loops the same item ID: each loop keeps its own item.
func TestReviewItemsWithTheSameIDBelongToTheirOwnLoop(t *testing.T) {
	s := twoReviewLoops(t)
	ctx := context.Background()
	widget := HandoffReviewItem{ID: "k-7-build", TaskID: "widget-card", Kind: "check", Pass: 1, Repo: "acme/widget", PR: 7, Version: 1,
		Title: "Check build: failure on aaa1111", CreatedAt: "2024-01-01T00:00:00Z"}
	gadget := HandoffReviewItem{ID: "k-7-build", TaskID: "gadget-card", Kind: "check", Pass: 1, Repo: "acme/gadget", PR: 7, Version: 1,
		Title: "Check build: failure on ccc3333", CreatedAt: "2024-01-01T00:00:01Z"}
	if err := s.UpsertHandoffReviewItem(ctx, widget); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertHandoffReviewItem(ctx, gadget); err != nil {
		t.Fatal(err)
	}

	for _, want := range []HandoffReviewItem{widget, gadget} {
		items, err := s.ListHandoffReviewItems(ctx, want.TaskID)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].Repo != want.Repo || items[0].Title != want.Title {
			t.Fatalf("%s's items = %+v; want its own %s item", want.TaskID, items, want.Repo)
		}
	}

	// Answering, posting and closing one loop's item leaves the other's.
	if err := s.AnswerHandoffReviewItem(ctx, "widget-card", "k-7-build", "replied", "flaky", "2024-01-02T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkHandoffReviewItemPosted(ctx, "widget-card", "k-7-build"); err != nil {
		t.Fatal(err)
	}
	widget.Closed = true
	if err := s.UpsertHandoffReviewItem(ctx, widget); err != nil {
		t.Fatal(err)
	}
	other, err := s.GetHandoffReviewItem(ctx, "gadget-card", "k-7-build")
	if err != nil {
		t.Fatal(err)
	}
	if other.Answer != "" || other.Posted || other.Closed || other.Repo != "acme/gadget" {
		t.Fatalf("the other loop's item changed: %+v", other)
	}
}

// Findings are their loop's too: a ruling or a comment ID recorded on one
// loop's finding leaves another loop's finding of the same ID alone.
func TestReviewFindingsWithTheSameIDBelongToTheirOwnLoop(t *testing.T) {
	s := twoReviewLoops(t)
	ctx := context.Background()
	for _, task := range []string{"widget-card", "gadget-card"} {
		f := HandoffReviewFinding{ID: "f-1", TaskID: task, Pass: 1, PR: 7, Severity: "blocking", Title: "No bound " + task, Body: "b", Status: "open"}
		if err := s.ReplaceHandoffReviewFindings(ctx, task, 1, []HandoffReviewFinding{f}); err != nil {
			t.Fatalf("%s: %v", task, err)
		}
	}
	if err := s.RuleHandoffReviewFinding(ctx, "widget-card", "f-1", "resolved", 2, "bounded"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHandoffReviewFindingComment(ctx, "widget-card", "f-1", 501); err != nil {
		t.Fatal(err)
	}

	widget, _ := s.ListHandoffReviewFindings(ctx, "widget-card")
	gadget, _ := s.ListHandoffReviewFindings(ctx, "gadget-card")
	if len(widget) != 1 || widget[0].Status != "resolved" || widget[0].CommentID != 501 {
		t.Fatalf("widget's findings = %+v", widget)
	}
	if len(gadget) != 1 || gadget[0].Status != "open" || gadget[0].CommentID != 0 || gadget[0].Title != "No bound gadget-card" {
		t.Fatalf("gadget's findings = %+v", gadget)
	}
}

// openBeforeReviewItemsPerLoop opens a database migrated through the
// migration that keys review items by their loop, with seed run on it
// first. It stops there: the conversion later copies the old tables onto
// the loop record and a later migration drops them.
func openBeforeReviewItemsPerLoop(t *testing.T, seed string) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "before-items-per-loop.db")
	at := slices.IndexFunc(migrations, func(name string) bool { return strings.HasSuffix(name, "_review_items_per_loop.sql") })
	if at < 1 {
		t.Fatal("no review_items_per_loop migration")
	}
	before := newStoreAtMigration(t, path, migrations[at-1])
	if _, err := before.db.Exec(seed); err != nil {
		t.Fatal(err)
	}
	if err := before.Close(); err != nil {
		t.Fatal(err)
	}
	s := newStoreAtMigration(t, path, migrations[at])
	t.Cleanup(func() { s.Close() })
	return s
}

func TestKeyingReviewItemsByLoopKeepsEveryRow(t *testing.T) {
	s := openBeforeReviewItemsPerLoop(t, `
		INSERT INTO tasks (id, title, status) VALUES ('card', 'Card', 'Doing');
		INSERT INTO handoff_review_findings (id, task_id, pass, pr, severity, title, body, comment_id, status, ruled_pass, ruling)
			VALUES ('f-1', 'card', 1, 7, 'blocking', 'No bound', 'loops forever', 501, 'resolved', 2, 'bounded');
		INSERT INTO handoff_review_items (id, task_id, kind, pass, repo, pr, version, title, answer, posted, created_at, delivered)
			VALUES ('k-7-build', 'card', 'check', 2, 'acme/widget', 7, 3, 'Check build: failure', 'fixed', 1, '2024-01-01T00:00:00Z', 1);
	`)
	var findings []HandoffReviewFinding
	if err := s.db.Select(&findings, `SELECT * FROM handoff_review_findings WHERE task_id = 'card'`); err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].ID != "f-1" || findings[0].CommentID != 501 || findings[0].Status != "resolved" || findings[0].Ruling != "bounded" {
		t.Fatalf("findings = %+v", findings)
	}
	var item HandoffReviewItem
	if err := s.db.Get(&item, `SELECT * FROM handoff_review_items WHERE task_id = 'card' AND id = 'k-7-build'`); err != nil {
		t.Fatal(err)
	}
	if item.Version != 3 || item.Answer != "fixed" || !item.Posted || !item.Delivered || item.Repo != "acme/widget" {
		t.Fatalf("item = %+v", item)
	}
}

// A handoff that ended mid-pass left its loop reviewing, holding a reviewer
// slot for good: the migration ends such loops and sets their pass aside,
// and leaves open handoffs' loops and workflow runs' loops alone.
func TestKeyingReviewItemsByLoopEndsLoopsOfEndedHandoffs(t *testing.T) {
	s := openBeforeReviewItemsPerLoop(t, `
		INSERT INTO tasks (id, title, status) VALUES ('recalled', 'R', 'Review'), ('returning', 'T', 'Review'), ('open', 'O', 'Review'), ('run', 'W', 'Doing');
		INSERT INTO handoffs (main_instance_id, handoff_id, main_name, main_task_id, task_id, purpose, kind, repo, branch, agent_kind, state, request_hash, created_at, updated_at, closed_at)
			VALUES ('m', 'h-1', 'main', 'mt-1', 'recalled', 'make_change', 'local', 'acme/widget', 'b', 'codex', 'returning', 'x', 'now', 'now', 'now'),
			       ('m', 'h-2', 'main', 'mt-2', 'returning', 'make_change', 'local', 'acme/widget', 'b', 'codex', 'returning', 'x', 'now', 'now', NULL),
			       ('m', 'h-3', 'main', 'mt-3', 'open', 'make_change', 'local', 'acme/widget', 'b', 'codex', 'in_progress', 'x', 'now', 'now', NULL);
		INSERT INTO handoff_reviews (task_id, phase, pass, updated_at) VALUES
			('recalled', 'reviewing', 1, 'now'), ('returning', 'waiting_for_reviewer', 1, 'now'), ('open', 'reviewing', 1, 'now'), ('run', 'reviewing', 1, 'now');
		INSERT INTO handoff_review_passes (task_id, pass, state, started_at) VALUES
			('recalled', 1, 'reviewing', 'now'), ('returning', 1, 'posted', 'now'), ('open', 1, 'reviewing', 'now'), ('run', 1, 'reviewing', 'now');
	`)
	for task, want := range map[string][2]string{
		"recalled":  {"ended", "discarded"},
		"returning": {"ended", "posted"},
		"open":      {"reviewing", "reviewing"},
		"run":       {"reviewing", "reviewing"},
	} {
		var phase, state string
		if err := s.db.Get(&phase, `SELECT phase FROM handoff_reviews WHERE task_id = ?`, task); err != nil {
			t.Fatal(err)
		}
		if err := s.db.Get(&state, `SELECT state FROM handoff_review_passes WHERE task_id = ? AND pass = 1`, task); err != nil {
			t.Fatal(err)
		}
		if phase != want[0] || state != want[1] {
			t.Errorf("%s: loop %q, pass %q; want %q, %q", task, phase, state, want[0], want[1])
		}
	}
}

// handedOffCard is a card a main handed off to this worker, its handoff open.
func handedOffCard(t *testing.T, s *Store, card string) {
	t.Helper()
	h := Handoff{MainInstanceID: "main-1", HandoffID: "h-" + card, MainName: "main", MainTaskID: "mt-" + card, TaskID: card,
		Purpose: "make_change", Kind: "local", Repo: "acme/widget", Branch: "feat", AgentKind: "codex", State: HandoffInProgress,
		RequestHash: "x", CreatedAt: "2026-11-01T09:00:00Z", UpdatedAt: "2026-11-01T09:00:00Z"}
	if err := s.CreateHandoffCard(context.Background(), Task{ID: card, Title: card, Status: "Doing", CreatedAt: "2024-01-01", UpdatedAt: "2024-01-01"}, nil, h); err != nil {
		t.Fatal(err)
	}
}

// A handed-off card's loop, made on its first write, is authored by the
// card's agent, as a loop converted from before the loop record is: so an
// agent-authored run by the card is refused alike on a new and an upgraded
// database.
func TestAHandedOffCardsLoopIsAuthoredByTheCardsAgent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	handedOffCard(t, s, "card")
	if err := s.SaveHandoffReview(ctx, HandoffReview{TaskID: "card", Phase: "ready", UpdatedAt: loopAt}); err != nil {
		t.Fatal(err)
	}
	loop, err := s.CurrentReviewLoop(ctx, "card")
	if err != nil || loop == nil {
		t.Fatalf("loop = %+v, %v", loop, err)
	}
	if !loop.AgentAuthor || loop.AuthorTask != "card" || loop.LinkedTask != "card" || loop.HandoffID != "h-card" || loop.MainInstanceID != "main-1" {
		t.Fatalf("the card's loop = %+v; want it authored by the card's agent and owned by its handoff", loop)
	}

	run := runLoop(t, s, "run-1", "acme/widget", 12)
	run.Loop.AgentAuthor, run.Loop.AuthorTask = true, "card"
	_, _, err = s.OpenReviewLoop(ctx, run)
	var conflict *ReviewLoopConflictError
	if !errors.Is(err, ErrAuthorLoopActive) || !errors.As(err, &conflict) || conflict.LoopID != loop.ID {
		t.Fatalf("an agent-authored run by the card: %v; want it refused naming %s", err, loop.ID)
	}
}

// A card whose agent already authors a run's open loop still gets its
// handoff's loop on its first write: the handoff reviews as it did before
// the loop record, and its loop is not counted as the agent's.
func TestAHandedOffCardsLoopIsMadeWhileItsAgentAuthorsARunsLoop(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	handedOffCard(t, s, "card")
	run := runLoop(t, s, "run-1", "acme/widget", 12)
	run.Loop.AgentAuthor, run.Loop.AuthorTask = true, "card"
	mustOpen(t, s, run)
	if err := s.SaveHandoffReview(ctx, HandoffReview{TaskID: "card", Phase: "ready", UpdatedAt: loopAt}); err != nil {
		t.Fatal(err)
	}
	loop, err := s.CurrentReviewLoop(ctx, "card")
	if err != nil || loop == nil || loop.HandoffID != "h-card" || loop.AgentAuthor {
		t.Fatalf("loop = %+v, %v", loop, err)
	}
}
