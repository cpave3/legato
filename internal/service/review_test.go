package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// reviewFixture wires a ReviewService against a real SQLite store and a real
// git repo acting as the task's worktree.
type reviewFixture struct {
	svc   *ReviewService
	store *store.Store
	tmux  *mockTmux
	repo  string
}

func newReviewFixture(t *testing.T) *reviewFixture {
	t.Helper()
	s := newReviewTestStore(t)
	repo := initTestRepo(t)

	createTask(t, s, "task-1")
	if err := s.SetTaskWorktree(context.Background(), "task-1", &store.TaskWorktree{
		PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}

	tmux := newMockTmux()
	return &reviewFixture{svc: NewReviewService(s, tmux, nil), store: s, tmux: tmux, repo: repo}
}

func newReviewTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// initTestRepo builds a repo with one commit on main and checks out a feature
// branch, mirroring the worktree layout legato creates.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	writeRepoFile(t, dir, "README.md", "hello\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "initial")
	gitRun(t, dir, "checkout", "-b", "feature")
	return dir
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitCommitAll(t *testing.T, dir, message string) string {
	t.Helper()
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", message)
	return gitRun(t, dir, "rev-parse", "HEAD")
}

func TestReviewSyncWithoutWorktreeUsesRecordedRepository(t *testing.T) {
	s := newReviewTestStore(t)
	repo := initTestRepo(t)
	createTask(t, s, "task-1")
	base := gitRun(t, repo, "rev-parse", "HEAD")
	if _, err := s.EnsureReviewTour(context.Background(), "task-1"); err != nil {
		t.Fatal(err)
	}
	svc := NewReviewService(s, nil, nil)
	if err := svc.BeginCapture(context.Background(), "task-1", repo); err != nil {
		t.Fatal(err)
	}
	tour, err := s.GetReviewTour(context.Background(), "task-1")
	if err != nil || tour.BaseSHA != base || tour.RepositoryPath != repo {
		t.Fatalf("tour = %+v, err = %v", tour, err)
	}
	writeRepoFile(t, repo, "plain.go", "package plain\n")
	gitCommitAll(t, repo, "plain repository commit")

	view, err := svc.Tour(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Steps) != 1 || view.Steps[0].Title != "plain repository commit" {
		t.Fatalf("steps = %+v", view.Steps)
	}
	if view.Messages == nil {
		t.Fatal("empty transcript must be an empty slice, not nil")
	}
}

func TestReviewSyncDirtyStepLifecycle(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	// Uncommitted work produces a dirty step, sorted last.
	writeRepoFile(t, f.repo, "wip.go", "package wip\n")
	if err := f.svc.Sync(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	steps, _ := f.store.ListReviewSteps(ctx, "task-1")
	if len(steps) != 1 || steps[0].Kind != "dirty" {
		t.Fatalf("steps = %+v, want single dirty step", steps)
	}
	dirtyID := steps[0].ID
	firstFingerprint := steps[0].DirtyFingerprint

	// Reviewing it, then changing the worktree, clears the reviewed mark.
	if err := f.svc.SetReviewed(ctx, "task-1", dirtyID, true); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, f.repo, "wip.go", "package wip // changed\n")
	if err := f.svc.Sync(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	step, _ := f.store.GetReviewStep(ctx, dirtyID)
	if step.DirtyFingerprint == firstFingerprint {
		t.Fatal("fingerprint should change when dirty content changes")
	}
	if step.ReviewedAt != nil {
		t.Fatal("changed dirty work must need re-review")
	}

	// Committing everything removes the dirty step.
	gitCommitAll(t, f.repo, "commit the wip")
	if err := f.svc.Sync(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	steps, _ = f.store.ListReviewSteps(ctx, "task-1")
	for _, s := range steps {
		if s.Kind == "dirty" {
			t.Fatalf("dirty step should be removed when worktree is clean: %+v", s)
		}
	}
}

func TestSetReviewedSyncsDirtyFingerprintBeforeMutation(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "README.md", "changed\n")
	if err := f.svc.Sync(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	steps, err := f.store.ListReviewSteps(ctx, "task-1")
	if err != nil || len(steps) != 1 {
		t.Fatalf("steps = %+v, err = %v", steps, err)
	}
	initialFingerprint := steps[0].DirtyFingerprint

	gitRun(t, f.repo, "add", "README.md")
	if err := f.svc.SetReviewed(ctx, "task-1", steps[0].ID, true); err != nil {
		t.Fatal(err)
	}
	step, err := f.store.GetReviewStep(ctx, steps[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if step.DirtyFingerprint == initialFingerprint {
		t.Fatal("staging the same diff must change the dirty fingerprint")
	}
	if step.ReviewedAt == nil {
		t.Fatal("SetReviewed must apply after syncing the changed worktree")
	}
}

func TestReviewSyncOrphansRewrittenCommits(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	oldSHA := gitCommitAll(t, f.repo, "add a")
	if err := f.svc.Sync(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}

	gitRun(t, f.repo, "commit", "--amend", "-m", "add a (amended)")
	newSHA := gitRun(t, f.repo, "rev-parse", "HEAD")
	if err := f.svc.Sync(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}

	steps, _ := f.store.ListReviewSteps(ctx, "task-1")
	bySHA := map[string]store.ReviewStep{}
	for _, s := range steps {
		bySHA[s.CommitSHA] = s
	}
	if bySHA[oldSHA].OrphanedAt == nil {
		t.Fatalf("old SHA should be orphaned: %+v", bySHA[oldSHA])
	}
	if _, ok := bySHA[newSHA]; !ok {
		t.Fatalf("amended SHA should have a new step: %+v", steps)
	}
	if bySHA[newSHA].OrphanedAt != nil {
		t.Fatal("new SHA must not be orphaned")
	}
}

func TestReviewReadyCompleteAndWatermarkReentry(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	writeRepoFile(t, f.repo, "b.go", "package b\n")
	lastSHA := gitCommitAll(t, f.repo, "add b")

	if err := f.svc.Ready(ctx, "task-1", "feature done"); err != nil {
		t.Fatal(err)
	}
	tour, _ := f.store.GetReviewTour(ctx, "task-1")
	if tour.Status != "ready" || tour.Summary != "feature done" || tour.ReadyAt == nil {
		t.Fatalf("tour after Ready = %+v", tour)
	}

	if err := f.svc.Complete(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	tour, _ = f.store.GetReviewTour(ctx, "task-1")
	if tour.Status != "reviewed" {
		t.Fatalf("Status = %q, want reviewed", tour.Status)
	}
	if tour.LastReviewedSHA != lastSHA {
		t.Fatalf("watermark = %q, want %q", tour.LastReviewedSHA, lastSHA)
	}
	steps, _ := f.store.ListReviewSteps(ctx, "task-1")
	for _, s := range steps {
		if s.ReviewedAt == nil {
			t.Fatalf("Complete must stamp all steps reviewed: %+v", s)
		}
	}

	// New work past the watermark re-queues the tour.
	writeRepoFile(t, f.repo, "c.go", "package c\n")
	gitCommitAll(t, f.repo, "add c")
	if err := f.svc.Sync(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	tour, _ = f.store.GetReviewTour(ctx, "task-1")
	if tour.Status != "ready" {
		t.Fatalf("Status after new commit = %q, want ready (re-entry)", tour.Status)
	}
}

func TestReviewAnnotate(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	sha1 := gitCommitAll(t, f.repo, "add a\n\noriginal body")
	writeRepoFile(t, f.repo, "b.go", "package b\n")
	gitCommitAll(t, f.repo, "add b")

	// Default anchor is HEAD; risk and order hint stick.
	two := 2
	stepID, err := f.svc.Annotate(ctx, "task-1", AnnotateArgs{Text: "tricky bit, review carefully", Risk: "high", OrderHint: &two})
	if err != nil {
		t.Fatal(err)
	}
	head, _ := f.store.GetReviewStep(ctx, stepID)
	if head.Title != "add b" {
		t.Fatalf("default anchor should be HEAD commit, got %q", head.Title)
	}
	if head.Risk != "high" || head.OrderHint == nil || *head.OrderHint != 2 {
		t.Fatalf("annotation not applied: %+v", head)
	}

	// Explicit SHA anchor appends to seeded narration.
	if _, err := f.svc.Annotate(ctx, "task-1", AnnotateArgs{SHA: sha1, Text: "extra context"}); err != nil {
		t.Fatal(err)
	}
	steps, _ := f.store.ListReviewSteps(ctx, "task-1")
	var first store.ReviewStep
	for _, s := range steps {
		if s.CommitSHA == sha1 {
			first = s
		}
	}
	if !strings.Contains(first.Narration, "original body") || !strings.Contains(first.Narration, "extra context") {
		t.Fatalf("Narration = %q, want append", first.Narration)
	}

	// --file with no SHA creates a note step.
	noteID, err := f.svc.Annotate(ctx, "task-1", AnnotateArgs{Text: "shared helper touched", Files: []string{"a.go", "b.go"}})
	if err != nil {
		t.Fatal(err)
	}
	note, _ := f.store.GetReviewStep(ctx, noteID)
	if note.Kind != "note" || !strings.Contains(note.Files, "a.go") {
		t.Fatalf("note step = %+v", note)
	}

	// Unknown SHA errors clearly.
	if _, err := f.svc.Annotate(ctx, "task-1", AnnotateArgs{SHA: "deadbeef", Text: "x"}); err == nil {
		t.Fatal("unknown sha must error")
	}
}

func TestReviewAnnotateHunkPersistsDurableNote(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "old.go", "package old\n\nfunc value() int { return 1 }\n")
	gitCommitAll(t, f.repo, "add old")
	gitRun(t, f.repo, "mv", "old.go", "new.go")
	writeRepoFile(t, f.repo, "new.go", "package old\n\nfunc value() int { return 2 }\n")
	sha := gitCommitAll(t, f.repo, "rename and change")

	hunk := 1
	noteID, err := f.svc.Annotate(ctx, "task-1", AnnotateArgs{
		SHA: sha, Text: "check this hunk", Files: []string{"old.go"}, Hunk: &hunk,
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.Tour(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.HunkNotes) != 1 {
		t.Fatalf("HunkNotes = %+v", view.HunkNotes)
	}
	note := view.HunkNotes[0]
	if note.ID != noteID || note.FilePath != "old.go" || note.HunkAnchor == "" || note.Body != "check this hunk" {
		t.Fatalf("note = %+v", note)
	}
}

func TestReviewAnnotateHunkRejectsInvalidSelection(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")

	one, two := 1, 2
	for _, tc := range []AnnotateArgs{
		{Text: "x", Files: []string{"missing.go"}, Hunk: &one},
		{Text: "x", Files: []string{"a.go"}, Hunk: &two},
		{Text: "x", Files: []string{"a.go", "b.go"}, Hunk: &one},
	} {
		if _, err := f.svc.Annotate(ctx, "task-1", tc); err == nil {
			t.Fatalf("Annotate(%+v) should fail", tc)
		}
	}
}

func TestReviewTourHunkNotesIsNonNil(t *testing.T) {
	f := newReviewFixture(t)
	view, err := f.svc.Tour(context.Background(), "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if view.HunkNotes == nil {
		t.Fatal("empty hunk notes must be an empty slice, not nil")
	}
}

func TestReviewQuestionAndAnswerLoop(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	if err := f.svc.Sync(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	steps, _ := f.store.ListReviewSteps(ctx, "task-1")
	stepID := steps[0].ID

	// Live agent session: question is delivered into its pane.
	if err := f.tmux.Spawn("legato-task-1", f.repo, 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := f.store.InsertAgentSession(ctx, store.AgentSession{
		TaskID: "task-1", TmuxSession: "legato-task-1", Command: "chimera",
		Status: "running", StartedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.AskQuestion(ctx, "task-1", stepID, "why not reuse the helper?"); err != nil {
		t.Fatal(err)
	}
	lines := f.tmux.sentLines["legato-task-1"]
	if len(lines) != 1 {
		t.Fatalf("sent lines = %v, want 1", lines)
	}
	if !strings.Contains(lines[0], "[legato review]") ||
		!strings.Contains(lines[0], stepID) ||
		!strings.Contains(lines[0], "legato review answer") ||
		!strings.Contains(lines[0], "why not reuse the helper?") {
		t.Fatalf("question line = %q", lines[0])
	}
	msgs, _ := f.store.ListReviewMessages(ctx, "task-1")
	if len(msgs) != 1 || msgs[0].Kind != "question" || msgs[0].DeliveredAt == nil {
		t.Fatalf("transcript = %+v", msgs)
	}

	// Agent answers via the CLI verb path (prefix resolution included).
	if err := f.svc.Answer(ctx, "task-1", stepID[:6], "the helper assumes X"); err != nil {
		t.Fatal(err)
	}
	msgs, _ = f.store.ListReviewMessages(ctx, "task-1")
	if len(msgs) != 2 || msgs[1].Kind != "answer" || msgs[1].Author != "agent" {
		t.Fatalf("transcript after answer = %+v", msgs)
	}

	// Dead session: question stored undelivered, typed error returned.
	if err := f.tmux.Kill("legato-task-1"); err != nil {
		t.Fatal(err)
	}
	err := f.svc.AskQuestion(ctx, "task-1", stepID, "still there?")
	if !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("err = %v, want ErrAgentOffline", err)
	}
	msgs, _ = f.store.ListReviewMessages(ctx, "task-1")
	if len(msgs) != 3 || msgs[2].DeliveredAt != nil {
		t.Fatalf("offline question should be stored undelivered: %+v", msgs)
	}
}

func TestReviewMessagesSyncBeforeMutatingTranscript(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	if err := f.svc.Sync(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	steps, _ := f.store.ListReviewSteps(ctx, "task-1")
	anchorID := steps[0].ID

	writeRepoFile(t, f.repo, "b.go", "package b\n")
	gitCommitAll(t, f.repo, "add b")
	if err := f.svc.AskQuestion(ctx, "task-1", anchorID, "question"); !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("AskQuestion err = %v, want ErrAgentOffline", err)
	}
	steps, _ = f.store.ListReviewSteps(ctx, "task-1")
	if len(steps) != 2 {
		t.Fatalf("AskQuestion must sync first; steps = %+v", steps)
	}

	writeRepoFile(t, f.repo, "c.go", "package c\n")
	gitCommitAll(t, f.repo, "add c")
	if err := f.svc.Answer(ctx, "task-1", anchorID, "answer"); err != nil {
		t.Fatal(err)
	}
	steps, _ = f.store.ListReviewSteps(ctx, "task-1")
	if len(steps) != 3 {
		t.Fatalf("Answer must sync first; steps = %+v", steps)
	}
}

func TestReviewTourAndStepDiff(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	writeRepoFile(t, f.repo, "wip.go", "package wip\n")

	view, err := f.svc.Tour(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if view.Tour.TaskID != "task-1" || len(view.Steps) != 2 {
		t.Fatalf("view = %+v", view)
	}

	// Commit step diff parses to the committed file.
	commitDiff, err := f.svc.StepDiff(ctx, "task-1", view.Steps[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(commitDiff) != 1 || commitDiff[0].NewPath != "a.go" {
		t.Fatalf("commit diff = %+v", commitDiff)
	}

	// Dirty step diff shows the uncommitted file.
	dirtyDiff, err := f.svc.StepDiff(ctx, "task-1", view.Steps[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(dirtyDiff) != 1 || dirtyDiff[0].NewPath != "wip.go" {
		t.Fatalf("dirty diff = %+v", dirtyDiff)
	}
}

func TestReviewQueue(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	if err := f.svc.Ready(ctx, "task-1", "done"); err != nil {
		t.Fatal(err)
	}

	items, err := f.svc.Queue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("queue = %+v, want 1 item", items)
	}
	if items[0].TaskID != "task-1" || items[0].Unreviewed != 1 || items[0].Title == "" {
		t.Fatalf("item = %+v", items[0])
	}

	// Completing the review empties the queue.
	if err := f.svc.Complete(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	items, _ = f.svc.Queue(ctx)
	if len(items) != 0 {
		t.Fatalf("queue after complete = %+v", items)
	}
}

func TestReviewQueueSyncsToursAndSkipsBrokenWorktrees(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	if err := f.svc.Complete(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, f.repo, "b.go", "package b\n")
	gitCommitAll(t, f.repo, "add b")

	createTask(t, f.store, "task-broken")
	missing := filepath.Join(t.TempDir(), "missing")
	if err := f.store.SetTaskWorktree(ctx, "task-broken", &store.TaskWorktree{
		PrimaryDir: missing, Path: missing, Branch: "feature", BaseBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.EnsureReviewTour(ctx, "task-broken"); err != nil {
		t.Fatal(err)
	}

	items, err := f.svc.Queue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].TaskID != "task-1" || items[0].Unreviewed != 1 {
		t.Fatalf("queue = %+v, want newly synced valid tour only", items)
	}
}

func TestReviewQueueSurfacesAbandonedCapturingTours(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	// Agent synced steps but died before running `review ready`.
	if err := f.svc.Sync(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.InsertAgentSession(ctx, store.AgentSession{
		TaskID: "task-1", TmuxSession: "legato-task-1", Command: "chimera",
		Status: "running", StartedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	// Session is in the DB but the tmux session is not alive (mock has no spawn).

	items, err := f.svc.Queue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != "capturing" {
		t.Fatalf("queue = %+v, want the abandoned capturing tour", items)
	}

	// A live session keeps capturing tours out of the queue.
	if err := f.tmux.Spawn("legato-task-1", f.repo, 80, 24); err != nil {
		t.Fatal(err)
	}
	items, _ = f.svc.Queue(ctx)
	if len(items) != 0 {
		t.Fatalf("queue with live agent = %+v, want empty", items)
	}
}

func TestReviewBadgeStatesIncludesReadyTourWithZeroUnreviewed(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	if _, err := f.store.EnsureReviewTour(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateReviewTour(ctx, "task-1", func(rt *store.ReviewTour) {
		rt.Status = "ready"
	}); err != nil {
		t.Fatal(err)
	}

	states, err := f.svc.ReviewBadgeStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	state, ok := states["task-1"]
	if !ok || !state.Ready || state.Unreviewed != 0 {
		t.Fatalf("state = %+v, present = %v; want ready with zero unreviewed", state, ok)
	}
}

func TestReviewSyncSeedsStepsFromCommits(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	sha1 := gitCommitAll(t, f.repo, "add feature a\n\nDecided on approach X because Y.")
	writeRepoFile(t, f.repo, "b.go", "package b\n")
	sha2 := gitCommitAll(t, f.repo, "add feature b")

	if err := f.svc.Sync(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}

	steps, err := f.store.ListReviewSteps(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 {
		t.Fatalf("len(steps) = %d, want 2: %+v", len(steps), steps)
	}
	if steps[0].CommitSHA != sha1 || steps[1].CommitSHA != sha2 {
		t.Fatalf("step order: %q, %q; want %q, %q", steps[0].CommitSHA, steps[1].CommitSHA, sha1, sha2)
	}
	if steps[0].Title != "add feature a" {
		t.Fatalf("Title = %q", steps[0].Title)
	}
	if steps[0].Narration != "Decided on approach X because Y." {
		t.Fatalf("Narration = %q", steps[0].Narration)
	}
	if !strings.HasPrefix(steps[0].ID, "rs-") {
		t.Fatalf("step ID = %q, want rs- prefix", steps[0].ID)
	}

	// Re-sync is idempotent.
	if err := f.svc.Sync(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	steps, _ = f.store.ListReviewSteps(ctx, "task-1")
	if len(steps) != 2 {
		t.Fatalf("re-sync duplicated steps: %d", len(steps))
	}
}
