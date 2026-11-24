package handofftest

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The release that brings the loop record, end to end (LEG-606): a worker
// an older Legato ran, with a handoff's loop and a review-pr-passes run's
// loop both mid-review, is upgraded. The conversion copies both onto the
// loop record and drops the old tables; each loop then finishes as it
// would have, every pass posted once.

// asOlderLegato stops the worker and puts back its database as a Legato
// from before the loop record kept it: the schema before the conversion,
// every row of the tables both schemas have (the loop record's aside:
// nothing wrote it), and each handed-off card's
// review loop in handoff_review*, as the engine reads it now. A run's loop
// is seeded on top (olderLegatoMidReview). Restart then upgrades it.
func asOlderLegato(t *testing.T, w *Worker) (runID, runTask string) {
	t.Helper()
	ctx := context.Background()
	w.Stop()
	now, err := store.New(w.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	reviews, err := now.ListHandoffReviews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	type loopRows struct {
		loop     store.HandoffReview
		passes   []store.HandoffReviewPass
		prs      []store.HandoffReviewPR
		findings []store.HandoffReviewFinding
		items    []store.HandoffReviewItem
	}
	var loops []loopRows
	for _, review := range reviews {
		rows := loopRows{loop: review}
		if rows.passes, err = now.ListHandoffReviewPasses(ctx, review.TaskID); err != nil {
			t.Fatal(err)
		}
		for _, p := range rows.passes {
			prs, err := now.ListHandoffReviewPRs(ctx, review.TaskID, p.Pass)
			if err != nil {
				t.Fatal(err)
			}
			rows.prs = append(rows.prs, prs...)
		}
		if rows.findings, err = now.ListHandoffReviewFindings(ctx, review.TaskID); err != nil {
			t.Fatal(err)
		}
		if rows.items, err = now.ListHandoffReviewItems(ctx, review.TaskID); err != nil {
			t.Fatal(err)
		}
		loops = append(loops, rows)
	}
	if err := now.Close(); err != nil {
		t.Fatal(err)
	}

	older := filepath.Join(t.TempDir(), "older.db")
	db, err := store.NewBefore(older, "_review_loop_conversion.sql")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", strings.Fields(query)[0], err)
		}
	}
	exec(`PRAGMA foreign_keys = OFF`)
	exec(`ATTACH DATABASE ? AS upgraded`, w.DBPath)
	tables := names(t, conn, `SELECT name FROM main.sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		AND name != 'schema_migrations' AND name NOT LIKE 'handoff_review%' AND name NOT LIKE 'review_loop%'
		AND name IN (SELECT name FROM upgraded.sqlite_master WHERE type = 'table')`)
	for _, table := range tables {
		columns := names(t, conn, `SELECT name FROM pragma_table_info(?) WHERE name IN (SELECT name FROM pragma_table_info(?, 'upgraded'))`, table, table)
		list := `"` + strings.Join(columns, `", "`) + `"`
		exec(fmt.Sprintf(`DELETE FROM main.%q`, table))
		exec(fmt.Sprintf(`INSERT INTO main.%q (%s) SELECT %s FROM upgraded.%q`, table, list, list, table))
	}
	// AUTOINCREMENT counters too, so an ID a removed row had is not given
	// out again.
	exec(`DELETE FROM main.sqlite_sequence`)
	exec(`INSERT INTO main.sqlite_sequence (name, seq) SELECT name, seq FROM upgraded.sqlite_sequence`)
	exec(`DETACH DATABASE upgraded`)
	exec(`PRAGMA foreign_keys = ON`)
	conn.Close()

	tx := db.DB()
	for _, rows := range loops {
		if rows.loop.Settings == "" {
			rows.loop.Settings = "{}"
		}
		if _, err := tx.NamedExecContext(ctx, `INSERT INTO handoff_reviews (task_id, settings, phase, pass, mismatches, nudged_at, author_busy_since_nudge,
			reason, queued_at, updated_at, delivered_pass, polled_at, last_new_at, skipped_comments, checks_polled_at, checks_heads, checks_pending)
			VALUES (:task_id, :settings, :phase, :pass, :mismatches, :nudged_at, :author_busy_since_nudge, :reason, :queued_at, :updated_at,
			:delivered_pass, :polled_at, :last_new_at, :skipped_comments, :checks_polled_at, :checks_heads, :checks_pending)`, rows.loop); err != nil {
			t.Fatal(err)
		}
		for _, p := range rows.passes {
			if _, err := tx.NamedExecContext(ctx, `INSERT INTO handoff_review_passes (task_id, pass, state, verdict, summary, reason, execution_id,
				checkout, head_sha, started_at, ended_at, attempt, posting_started_at, post_attempts)
				VALUES (:task_id, :pass, :state, :verdict, :summary, :reason, :execution_id, :checkout, :head_sha, :started_at, :ended_at,
				:attempt, :posting_started_at, :post_attempts)`, p); err != nil {
				t.Fatal(err)
			}
		}
		for _, pr := range rows.prs {
			if _, err := tx.NamedExecContext(ctx, `INSERT INTO handoff_review_prs (task_id, pass, repo, number, branch, base, title, body, url,
				head_sha, review_id, review_url, author, event, local_branch, head_repo)
				VALUES (:task_id, :pass, :repo, :number, :branch, :base, :title, :body, :url, :head_sha, :review_id, :review_url, :author,
				:event, :local_branch, :head_repo)`, pr); err != nil {
				t.Fatal(err)
			}
		}
		for _, f := range rows.findings {
			if _, err := tx.NamedExecContext(ctx, `INSERT INTO handoff_review_findings (id, task_id, pass, pr, severity, title, body, path, line,
				start_line, side, comment_id, status, ruled_pass, ruling)
				VALUES (:id, :task_id, :pass, :pr, :severity, :title, :body, :path, :line, :start_line, :side, :comment_id, :status,
				:ruled_pass, :ruling)`, f); err != nil {
				t.Fatal(err)
			}
		}
		for _, item := range rows.items {
			if _, err := tx.NamedExecContext(ctx, `INSERT INTO handoff_review_items (id, task_id, kind, pass, repo, pr, path, line, url, version,
				outdated, author, title, body, thread_comment_id, source_id, answer, answer_note, answered_at, posted, answer_marker, closed,
				created_at, delivered)
				VALUES (:id, :task_id, :kind, :pass, :repo, :pr, :path, :line, :url, :version, :outdated, :author, :title, :body,
				:thread_comment_id, :source_id, :answer, :answer_note, :answered_at, :posted, :answer_marker, :closed, :created_at,
				:delivered)`, item); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	runID, runTask = olderLegatoMidReview(t, older)

	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(w.DBPath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if err := os.Rename(older, w.DBPath); err != nil {
		t.Fatal(err)
	}
	return runID, runTask
}

func names(t *testing.T, conn *sql.Conn, query string, args ...any) []string {
	t.Helper()
	rows, err := conn.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAHandoffLoopAndARunLoopMidReviewFinishOnTheLoopRecordAfterTheUpgrade(t *testing.T) {
	ctx := context.Background()
	// The older Legato: the handoff's pass 1 posted one blocking finding on
	// #7, and its author was told.
	h := newReviewed(t, ReviewPassOptions{})
	finding := changesPass(t, h, "The loop has no bound.")
	if posted := h.worker.ReviewGitHub.Posted(); len(posted) != 1 {
		t.Fatalf("before the upgrade: posted = %+v", posted)
	}
	runID, runTask := asOlderLegato(t, h.worker)

	h.worker.Restart()
	h.worker.ReviewGitHub.OpenPR(12, "feature/greeting", "bbb222")
	h.worker.Checkout.File("greet.go", 30)
	r := reviewRun{worker: h.worker, runs: StartReviewRuns(t, h.worker)}

	for _, table := range []string{"handoff_reviews", "handoff_review_passes", "handoff_review_items"} {
		var n int
		if err := h.worker.Store.DB().GetContext(ctx, &n, `SELECT COUNT(*) FROM sqlite_master WHERE name = ?`, table); err != nil || n != 0 {
			t.Fatalf("%s is still there after the upgrade (%v)", table, err)
		}
	}
	handoffLoop, err := h.worker.Store.CurrentReviewLoop(ctx, h.card)
	if err != nil || handoffLoop == nil || handoffLoop.OwnerKind != store.ReviewLoopOwnerHandoff || handoffLoop.MarkerKey != h.card ||
		handoffLoop.State != store.ReviewLoopOpenState || handoffLoop.Phase != service.ReviewPhaseWaitingForAuthor {
		t.Fatalf("the handoff's converted loop = %+v, %v", handoffLoop, err)
	}
	runLoop, err := h.worker.Store.CurrentReviewLoop(ctx, runTask)
	if err != nil || runLoop == nil || runLoop.RunID != runID || runLoop.State != store.ReviewLoopOpenState {
		t.Fatalf("the run's converted loop = %+v, %v", runLoop, err)
	}

	// The handoff's author fixes it and is done again: pass 2 approves.
	h.answer(t, finding, service.AnswerFixed, "bounded the retries")
	h.worker.Checkout.Commit(h.branch, "bbb222")
	h.worker.ReviewGitHub.OpenPR(7, h.branch, "bbb222")
	h.tick()
	h.finish(t)
	if loop := h.loop(t); loop.Pass != 2 || loop.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("the handoff's loop after the author's push = %+v", loop)
	}
	if _, err := h.submit(t, service.ReviewSubmission{Summary: "Fixed.", Rulings: []service.SubmittedRuling{{Finding: finding, Status: "resolved", Reason: "the loop is bounded"}}}); err != nil {
		t.Fatal(err)
	}
	if h.handoffState(t) != store.HandoffDone || h.loop(t).Phase != service.ReviewPhaseClean {
		t.Fatalf("handoff %s, loop %+v; want it done after a clean pass", h.handoffState(t), h.loop(t))
	}

	// The run's author pushes: pass 2 approves and the run succeeds.
	h.worker.ReviewGitHub.Push(12, "ccc333")
	h.worker.Checkout.Change("ccc333", "greet.go", 3, 5)
	r.tick()
	h.worker.Clock.Advance(3 * time.Minute)
	r.tick()
	if got := r.loopPhase(t, runTask); !r.reviewer(t, runTask) || got.Pass != 2 || got.Phase != service.ReviewPhaseReviewing {
		t.Fatalf("the run's loop after the push = %+v", got)
	}
	r.submit(t, runTask, service.ReviewSubmission{Summary: "Fixed.", Rulings: []service.SubmittedRuling{{Finding: "f-1", Status: "resolved", Reason: "guarded"}}})
	if view := r.show(t, runID); view.Run.Status != "succeeded" {
		t.Fatalf("run %q: %s", view.Run.Status, view.Run.Error)
	}

	// Every pass was posted once: the handoff's two and the run's second
	// (its first was posted before the upgrade, by the older Legato).
	posted := h.worker.ReviewGitHub.Posted()
	want := []string{h.card + "/pass-1/pr-7", h.card + "/pass-2/pr-7", runTask + "/pass-2/pr-12"}
	if len(posted) != len(want) {
		t.Fatalf("posted = %+v; want %v", posted, want)
	}
	for i, p := range posted {
		if p.Marker != want[i] {
			t.Fatalf("post %d is %s; want %v", i, p.Marker, want)
		}
	}
	if posted[1].Event != service.ReviewApprove || posted[2].Event != service.ReviewApprove {
		t.Fatalf("posted = %+v; want both second passes to approve", posted)
	}
	if loop, _ := h.worker.Store.CurrentReviewLoop(ctx, runTask); loop.State != store.ReviewLoopClosedState || loop.Outcome != service.ReviewOutcomeApproved {
		t.Fatalf("the run's loop = %+v; want it closed approved", loop)
	}
	if held, _ := h.worker.Store.ActiveReviewLoop(ctx, "acme/widget", 12); held != nil {
		t.Fatalf("#12 is still claimed by %s", held.ID)
	}
}
