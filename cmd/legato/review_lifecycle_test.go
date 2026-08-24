package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func TestReviewDiscardRemovesSelectedTour(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	tour, err := s.EnsureReviewTour(ctx, "task-1", "rewrite")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	configureReviewCLITest(t, dbPath)
	if code := runReviewCmd([]string{"discard", "--task", "task-1", "--name", "rewrite"}); code != 0 {
		t.Fatalf("runReviewCmd() = %d, want 0", code)
	}

	s, err = store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.GetReviewTour(ctx, tour.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetReviewTour() error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetTask(ctx, "task-1"); err != nil {
		t.Fatalf("discard removed task: %v", err)
	}
}

func TestReviewRestartReplacesPacketAndPreservesCaptureBoundary(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	tour, err := s.EnsureReviewTour(ctx, "task-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateReviewTour(ctx, tour.ID, func(rt *store.ReviewTour) {
		rt.BaseSHA = "base-before-rewrite"
		rt.RepositoryPath = "/repo"
		rt.Status = "ready"
		rt.Summary = "stale review"
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertReviewStep(ctx, store.ReviewStep{
		ID: "stale-step", TaskID: "task-1", TourID: tour.ID, Kind: "file", Title: "Stale", Files: "[]",
	}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	configureReviewCLITest(t, dbPath)
	if code := runReviewCmd([]string{"restart", "--feedback", "Focus on error handling", "--task", "task-1"}); code != 0 {
		t.Fatalf("runReviewCmd() = %d, want 0", code)
	}

	s, err = store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	restarted, err := s.GetReviewTour(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Status != "capturing" || restarted.Summary != "" {
		t.Fatalf("restarted tour = %+v, want clean capturing packet", restarted)
	}
	if restarted.BaseSHA != "base-before-rewrite" || restarted.RepositoryPath != "/repo" {
		t.Fatalf("capture boundary lost: %+v", restarted)
	}
	pass, err := s.GetActiveReviewPass(ctx, tour.ID)
	if err != nil || pass.Guidance != "Focus on error handling" {
		t.Fatalf("replacement pass = %+v, err = %v", pass, err)
	}
	steps, err := s.ListReviewSteps(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 0 {
		t.Fatalf("steps = %+v, want stale artifacts removed", steps)
	}
}

func TestReviewChapterEditAndRemoveCLI(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	repo := initReviewCLIRepo(t)
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskWorktree(ctx, "task-1", &store.TaskWorktree{PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	tour, _ := s.EnsureReviewTour(ctx, "task-1", "")
	svc := service.NewReviewService(s, nil, nil)
	chapterID, err := svc.CreateChapter(ctx, tour.ID, service.ChapterArgs{Title: "Old", Includes: []service.ChapterInclude{{FilePath: "chapter.go", Hunk: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	configureReviewCLITest(t, dbPath)

	if code := runReviewCmd([]string{"chapter", "edit", chapterID[:8], "New", "New narration", "--risk", "high", "--order", "4", "--task", "task-1"}); code != 0 {
		t.Fatalf("edit code = %d", code)
	}
	s, _ = store.New(dbPath)
	edited, _ := s.GetReviewStep(ctx, chapterID)
	if edited.Title != "New" || edited.Narration != "New narration" || edited.Risk != "high" || edited.OrderHint == nil || *edited.OrderHint != 4 {
		t.Fatalf("edited = %+v", edited)
	}
	s.Close()

	if code := runReviewCmd([]string{"chapter", "remove", chapterID[:8], "--task", "task-1"}); code != 0 {
		t.Fatalf("remove code = %d", code)
	}
	s, _ = store.New(dbPath)
	defer s.Close()
	if _, err := s.GetReviewStep(ctx, chapterID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("chapter still exists: %v", err)
	}
}

func TestReviewAnnotationEditAndRemoveCLI(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	repo := initReviewCLIRepo(t)
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskWorktree(ctx, "task-1", &store.TaskWorktree{PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	tour, _ := s.EnsureReviewTour(ctx, "task-1", "")
	s.Close()
	configureReviewCLITest(t, dbPath)

	noteID := strings.TrimSpace(captureStdout(t, func() int {
		return runReviewCmd([]string{"annotate", "old", "--file", "chapter.go", "--hunk", "1", "--lines", "1-1", "--task", "task-1"})
	}))
	s, _ = store.New(dbPath)
	notes, _ := s.ListReviewHunkNotes(ctx, tour.ID)
	if len(notes) != 1 || notes[0].LineStart == nil || notes[0].LineEnd == nil || *notes[0].LineStart != 1 || *notes[0].LineEnd != 1 {
		t.Fatalf("created line notes = %+v", notes)
	}
	s.Close()

	if code := runReviewCmd([]string{"annotation", "edit", noteID[:8], "new", "--task", "task-1"}); code != 0 {
		t.Fatalf("edit code = %d", code)
	}
	s, _ = store.New(dbPath)
	notes, _ = s.ListReviewHunkNotes(ctx, tour.ID)
	if len(notes) != 1 || notes[0].Body != "new" {
		t.Fatalf("edited notes = %+v", notes)
	}
	s.Close()

	if code := runReviewCmd([]string{"annotation", "remove", noteID[:8], "--task", "task-1"}); code != 0 {
		t.Fatalf("remove code = %d", code)
	}
	s, _ = store.New(dbPath)
	defer s.Close()
	notes, _ = s.ListReviewHunkNotes(ctx, tour.ID)
	if len(notes) != 0 {
		t.Fatalf("notes after remove = %+v", notes)
	}
}

func TestReviewChapterJSONVerbsExposeMetadataAndSelectedDiff(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	repo := initReviewCLIRepo(t)
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskWorktree(ctx, "task-1", &store.TaskWorktree{PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	tour, err := s.EnsureReviewTour(ctx, "task-1", "")
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewReviewService(s, nil, nil)
	stepID, err := svc.CreateChapter(ctx, tour.ID, service.ChapterArgs{
		Title: "JSON chapter", Narration: "Machine-readable review context.",
		Includes: []service.ChapterInclude{{FilePath: "chapter.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	configureReviewCLITest(t, dbPath)

	listOutput := captureStdout(t, func() int {
		return runReviewCmd([]string{"chapters", "--json", "--task", "task-1"})
	})
	var list service.ReviewChaptersView
	if err := json.Unmarshal([]byte(listOutput), &list); err != nil {
		t.Fatalf("chapters JSON: %v\n%s", err, listOutput)
	}
	if len(list.Chapters) != 1 || list.Chapters[0].ID != stepID || len(list.Chapters[0].Hunks) != 1 {
		t.Fatalf("chapters = %+v", list.Chapters)
	}

	detailOutput := captureStdout(t, func() int {
		return runReviewCmd([]string{"chapter", "show", stepID[:8], "--json", "--task", "task-1"})
	})
	var detail service.ReviewChapterDetail
	if err := json.Unmarshal([]byte(detailOutput), &detail); err != nil {
		t.Fatalf("chapter detail JSON: %v\n%s", err, detailOutput)
	}
	if detail.ID != stepID || len(detail.Diff) != 1 || detail.Diff[0].NewPath != "chapter.go" {
		t.Fatalf("detail = %+v", detail)
	}
}

func TestPublicReviewMutationCommandsUpdatePacket(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	repo := initReviewCLIRepo(t)
	if err := s.CreateTask(ctx, store.Task{ID: "task-public", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskWorktree(ctx, "task-public", &store.TaskWorktree{PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	tour, err := s.EnsureReviewTour(ctx, "task-public", "named")
	if err != nil {
		t.Fatal(err)
	}
	svc := service.NewReviewService(s, nil, nil)
	chapterID, err := svc.CreateChapter(ctx, tour.ID, service.ChapterArgs{Title: "Old", Narration: "keep", Includes: []service.ChapterInclude{{FilePath: "chapter.go", Hunk: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	hunk := 1
	noteID, err := svc.Annotate(ctx, tour.ID, service.AnnotateArgs{Text: "old", Files: []string{"chapter.go"}, Hunk: &hunk})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	configureReviewCLITest(t, dbPath)

	chapterOut := captureStdout(t, func() int {
		return runCLI([]string{"review", "chapter", "edit", chapterID[:8], "--title", "New", "--risk", "high", "--order", "2", "--include", "chapter.go:1", "--task", "task-public", "--name", "named", "--json"})
	})
	var chapterEnvelope struct {
		Data struct {
			ChapterID string `json:"chapter_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(chapterOut), &chapterEnvelope); err != nil || chapterEnvelope.Data.ChapterID != chapterID {
		t.Fatalf("chapter envelope = %s, err=%v", chapterOut, err)
	}
	annotationOut := captureStdout(t, func() int {
		return runCLI([]string{"review", "annotation", "edit", noteID[:8], "--text", "new", "--risk", "medium", "--order", "3", "--file", "chapter.go", "--hunk", "1", "--lines", "1-1", "--task", "task-public", "--name", "named", "--json"})
	})
	var annotationEnvelope struct {
		Data struct {
			AnnotationID string `json:"annotation_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(annotationOut), &annotationEnvelope); err != nil || annotationEnvelope.Data.AnnotationID != noteID {
		t.Fatalf("annotation envelope = %s, err=%v", annotationOut, err)
	}

	s, err = store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	step, _ := s.GetReviewStep(ctx, chapterID)
	if step.Title != "New" || step.Narration != "keep" || step.Risk != "high" || step.OrderHint == nil || *step.OrderHint != 2 {
		t.Fatalf("chapter = %+v", step)
	}
	notes, _ := s.ListReviewHunkNotes(ctx, tour.ID)
	if len(notes) != 1 || notes[0].Body != "new" || notes[0].Risk != "medium" || notes[0].OrderHint == nil || *notes[0].OrderHint != 3 || notes[0].LineStart == nil {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestPublicReviewAnswerPreservesMarkdownAndReplacesRetry(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	repo := initReviewCLIRepo(t)
	if err := s.CreateTask(ctx, store.Task{ID: "task-answer", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskWorktree(ctx, "task-answer", &store.TaskWorktree{PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	tour, _ := s.EnsureReviewTour(ctx, "task-answer", "")
	svc := service.NewReviewService(s, nil, nil)
	stepID, err := svc.CreateChapter(ctx, tour.ID, service.ChapterArgs{Title: "Answer", Includes: []service.ChapterInclude{{FilePath: "chapter.go", Hunk: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertReviewMessage(ctx, store.ReviewMessage{TaskID: "task-answer", TourID: tour.ID, StepID: stepID, Kind: "question", Author: "user", Body: "Explain"}, true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	configureReviewCLITest(t, dbPath)

	stdinBody := "Uses `ProposalDraft`.\n\n```sh\nprintf '%s' \"$VARIABLE $(command) isn't expanded\"\n```\n"
	withReviewStdin(t, stdinBody, func() {
		if code := runCLI([]string{"review", "answer", stepID[:8], "--file", "-", "--task", "task-answer"}); code != 0 {
			t.Fatalf("stdin answer code = %d", code)
		}
	})
	s, err = store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := s.ListReviewMessages(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[1].Body != stdinBody {
		t.Fatalf("stdin transcript = %+v", msgs)
	}
	s.Close()

	fileBody := "Corrected `ProposalDraft` with $VARIABLE, $(command), \"quotes\", and apostrophes.\n"
	answerPath := filepath.Join(t.TempDir(), "answer.md")
	if err := os.WriteFile(answerPath, []byte(fileBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runCLI([]string{"review", "answer", stepID[:8], "--file", answerPath, "--task", "task-answer"}); code != 0 {
		t.Fatalf("file answer code = %d", code)
	}

	s, err = store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	msgs, err = s.ListReviewMessages(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[1].Kind != "answer" || msgs[1].Body != fileBody {
		t.Fatalf("transcript = %+v", msgs)
	}
}

func TestPublicReviewAnswerRejectsUnsafeOrInvalidInputWithoutMutation(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	repo := initReviewCLIRepo(t)
	if err := s.CreateTask(ctx, store.Task{ID: "task-answer", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskWorktree(ctx, "task-answer", &store.TaskWorktree{PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	tour, _ := s.EnsureReviewTour(ctx, "task-answer", "")
	svc := service.NewReviewService(s, nil, nil)
	stepID, err := svc.CreateChapter(ctx, tour.ID, service.ChapterArgs{Title: "Answer", Includes: []service.ChapterInclude{{FilePath: "chapter.go", Hunk: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertReviewMessage(ctx, store.ReviewMessage{TaskID: "task-answer", TourID: tour.ID, StepID: stepID, Kind: "question", Author: "user", Body: "Explain"}, true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	configureReviewCLITest(t, dbPath)

	invalidUTF8 := filepath.Join(t.TempDir(), "invalid.md")
	if err := os.WriteFile(invalidUTF8, []byte{0xff}, 0o600); err != nil {
		t.Fatal(err)
	}
	blank := filepath.Join(t.TempDir(), "blank.md")
	if err := os.WriteFile(blank, []byte(" \n\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := [][]string{
		{"review", "answer", stepID[:8], "damaged text", "--task", "task-answer"},
		{"review", "answer", stepID[:8], "--task", "task-answer"},
		{"review", "answer", stepID[:8], "--file", blank, "--file", blank, "--task", "task-answer"},
		{"review", "answer", stepID[:8], "--file", filepath.Join(t.TempDir(), "missing.md"), "--task", "task-answer"},
		{"review", "answer", stepID[:8], "--file", invalidUTF8, "--task", "task-answer"},
		{"review", "answer", stepID[:8], "--file", blank, "--task", "task-answer"},
	}
	for _, args := range cases {
		stderr, code := captureStderrWithCode(t, func() int { return runCLI(args) })
		if code == 0 || !strings.Contains(stderr, "--file - <<'LEGATO_ANSWER'") {
			t.Fatalf("runCLI(%v) code=%d stderr=%q", args, code, stderr)
		}
	}

	s, err = store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	msgs, err := s.ListReviewMessages(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Kind != "question" {
		t.Fatalf("rejected answers mutated transcript: %+v", msgs)
	}
}

func TestPublicReviewMutationHelpResolves(t *testing.T) {
	for _, path := range [][]string{{"review", "chapter", "edit"}, {"review", "chapter", "remove"}, {"review", "annotation", "edit"}, {"review", "annotation", "remove"}} {
		out := helpText(path)
		if strings.Contains(out, "Unknown command") || !strings.Contains(out, "Usage:") {
			t.Fatalf("help %v:\n%s", path, out)
		}
	}
}

func initReviewCLIRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-b", "main")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "initial")
	git("checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "chapter.go"), []byte("package chapter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-m", "add chapter")
	return dir
}

func withReviewStdin(t *testing.T, body string, run func()) {
	t.Helper()
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = old
		r.Close()
	}()
	go func() {
		_, _ = io.WriteString(w, body)
		_ = w.Close()
	}()
	run()
}

func captureStdout(t *testing.T, run func() int) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := run()
	w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	if code != 0 {
		t.Fatalf("runReviewCmd() = %d", code)
	}
	return string(out)
}

func reviewCLITestStore(t *testing.T) (string, *store.Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "legato.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	return dbPath, s
}

func configureReviewCLITest(t *testing.T, dbPath string) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf("db:\n  path: %q\n", dbPath)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)
	t.Setenv("LEGATO_TASK_ID", "")
	t.Setenv("LEGATO_REVIEW_NAME", "")
}
