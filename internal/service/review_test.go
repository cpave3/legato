package service

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// reviewFixture wires a ReviewService against a real SQLite store and a real
// git repo acting as the task's worktree.
type reviewFixture struct {
	svc    *ReviewService
	store  *store.Store
	tmux   *mockTmux
	repo   string
	tourID string
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
	tour, err := s.EnsureReviewTour(context.Background(), "task-1", "")
	if err != nil {
		t.Fatal(err)
	}
	return &reviewFixture{svc: NewReviewService(s, tmux, nil), store: s, tmux: tmux, repo: repo, tourID: tour.ID}
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

func TestReviewTourCreatesPassOneWithImmutableLatestPlanSnapshot(t *testing.T) {
	s := newReviewTestStore(t)
	ctx := context.Background()
	repo := initTestRepo(t)
	createTask(t, s, "task-1")
	if err := s.SetTaskWorktree(ctx, "task-1", &store.TaskWorktree{PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	insertApprovedReviewPlan(t, s, "task-1", "older", "# Older plan")
	insertApprovedReviewPlan(t, s, "task-1", "latest", "# Captured plan")

	svc := NewReviewService(s, nil, nil)
	tour, err := svc.EnsureReviewTour(ctx, "task-1", "")
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.Tour(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Passes) != 1 || view.Passes[0].Pass.Number != 1 {
		t.Fatalf("passes = %+v, want pass 1", view.Passes)
	}
	captured := view.Passes[0].CapturedPlan
	if captured == nil || captured.PlanID != "plan-latest" || captured.Revision != 1 || captured.Markdown != "# Captured plan" {
		t.Fatalf("captured plan = %+v", captured)
	}

	if err := s.InsertPlanRevision(ctx,
		store.Plan{ID: "plan-latest", TaskID: "task-1", Name: "latest", Title: "latest", Status: "proposed", LatestRevision: 2},
		store.PlanRevision{ID: "plan-latest-r2", PlanID: "plan-latest", Revision: 2, Markdown: "# Revised later", ManifestJSON: "{}"}, nil); err != nil {
		t.Fatal(err)
	}
	view, err = svc.Tour(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := view.Passes[0].CapturedPlan.Markdown; got != "# Captured plan" {
		t.Fatalf("captured markdown changed to %q", got)
	}
}

func insertApprovedReviewPlan(t *testing.T, s *store.Store, taskID, name, markdown string) {
	t.Helper()
	ctx := context.Background()
	id := "plan-" + name
	if err := s.InsertPlanRevision(ctx,
		store.Plan{ID: id, TaskID: taskID, Name: name, Title: name, Status: "proposed", LatestRevision: 1},
		store.PlanRevision{ID: id + "-r1", PlanID: id, Revision: 1, Markdown: markdown, ManifestJSON: "{}"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ApprovePlan(ctx, id, false); err != nil {
		t.Fatal(err)
	}
}

func TestReviewArtifactsBelongToActivePass(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "a.go", "package a\n")
	sha := gitCommitAll(t, f.repo, "add a")
	chapterID, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "Package A", Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hunk := 1
	noteID, err := f.svc.Annotate(ctx, f.tourID, AnnotateArgs{SHA: sha, Text: "note", Files: []string{"a.go"}, Hunk: &hunk})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.AskQuestion(ctx, f.tourID, chapterID, ReviewQuestion{Text: "why?"}); !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("AskQuestion error = %v, want ErrAgentOffline", err)
	}

	pass, err := f.store.GetActiveReviewPass(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	steps, _ := f.store.ListReviewSteps(ctx, f.tourID)
	for _, step := range steps {
		if step.PassID != pass.ID {
			t.Fatalf("step %s pass = %q, want %q", step.ID, step.PassID, pass.ID)
		}
	}
	note, err := f.store.GetReviewHunkNoteByPrefix(ctx, f.tourID, noteID)
	if err != nil || note.PassID != pass.ID {
		t.Fatalf("note = %+v, err = %v, want pass %q", note, err, pass.ID)
	}
	messages, err := f.store.ListReviewMessages(ctx, f.tourID)
	if err != nil || len(messages) != 1 || messages[0].PassID != pass.ID {
		t.Fatalf("messages = %+v, err = %v, want pass %q", messages, err, pass.ID)
	}
	hunks, err := f.store.ListReviewChapterHunks(ctx, chapterID)
	if err != nil || len(hunks) != 1 || hunks[0].PassID != pass.ID {
		t.Fatalf("chapter hunks = %+v, err = %v, want pass %q", hunks, err, pass.ID)
	}
}

func TestRestartReviewReplacesCurrentPass(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	before, err := f.store.GetActiveReviewPass(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.svc.Restart(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	passes, err := f.store.ListReviewPasses(ctx, f.tourID)
	if err != nil || len(passes) != 1 || passes[0].Number != 1 || passes[0].ID == before.ID {
		t.Fatalf("passes = %+v, err = %v; restart must replace pass 1", passes, err)
	}
	steps, err := f.store.ListReviewSteps(ctx, f.tourID)
	if err != nil || len(steps) != 0 {
		t.Fatalf("steps = %+v, err = %v, want replacement pass empty", steps, err)
	}
}

func TestAdvancePassPreservesPriorPassAndStartsNextPass(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	firstSteps, err := f.store.ListReviewSteps(ctx, f.tourID)
	if err != nil || len(firstSteps) != 1 {
		t.Fatalf("first steps = %+v, err = %v", firstSteps, err)
	}

	if err := f.svc.AdvancePass(ctx, f.tourID, "Address reviewer feedback"); err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Passes) != 2 || view.Passes[0].Pass.Status != "superseded" || view.Passes[1].Pass.Number != 2 || view.Passes[1].Pass.Guidance != "Address reviewer feedback" {
		t.Fatalf("passes = %+v", view.Passes)
	}
	if len(view.Passes[0].Steps) != 1 || view.Passes[0].Steps[0].ID != firstSteps[0].ID {
		t.Fatalf("prior pass steps = %+v, want preserved %s", view.Passes[0].Steps, firstSteps[0].ID)
	}
	if len(view.Steps) != 1 || view.Steps[0].PassID != view.Passes[1].Pass.ID {
		t.Fatalf("active steps = %+v, want synced into pass 2", view.Steps)
	}
}

func TestDeleteReviewRemovesPacketAndPublishesChange(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Delete(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetReviewTour(ctx, f.tourID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("tour err = %v", err)
	}
	queue, err := f.svc.Queue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 0 {
		t.Fatalf("queue = %+v", queue)
	}
}

func TestRestartReviewClearsPacketPreservesCaptureAndPublishesChange(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	bus := events.New()
	changes := bus.Subscribe(events.EventReviewChanged)
	f.svc = NewReviewService(f.store, f.tmux, bus)
	if _, err := f.store.UpdateReviewTour(ctx, f.tourID, func(tour *store.ReviewTour) {
		tour.BaseSHA = "original-base"
		tour.RepositoryPath = f.repo
		tour.Status = "ready"
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.InsertReviewStep(ctx, store.ReviewStep{ID: "stale", TaskID: "task-1", TourID: f.tourID, Kind: "file", Files: "[]"}); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.Restart(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	tour, err := f.store.GetReviewTour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if tour.Status != "capturing" || tour.BaseSHA != "original-base" || tour.RepositoryPath != f.repo {
		t.Fatalf("restarted tour = %+v", tour)
	}
	steps, err := f.store.ListReviewSteps(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 0 {
		t.Fatalf("steps = %+v, want none", steps)
	}
	select {
	case event := <-changes:
		payload := event.Payload.(events.ReviewChangedPayload)
		if payload.TourID != f.tourID || payload.Kind != "restarted" {
			t.Fatalf("event payload = %+v", payload)
		}
	default:
		t.Fatal("restart did not publish review change")
	}
}

func TestEditAndRemoveReviewChapterByPrefix(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "chapter.go", "package chapter\n")
	gitCommitAll(t, f.repo, "add chapter")
	chapterID, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "Old title", Narration: "Old narration", Risk: "low", Includes: []ChapterInclude{{FilePath: "chapter.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	order := 7
	if err := f.svc.EditChapter(ctx, f.tourID, chapterID[:8], ChapterEditArgs{
		Title: "New title", Narration: "New narration", Risk: "high", OrderHint: &order,
	}); err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.Chapter(ctx, f.tourID, chapterID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Title != "New title" || view.Narration != "New narration" || view.Risk != "high" || view.OrderHint == nil || *view.OrderHint != 7 || len(view.Hunks) != 1 {
		t.Fatalf("edited chapter = %+v", view)
	}
	if err := f.svc.RemoveChapter(ctx, f.tourID, chapterID[:8]); err != nil {
		t.Fatal(err)
	}
	chapters, err := f.svc.Chapters(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chapters.Chapters) != 0 {
		t.Fatalf("chapters after remove = %+v", chapters.Chapters)
	}
}

func TestReviewChaptersReturnsOrderedMetadataMembershipsAndDiffs(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "chapter.go", "package chapter\n\nfunc Added() {}\n")
	gitCommitAll(t, f.repo, "add chapter")
	stepID, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "Chapter API", Narration: "Expose chapter context to automation.", Risk: "medium",
		Includes: []ChapterInclude{{FilePath: "chapter.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}

	view, err := f.svc.Chapters(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Tour.ID != f.tourID || len(view.Chapters) != 1 {
		t.Fatalf("chapters view = %+v", view)
	}
	chapter := view.Chapters[0]
	if chapter.ID != stepID || chapter.Title != "Chapter API" || chapter.Narration != "Expose chapter context to automation." || chapter.Risk != "medium" || chapter.Generated {
		t.Fatalf("chapter = %+v", chapter)
	}
	if len(chapter.Hunks) != 1 || chapter.Hunks[0].FilePath != "chapter.go" || chapter.Hunks[0].HunkAnchor == "" {
		t.Fatalf("chapter hunks = %+v", chapter.Hunks)
	}

	detail, err := f.svc.Chapter(ctx, f.tourID, stepID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ID != stepID || len(detail.Diff) != 1 || detail.Diff[0].NewPath != "chapter.go" || len(detail.Diff[0].Hunks) != 1 {
		t.Fatalf("chapter detail = %+v", detail)
	}
}

func TestUnreviewedCountsPreferChaptersOverHiddenCommitSteps(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	if _, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{Title: "Package A", Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}}}); err != nil {
		t.Fatal(err)
	}
	counts, err := f.store.UnreviewedReviewCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts["task-1"] != 1 {
		t.Fatalf("count = %d, want chapter only", counts["task-1"])
	}
}

func TestReadyFreezesHeadAndCreatesRemainingChangesChapter(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	writeRepoFile(t, f.repo, "b.go", "package b\n")
	head := gitCommitAll(t, f.repo, "add b")

	_, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{Title: "Package A", Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Ready(ctx, f.tourID, "ready"); err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Tour.HeadSHA != head {
		t.Fatalf("head = %q, want %q", view.Tour.HeadSHA, head)
	}
	if len(view.Steps) != 2 || view.Steps[1].Title != "Remaining changes" {
		t.Fatalf("steps = %+v", view.Steps)
	}
	files, err := f.svc.StepDiff(ctx, f.tourID, view.Steps[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].NewPath != "b.go" {
		t.Fatalf("remaining = %+v", files)
	}

	writeRepoFile(t, f.repo, "c.go", "package c\n")
	gitCommitAll(t, f.repo, "add c after ready")
	files, err = f.svc.StepDiff(ctx, f.tourID, view.Steps[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].NewPath != "a.go" {
		t.Fatalf("frozen chapter changed = %+v", files)
	}
}

func TestCreateChapterGroupsSelectedHunksAcrossCommits(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	writeRepoFile(t, f.repo, "b.go", "package b\n")
	gitCommitAll(t, f.repo, "add b")

	stepID, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "Packages", Narration: "Review these together",
		Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}, {FilePath: "b.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Steps) != 1 || view.Steps[0].ID != stepID || view.Steps[0].Kind != "chapter" {
		t.Fatalf("steps = %+v, want authored chapter only", view.Steps)
	}
	files, err := f.svc.StepDiff(ctx, f.tourID, stepID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].NewPath != "a.go" || files[1].NewPath != "b.go" {
		t.Fatalf("chapter diff = %+v", files)
	}
}

func TestReviewSyncWithoutWorktreeUsesRecordedRepository(t *testing.T) {
	s := newReviewTestStore(t)
	repo := initTestRepo(t)
	createTask(t, s, "task-1")
	base := gitRun(t, repo, "rev-parse", "HEAD")
	tour, err := s.EnsureReviewTour(context.Background(), "task-1", "")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewReviewService(s, nil, nil)
	if err := svc.BeginCapture(context.Background(), "task-1", repo); err != nil {
		t.Fatal(err)
	}
	tour, err = s.GetReviewTour(context.Background(), tour.ID)
	if err != nil || tour.BaseSHA != base || tour.RepositoryPath != repo {
		t.Fatalf("tour = %+v, err = %v", tour, err)
	}
	writeRepoFile(t, repo, "plain.go", "package plain\n")
	gitCommitAll(t, repo, "plain repository commit")

	view, err := svc.Tour(context.Background(), tour.ID)
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
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	steps, _ := f.store.ListReviewSteps(ctx, f.tourID)
	if len(steps) != 1 || steps[0].Kind != "dirty" {
		t.Fatalf("steps = %+v, want single dirty step", steps)
	}
	dirtyID := steps[0].ID
	firstFingerprint := steps[0].DirtyFingerprint

	// Reviewing it, then changing the worktree, clears the reviewed mark.
	if err := f.svc.SetReviewed(ctx, f.tourID, dirtyID, true); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, f.repo, "wip.go", "package wip // changed\n")
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
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
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	steps, _ = f.store.ListReviewSteps(ctx, f.tourID)
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
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	steps, err := f.store.ListReviewSteps(ctx, f.tourID)
	if err != nil || len(steps) != 1 {
		t.Fatalf("steps = %+v, err = %v", steps, err)
	}
	initialFingerprint := steps[0].DirtyFingerprint

	gitRun(t, f.repo, "add", "README.md")
	if err := f.svc.SetReviewed(ctx, f.tourID, steps[0].ID, true); err != nil {
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
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}

	gitRun(t, f.repo, "commit", "--amend", "-m", "add a (amended)")
	newSHA := gitRun(t, f.repo, "rev-parse", "HEAD")
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}

	steps, _ := f.store.ListReviewSteps(ctx, f.tourID)
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

func TestCompletedReviewIsExcludedFromSyncAndQueue(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	if _, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "Package A", Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Ready(ctx, f.tourID, "initial review"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Complete(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}

	writeRepoFile(t, f.repo, "b.go", "package b\n")
	gitCommitAll(t, f.repo, "follow-up fix")

	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	steps, err := f.store.ListReviewSteps(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 2 { // authored chapter plus its hidden original commit
		t.Fatalf("completed review imported follow-up work: %+v", steps)
	}
	items, err := f.svc.Queue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("completed review returned to queue: %+v", items)
	}
}

func TestReadyDoesNotRequeueCompletedTourWithoutNewReviewableWork(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	if _, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "Package A", Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Ready(ctx, f.tourID, "initial review"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Complete(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.Ready(ctx, f.tourID, "unchanged packet"); err != nil {
		t.Fatal(err)
	}

	tour, err := f.store.GetReviewTour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if tour.Status != "reviewed" {
		t.Fatalf("status = %q, want reviewed", tour.Status)
	}
	items, err := f.svc.Queue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("queue = %+v, want completed tour hidden", items)
	}
}

func TestAddingChapterToCompletedTourMakesItVisibleAndReviewable(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	firstID, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "Initial chapter", Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Ready(ctx, f.tourID, "initial review"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Complete(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}

	secondID, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "Additional context", Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}

	view, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Steps) != 2 || view.Steps[0].ID != firstID || view.Steps[1].ID != secondID {
		t.Fatalf("steps = %+v, want old and additional chapters", view.Steps)
	}
	if view.Steps[0].ReviewedAt == nil || view.Steps[1].ReviewedAt != nil {
		t.Fatalf("review state = %+v", view.Steps)
	}
	items, err := f.svc.Queue(ctx)
	if err != nil || len(items) != 1 || items[0].Unreviewed != 1 {
		t.Fatalf("queue = %+v, err = %v", items, err)
	}
}

func TestReviewReadyCompleteAndTerminalWatermark(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	writeRepoFile(t, f.repo, "b.go", "package b\n")
	lastSHA := gitCommitAll(t, f.repo, "add b")

	if err := f.svc.Ready(ctx, f.tourID, "feature done"); err != nil {
		t.Fatal(err)
	}
	tour, _ := f.store.GetReviewTour(ctx, f.tourID)
	if tour.Status != "ready" || tour.Summary != "feature done" || tour.ReadyAt == nil {
		t.Fatalf("tour after Ready = %+v", tour)
	}

	if err := f.svc.Complete(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	tour, _ = f.store.GetReviewTour(ctx, f.tourID)
	if tour.Status != "reviewed" {
		t.Fatalf("Status = %q, want reviewed", tour.Status)
	}
	if tour.LastReviewedSHA != lastSHA {
		t.Fatalf("watermark = %q, want %q", tour.LastReviewedSHA, lastSHA)
	}
	steps, _ := f.store.ListReviewSteps(ctx, f.tourID)
	for _, s := range steps {
		if s.ReviewedAt == nil {
			t.Fatalf("Complete must stamp all steps reviewed: %+v", s)
		}
	}

	// New work does not revive a completed tour.
	writeRepoFile(t, f.repo, "c.go", "package c\n")
	gitCommitAll(t, f.repo, "add c")
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	tour, _ = f.store.GetReviewTour(ctx, f.tourID)
	if tour.Status != "reviewed" {
		t.Fatalf("Status after new commit = %q, want reviewed (terminal)", tour.Status)
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
	stepID, err := f.svc.Annotate(ctx, f.tourID, AnnotateArgs{Text: "tricky bit, review carefully", Risk: "high", OrderHint: &two})
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
	if _, err := f.svc.Annotate(ctx, f.tourID, AnnotateArgs{SHA: sha1, Text: "extra context"}); err != nil {
		t.Fatal(err)
	}
	steps, _ := f.store.ListReviewSteps(ctx, f.tourID)
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
	noteID, err := f.svc.Annotate(ctx, f.tourID, AnnotateArgs{Text: "shared helper touched", Files: []string{"a.go", "b.go"}})
	if err != nil {
		t.Fatal(err)
	}
	note, _ := f.store.GetReviewStep(ctx, noteID)
	if note.Kind != "note" || !strings.Contains(note.Files, "a.go") {
		t.Fatalf("note step = %+v", note)
	}

	// Unknown SHA errors clearly.
	if _, err := f.svc.Annotate(ctx, f.tourID, AnnotateArgs{SHA: "deadbeef", Text: "x"}); err == nil {
		t.Fatal("unknown sha must error")
	}
}

func TestAnnotatingCompletedCommitRequeuesItForReview(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "a.go", "package a\n")
	sha := gitCommitAll(t, f.repo, "add a")
	if err := f.svc.Ready(ctx, f.tourID, "ready"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Complete(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}

	stepID, err := f.svc.Annotate(ctx, f.tourID, AnnotateArgs{SHA: sha, Text: "new review context"})
	if err != nil {
		t.Fatal(err)
	}

	step, err := f.store.GetReviewStep(ctx, stepID)
	if err != nil {
		t.Fatal(err)
	}
	if step.ReviewedAt != nil {
		t.Fatal("annotated step must become unreviewed")
	}
	tour, _ := f.store.GetReviewTour(ctx, f.tourID)
	if tour.Status != "ready" {
		t.Fatalf("status = %q, want ready", tour.Status)
	}
	items, err := f.svc.Queue(ctx)
	if err != nil || len(items) != 1 || items[0].Unreviewed != 1 {
		t.Fatalf("queue = %+v, err = %v", items, err)
	}
}

func TestAnnotatingHunkOnCompletedStepRequeuesItForReview(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "a.go", "package a\n")
	sha := gitCommitAll(t, f.repo, "add a")
	if err := f.svc.Ready(ctx, f.tourID, "ready"); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Complete(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}

	hunk := 1
	if _, err := f.svc.Annotate(ctx, f.tourID, AnnotateArgs{
		SHA: sha, Text: "inspect this line", Files: []string{"a.go"}, Hunk: &hunk,
	}); err != nil {
		t.Fatal(err)
	}

	steps, _ := f.store.ListReviewSteps(ctx, f.tourID)
	if len(steps) != 1 || steps[0].ReviewedAt != nil {
		t.Fatalf("steps = %+v, want annotated step unreviewed", steps)
	}
	tour, _ := f.store.GetReviewTour(ctx, f.tourID)
	if tour.Status != "ready" {
		t.Fatalf("status = %q, want ready", tour.Status)
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
	noteID, err := f.svc.Annotate(ctx, f.tourID, AnnotateArgs{
		SHA: sha, Text: "check this hunk", Files: []string{"old.go"}, Hunk: &hunk,
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.Tour(ctx, f.tourID)
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

func TestReviewAnnotateLineRangePersistsSelectionWithinHunk(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "range.go", "package range_test\n\nfunc First() {}\nfunc Second() {}\n")
	sha := gitCommitAll(t, f.repo, "add range")
	hunk, start, end := 1, 2, 3
	noteID, err := f.svc.Annotate(ctx, f.tourID, AnnotateArgs{
		SHA: sha, Text: "these lines form one invariant", Files: []string{"range.go"}, Hunk: &hunk,
		LineStart: &start, LineEnd: &end,
	})
	if err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.HunkNotes) != 1 {
		t.Fatalf("HunkNotes = %+v", view.HunkNotes)
	}
	note := view.HunkNotes[0]
	if note.ID != noteID || note.LineStart == nil || note.LineEnd == nil || *note.LineStart != 2 || *note.LineEnd != 3 || note.LineAnchor == "" {
		t.Fatalf("line note = %+v", note)
	}
}

func TestEditAndRemoveReviewAnnotationByPrefix(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "note.go", "package note\n")
	sha := gitCommitAll(t, f.repo, "add note")
	hunk := 1
	noteID, err := f.svc.Annotate(ctx, f.tourID, AnnotateArgs{SHA: sha, Text: "old note", Files: []string{"note.go"}, Hunk: &hunk})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.EditAnnotation(ctx, f.tourID, noteID[:8], "new note"); err != nil {
		t.Fatal(err)
	}
	notes, _ := f.store.ListReviewHunkNotes(ctx, f.tourID)
	if len(notes) != 1 || notes[0].Body != "new note" || notes[0].UpdatedAt == "" {
		t.Fatalf("edited notes = %+v", notes)
	}
	if err := f.svc.RemoveAnnotation(ctx, f.tourID, noteID[:8]); err != nil {
		t.Fatal(err)
	}
	notes, _ = f.store.ListReviewHunkNotes(ctx, f.tourID)
	if len(notes) != 0 {
		t.Fatalf("notes after remove = %+v", notes)
	}
}

func TestReviewAnnotateLineRangeRejectsLinesOutsideHunk(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "range.go", "package range_test\n")
	sha := gitCommitAll(t, f.repo, "add range")
	hunk, start, end := 1, 1, 99
	_, err := f.svc.Annotate(ctx, f.tourID, AnnotateArgs{
		SHA: sha, Text: "invalid", Files: []string{"range.go"}, Hunk: &hunk,
		LineStart: &start, LineEnd: &end,
	})
	if err == nil || !strings.Contains(err.Error(), "has") {
		t.Fatalf("error = %v, want line range bounds error", err)
	}
}

func TestReviewChapterReanchorsCommitLineNoteByContent(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "lines.go", "package lines\n\nfunc First() {}\nfunc Second() {}\n")
	sha := gitCommitAll(t, f.repo, "add lines")
	hunk, start, end := 1, 2, 3
	noteID, err := f.svc.Annotate(ctx, f.tourID, AnnotateArgs{
		SHA: sha, Text: "line intent", Files: []string{"lines.go"}, Hunk: &hunk,
		LineStart: &start, LineEnd: &end,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, f.repo, "lines.go", "package lines\n\nfunc First() {}\nfunc Second() {}\nfunc Third() {}\n")
	gitCommitAll(t, f.repo, "extend same hunk")
	chapterID, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "Line chapter", Includes: []ChapterInclude{{FilePath: "lines.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}

	detail, err := f.svc.Chapter(ctx, f.tourID, chapterID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.HunkNotes) != 1 {
		t.Fatalf("HunkNotes = %+v", detail.HunkNotes)
	}
	note := detail.HunkNotes[0]
	if note.ID != noteID || note.HunkAnchor != detail.Diff[0].Hunks[0].Anchor || note.LineStart == nil || note.LineEnd == nil {
		t.Fatalf("reanchored note = %+v, diff = %+v", note, detail.Diff)
	}
	if reviewLineAnchor(detail.Diff[0].Hunks[0].Lines[*note.LineStart-1:*note.LineEnd]) != note.LineAnchor {
		t.Fatalf("line range %d-%d does not match anchor", *note.LineStart, *note.LineEnd)
	}
}

func TestReviewTourProjectsCommitHunkNotesOntoVisibleChapters(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "noted.go", "package noted\n\nfunc Value() int { return 1 }\n")
	sha := gitCommitAll(t, f.repo, "add noted file")

	hunk := 1
	noteID, err := f.svc.Annotate(ctx, f.tourID, AnnotateArgs{
		SHA: sha, Text: "why this hunk matters", Files: []string{"noted.go"}, Hunk: &hunk,
	})
	if err != nil {
		t.Fatal(err)
	}
	chapterID, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "Noted behavior", Includes: []ChapterInclude{{FilePath: "noted.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}

	view, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Steps) != 1 || view.Steps[0].ID != chapterID {
		t.Fatalf("visible steps = %+v, want only chapter %s", view.Steps, chapterID)
	}
	if len(view.HunkNotes) != 1 {
		t.Fatalf("HunkNotes = %+v", view.HunkNotes)
	}
	projected := view.HunkNotes[0]
	if projected.ID != noteID || projected.StepID != chapterID || projected.FilePath != "noted.go" || projected.Body != "why this hunk matters" {
		t.Fatalf("projected note = %+v", projected)
	}
}

func TestReviewTourKeepsChangedCommitHunkNoteVisibleAsUnmatchedChapterNote(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "changed.go", "package changed\n\nfunc Value() int { return 1 }\n")
	sha := gitCommitAll(t, f.repo, "add changed file")
	hunk := 1
	if _, err := f.svc.Annotate(ctx, f.tourID, AnnotateArgs{
		SHA: sha, Text: "original intent", Files: []string{"changed.go"}, Hunk: &hunk,
	}); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, f.repo, "changed.go", "package changed\n\nfunc Value() int { return 2 }\n")
	gitCommitAll(t, f.repo, "revise changed file")
	chapterID, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "Changed behavior", Includes: []ChapterInclude{{FilePath: "changed.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}

	view, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.HunkNotes) != 1 || view.HunkNotes[0].StepID != chapterID || view.HunkNotes[0].Body != "original intent" {
		t.Fatalf("changed hunk note not projected as unmatched chapter note: %+v", view.HunkNotes)
	}
	chapterDiff, err := f.svc.StepDiff(ctx, f.tourID, chapterID)
	if err != nil {
		t.Fatal(err)
	}
	if view.HunkNotes[0].HunkAnchor == chapterDiff[0].Hunks[0].Anchor {
		t.Fatal("test requires a stale anchor so the UI renders it as unmatched")
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
		if _, err := f.svc.Annotate(ctx, f.tourID, tc); err == nil {
			t.Fatalf("Annotate(%+v) should fail", tc)
		}
	}
}

func TestReviewTourHunkNotesIsNonNil(t *testing.T) {
	f := newReviewFixture(t)
	view, err := f.svc.Tour(context.Background(), f.tourID)
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
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	steps, _ := f.store.ListReviewSteps(ctx, f.tourID)
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

	if err := f.svc.AskQuestion(ctx, f.tourID, stepID, ReviewQuestion{Text: "why not reuse the helper?"}); err != nil {
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
	msgs, _ := f.store.ListReviewMessages(ctx, f.tourID)
	if len(msgs) != 1 || msgs[0].Kind != "question" || msgs[0].DeliveredAt == nil {
		t.Fatalf("transcript = %+v", msgs)
	}

	// Agent answers via the CLI verb path (prefix resolution included).
	if err := f.svc.Answer(ctx, f.tourID, stepID[:6], "the helper assumes X"); err != nil {
		t.Fatal(err)
	}
	msgs, _ = f.store.ListReviewMessages(ctx, f.tourID)
	if len(msgs) != 2 || msgs[1].Kind != "answer" || msgs[1].Author != "agent" {
		t.Fatalf("transcript after answer = %+v", msgs)
	}

	// Dead session: question stored undelivered, typed error returned.
	if err := f.tmux.Kill("legato-task-1"); err != nil {
		t.Fatal(err)
	}
	err := f.svc.AskQuestion(ctx, f.tourID, stepID, ReviewQuestion{Text: "still there?"})
	if !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("err = %v, want ErrAgentOffline", err)
	}
	msgs, _ = f.store.ListReviewMessages(ctx, f.tourID)
	if len(msgs) != 3 || msgs[2].DeliveredAt != nil {
		t.Fatalf("offline question should be stored undelivered: %+v", msgs)
	}
}

func TestUniqueReviewChapterHunksKeepsFirstMembershipOrder(t *testing.T) {
	hunks := []store.ReviewChapterHunk{
		{ID: "first", FilePath: "pnpm-lock.yaml", HunkAnchor: "same", Seq: 0},
		{ID: "other", FilePath: "pnpm-lock.yaml", HunkAnchor: "other", Seq: 1},
		{ID: "duplicate", FilePath: "pnpm-lock.yaml", HunkAnchor: "same", Seq: 2},
	}

	got := uniqueReviewChapterHunks(hunks)

	if len(got) != 2 || got[0].ID != "first" || got[1].ID != "other" {
		t.Fatalf("unique hunks = %+v", got)
	}
}

func TestReviewQuestionIncludesValidatedLineSelection(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n\nfunc answer() int {\n\treturn 42\n}\n")
	gitCommitAll(t, f.repo, "add answer")
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	steps, _ := f.store.ListReviewSteps(ctx, f.tourID)
	stepID := steps[0].ID
	files, err := f.svc.StepDiff(ctx, f.tourID, stepID)
	if err != nil {
		t.Fatal(err)
	}
	hunk := files[0].Hunks[0]

	if err := f.tmux.Spawn("legato-task-1", f.repo, 80, 24); err != nil {
		t.Fatal(err)
	}
	if err := f.store.InsertAgentSession(ctx, store.AgentSession{
		TaskID: "task-1", TmuxSession: "legato-task-1", Command: "chimera",
		Status: "running", StartedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	req := ReviewQuestion{
		Text: "Why return this value?",
		Selection: &ReviewLineSelection{
			FilePath: "a.go", HunkAnchor: hunk.Anchor, Start: 2, End: 3,
		},
	}
	if err := f.svc.AskQuestion(ctx, f.tourID, stepID, req); err != nil {
		t.Fatal(err)
	}

	wantParts := []string{"Why return this value?", "**Selected lines from `a.go`", hunk.Header, "```diff", "+3 func answer() int {", "+4 \treturn 42", "```"}
	lines := f.tmux.sentLines["legato-task-1"]
	if len(lines) != 1 {
		t.Fatalf("sent lines = %v", lines)
	}
	msgs, err := f.store.ListReviewMessages(ctx, f.tourID)
	if err != nil || len(msgs) != 1 {
		t.Fatalf("messages = %+v, err = %v", msgs, err)
	}
	if !strings.Contains(lines[0], "Reply in Markdown") {
		t.Errorf("delivered question missing Markdown guidance:\n%s", lines[0])
	}
	for _, want := range wantParts {
		if !strings.Contains(lines[0], want) {
			t.Errorf("delivered question missing %q:\n%s", want, lines[0])
		}
		if !strings.Contains(msgs[0].Body, want) {
			t.Errorf("stored question missing %q:\n%s", want, msgs[0].Body)
		}
	}
}

func TestReviewMessagesSyncBeforeMutatingTranscript(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	steps, _ := f.store.ListReviewSteps(ctx, f.tourID)
	anchorID := steps[0].ID

	writeRepoFile(t, f.repo, "b.go", "package b\n")
	gitCommitAll(t, f.repo, "add b")
	if err := f.svc.AskQuestion(ctx, f.tourID, anchorID, ReviewQuestion{Text: "question"}); !errors.Is(err, ErrAgentOffline) {
		t.Fatalf("AskQuestion err = %v, want ErrAgentOffline", err)
	}
	steps, _ = f.store.ListReviewSteps(ctx, f.tourID)
	if len(steps) != 2 {
		t.Fatalf("AskQuestion must sync first; steps = %+v", steps)
	}

	writeRepoFile(t, f.repo, "c.go", "package c\n")
	gitCommitAll(t, f.repo, "add c")
	if err := f.svc.Answer(ctx, f.tourID, anchorID, "answer"); err != nil {
		t.Fatal(err)
	}
	steps, _ = f.store.ListReviewSteps(ctx, f.tourID)
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

	view, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Tour.TaskID != "task-1" || len(view.Steps) != 2 {
		t.Fatalf("view = %+v", view)
	}

	// Commit step diff parses to the committed file.
	commitDiff, err := f.svc.StepDiff(ctx, f.tourID, view.Steps[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(commitDiff) != 1 || commitDiff[0].NewPath != "a.go" {
		t.Fatalf("commit diff = %+v", commitDiff)
	}

	// Dirty step diff shows the uncommitted file.
	dirtyDiff, err := f.svc.StepDiff(ctx, f.tourID, view.Steps[1].ID)
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
	if err := f.svc.Ready(ctx, f.tourID, "done"); err != nil {
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
	if err := f.svc.Complete(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	items, _ = f.svc.Queue(ctx)
	if len(items) != 0 {
		t.Fatalf("queue after complete = %+v", items)
	}
}

func TestReviewQueueDoesNotRefreshUnchangedTourTimestamp(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	if err := f.svc.Ready(ctx, f.tourID, "done"); err != nil {
		t.Fatal(err)
	}
	before, err := f.store.GetReviewTour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Queue(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := f.store.GetReviewTour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if after.UpdatedAt != before.UpdatedAt {
		t.Fatalf("queue refresh changed updated_at: before=%s after=%s", before.UpdatedAt, after.UpdatedAt)
	}
}

func TestReviewQueueSyncsToursAndSkipsBrokenWorktrees(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	if err := f.svc.Ready(ctx, f.tourID, "ready"); err != nil {
		t.Fatal(err)
	}

	createTask(t, f.store, "task-broken")
	missing := filepath.Join(t.TempDir(), "missing")
	if err := f.store.SetTaskWorktree(ctx, "task-broken", &store.TaskWorktree{
		PrimaryDir: missing, Path: missing, Branch: "feature", BaseBranch: "main",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.EnsureReviewTour(ctx, "task-broken", ""); err != nil {
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
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
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

func TestReviewBadgeStatesExcludesReadyTourWithZeroUnreviewed(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	if _, err := f.store.EnsureReviewTour(ctx, "task-1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.UpdateReviewTour(ctx, f.tourID, func(rt *store.ReviewTour) {
		rt.Status = "ready"
	}); err != nil {
		t.Fatal(err)
	}

	states, err := f.svc.ReviewBadgeStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	state, ok := states["task-1"]
	if ok && state.Ready {
		t.Fatalf("state = %+v; empty ready tour must not show a ready badge", state)
	}
}

func TestReviewSyncSeedsStepsFromCommits(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()

	writeRepoFile(t, f.repo, "a.go", "package a\n")
	sha1 := gitCommitAll(t, f.repo, "add feature a\n\nDecided on approach X because Y.")
	writeRepoFile(t, f.repo, "b.go", "package b\n")
	sha2 := gitCommitAll(t, f.repo, "add feature b")

	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}

	steps, err := f.store.ListReviewSteps(ctx, f.tourID)
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
	if err := f.svc.Sync(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	steps, _ = f.store.ListReviewSteps(ctx, f.tourID)
	if len(steps) != 2 {
		t.Fatalf("re-sync duplicated steps: %d", len(steps))
	}
}
