package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

var cliTestPNG = []byte("\x89PNG\r\n\x1a\nthe board")

// documentImageServices opens the plan and review services over a test
// database the way the commands do: with the image folders beside it.
func documentImageServices(t *testing.T, dbPath string) (*store.Store, *service.PlanService, *service.ReviewService) {
	t.Helper()
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	dataDir := filepath.Dir(dbPath)
	plans := service.NewPlanService(s, nil, nil)
	plans.SetImageDir(filepath.Join(dataDir, "plan-images"))
	reviews := service.NewReviewService(s, nil, nil)
	reviews.SetImageDirs(filepath.Join(dataDir, "review-images"), filepath.Join(dataDir, "plan-images"))
	return s, plans, reviews
}

func writePlanBundle(t *testing.T, markdown string, files map[string][]byte) string {
	t.Helper()
	bundle := filepath.Join(t.TempDir(), "bundle")
	if err := os.MkdirAll(filepath.Join(bundle, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	all := map[string][]byte{"plan.md": []byte(markdown), "plan.json": []byte(`{"schema_version":1,"title":"Plan"}`)}
	for name, content := range files {
		all[name] = content
	}
	for name, content := range all {
		if err := os.WriteFile(filepath.Join(bundle, filepath.FromSlash(name)), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return bundle
}

func TestPlanValidateReportsImagesItCannotShowWithSourceLines(t *testing.T) {
	bundle := writePlanBundle(t, "# Plan\n\n![fine](assets/board.png)\n\n![gone](assets/missing.png)\n\n![text](assets/notes.png)\n",
		map[string][]byte{"assets/board.png": cliTestPNG, "assets/notes.png": []byte("plain text")})
	path := filepath.Join(bundle, "plan.md")

	stderr, code := captureStderrWithCode(t, func() int {
		return runCLI([]string{"plan", "validate", path})
	})
	if code != exitUsage {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{path + ":5:", "assets/missing.png", "there is no file", path + ":7:", "assets/notes.png", "not an image"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr %q does not contain %q", stderr, want)
		}
	}
	if strings.Contains(stderr, "assets/board.png") {
		t.Fatalf("stderr names an image that is fine: %q", stderr)
	}
}

func TestPlanValidateAcceptsAPlanWhoseImagesAreInItsBundle(t *testing.T) {
	bundle := writePlanBundle(t, "# Plan\n\n![fine](assets/board.png)\n", map[string][]byte{"assets/board.png": cliTestPNG})

	output := captureStdout(t, func() int {
		return runCLI([]string{"plan", "validate", filepath.Join(bundle, "plan.md")})
	})
	if output != "Plan is valid.\n" {
		t.Fatalf("output = %q", output)
	}
}

func TestPlanSubmitCommandKeepsTheBundlesImages(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	if err := s.CreateTask(context.Background(), store.Task{ID: "task-1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	configureReviewCLITest(t, dbPath)
	bundle := writePlanBundle(t, "# Plan\n\n![The board](assets/board.png)\n", map[string][]byte{"assets/board.png": cliTestPNG})

	captureStdout(t, func() int {
		return runCLI([]string{"plan", "submit", bundle, "--task", "task-1", "--name", "shots"})
	})

	_, plans, _ := documentImageServices(t, dbPath)
	view, err := plans.Plan(context.Background(), "pl-task-1-shots")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Images) != 1 || view.Images[0].Name != "assets/board.png" {
		t.Fatalf("the submitted plan's images = %+v", view.Images)
	}
}

// reviewCLIChapterFixture is a task with a repository under review, and a
// folder with one image file in it.
func reviewCLIChapterFixture(t *testing.T) (dbPath, tourID, shots string) {
	t.Helper()
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	repo := initReviewCLIRepo(t)
	if err := s.CreateTask(ctx, store.Task{ID: "task-shots", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskWorktree(ctx, "task-shots", &store.TaskWorktree{PrimaryDir: repo, Path: repo, Branch: "feature", BaseBranch: "main"}); err != nil {
		t.Fatal(err)
	}
	tour, err := s.EnsureReviewTour(ctx, "task-shots", "")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	configureReviewCLITest(t, dbPath)
	shots = t.TempDir()
	if err := os.WriteFile(filepath.Join(shots, "after.png"), cliTestPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	return dbPath, tour.ID, shots
}

func TestReviewChapterTakesItsNarrationFromAFileWithImagesBesideIt(t *testing.T) {
	dbPath, tourID, shots := reviewCLIChapterFixture(t)
	// Markdown a shell would have changed, and an image named relative to
	// the file.
	narration := "The header uses `$HOME` now.\n\n![After](after.png)\n"
	narrationPath := filepath.Join(shots, "narration.md")
	if err := os.WriteFile(narrationPath, []byte(narration), 0o644); err != nil {
		t.Fatal(err)
	}

	captureStdout(t, func() int {
		return runCLI([]string{"review", "chapter", "Header", "--narration-file", narrationPath, "--include", "chapter.go:1", "--task", "task-shots"})
	})

	_, _, reviews := documentImageServices(t, dbPath)
	view, err := reviews.Tour(context.Background(), tourID)
	if err != nil {
		t.Fatal(err)
	}
	var chapter *store.ReviewStep
	for i := range view.Steps {
		if view.Steps[i].Title == "Header" {
			chapter = &view.Steps[i]
		}
	}
	if chapter == nil || chapter.Narration != narration {
		t.Fatalf("chapter = %+v", chapter)
	}
	if images := view.StepImages[chapter.ID]; len(images) != 1 || images[0].Name != "after.png" {
		t.Fatalf("chapter images = %+v", images)
	}

	// The same option on an edit, read from standard input.
	edited := "Now with ![After](" + filepath.Join(shots, "after.png") + ") inline.\n"
	withReviewStdin(t, edited, func() {
		captureStdout(t, func() int {
			return runCLI([]string{"review", "chapter", "edit", chapter.ID, "--narration-file", "-", "--task", "task-shots"})
		})
	})
	view, err = reviews.Tour(context.Background(), tourID)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range view.Steps {
		if step.ID == chapter.ID && step.Narration != edited {
			t.Fatalf("edited narration = %q", step.Narration)
		}
	}
	if images := view.StepImages[chapter.ID]; len(images) != 1 || images[0].Name != filepath.Join(shots, "after.png") {
		t.Fatalf("edited chapter images = %+v", images)
	}
}

func TestReviewChapterTakesNarrationOnlyOneWay(t *testing.T) {
	_, _, shots := reviewCLIChapterFixture(t)
	narrationPath := filepath.Join(shots, "narration.md")
	if err := os.WriteFile(narrationPath, []byte("From a file."), 0o644); err != nil {
		t.Fatal(err)
	}

	stderr, code := captureStderrWithCode(t, func() int {
		return runCLI([]string{"review", "chapter", "Header", "From the command line.", "--narration-file", narrationPath, "--include", "chapter.go:1", "--task", "task-shots"})
	})
	if code == 0 || !strings.Contains(stderr, "narration") {
		t.Fatalf("code = %d, stderr = %q", code, stderr)
	}
}

func TestReviewAnswerFileShowsTheImagesBesideIt(t *testing.T) {
	dbPath, tourID, shots := reviewCLIChapterFixture(t)
	s, _, reviews := documentImageServices(t, dbPath)
	ctx := context.Background()
	stepID, err := reviews.CreateChapter(ctx, tourID, service.ChapterArgs{Title: "Header", Includes: []service.ChapterInclude{{FilePath: "chapter.go", Hunk: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertReviewMessage(ctx, store.ReviewMessage{TaskID: "task-shots", TourID: tourID, StepID: stepID, Kind: "question", Author: "user", Body: "How does it look?"}, true); err != nil {
		t.Fatal(err)
	}
	answerPath := filepath.Join(shots, "answer.md")
	if err := os.WriteFile(answerPath, []byte("Like this:\n\n![After](after.png)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := runCLI([]string{"review", "answer", stepID, "--file", answerPath, "--task", "task-shots"}); code != 0 {
		t.Fatalf("answer code = %d", code)
	}

	view, err := reviews.Tour(ctx, tourID)
	if err != nil {
		t.Fatal(err)
	}
	answer := view.Messages[len(view.Messages)-1]
	if images := view.MessageImages[answer.ID]; answer.Kind != "answer" || len(images) != 1 || images[0].Name != "after.png" {
		t.Fatalf("answer = %+v, images = %+v", answer, images)
	}
}

func TestPlanAnswerTakesItsMarkdownFromAFileWithImagesBesideIt(t *testing.T) {
	dbPath, s := reviewCLITestStore(t)
	ctx := context.Background()
	if err := s.CreateTask(ctx, store.Task{ID: "task-1", Title: "Task", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	configureReviewCLITest(t, dbPath)
	_, plans, _ := documentImageServices(t, dbPath)
	submitted, err := plans.Submit(ctx, "task-1", "shots", writePlanBundle(t, "# Plan\n", nil))
	if err != nil {
		t.Fatal(err)
	}
	threadID, _ := plans.AskQuestion(ctx, submitted.Plan.ID, "What will it look like?")
	shots := t.TempDir()
	if err := os.WriteFile(filepath.Join(shots, "mockup.png"), cliTestPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	body := "Like this, with `$HOME` unexpanded:\n\n![Mockup](mockup.png)\n"
	answerPath := filepath.Join(shots, "answer.md")
	if err := os.WriteFile(answerPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := runCLI([]string{"plan", "answer", threadID, "--file", answerPath, "--task", "task-1", "--name", "shots"}); code != 0 {
		t.Fatalf("answer code = %d", code)
	}

	view, err := plans.Plan(ctx, submitted.Plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	answer := view.Messages[len(view.Messages)-1]
	if answer.Kind != "answer" || answer.Body != body {
		t.Fatalf("answer = %+v", answer)
	}
	if images := view.MessageImages[answer.ID]; len(images) != 1 || images[0].Name != "mockup.png" {
		t.Fatalf("answer images = %+v", images)
	}

	// The answer as an argument still works, and one way at a time.
	if code := runCLI([]string{"plan", "answer", threadID, "Plain text.", "--task", "task-1", "--name", "shots"}); code != 0 {
		t.Fatalf("argument answer code = %d", code)
	}
	if _, code := captureStderrWithCode(t, func() int {
		return runCLI([]string{"plan", "answer", threadID, "Both.", "--file", answerPath, "--task", "task-1", "--name", "shots"})
	}); code == 0 {
		t.Fatal("took an answer given both as an argument and as a file")
	}
}
