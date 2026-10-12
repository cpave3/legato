package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// newReviewImageFixture is a review of a repository with one changed file,
// and a review service with folders to keep its copies of image files in.
func newReviewImageFixture(t *testing.T) (f *reviewFixture, imageDir string) {
	t.Helper()
	f = newReviewFixture(t)
	imageDir = filepath.Join(t.TempDir(), "review-images")
	f.svc.SetImageDirs(imageDir, filepath.Join(t.TempDir(), "plan-images"))
	writeRepoFile(t, f.repo, "a.go", "package a\n")
	gitCommitAll(t, f.repo, "add a")
	return f, imageDir
}

func writeImageFile(t *testing.T, dir, name string, content []byte) string {
	t.Helper()
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestChapterNamesWhereItsNarrationsImagesAreServed(t *testing.T) {
	f, _ := newReviewImageFixture(t)
	ctx := context.Background()
	shots := t.TempDir()
	before := writeImageFile(t, shots, "before.png", pngImage("before"))
	writeImageFile(t, shots, "after.png", pngImage("after"))

	// One image by its absolute path, one relative to where the narration
	// was written.
	narration := "Before:\n\n![Before](" + before + ")\n\nAfter:\n\n![After](after.png)\n"
	chapterID, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "The new header", Narration: narration, NarrationDir: shots,
		Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}

	view, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	images := view.StepImages[chapterID]
	if len(images) != 2 || images[0].Name != before || images[1].Name != "after.png" {
		t.Fatalf("chapter images = %+v", images)
	}
	if got := readDocumentImage(t, f.svc, ReviewImageRoute, images[1].URL); string(got) != string(pngImage("after")) {
		t.Fatalf("served image = %q", got)
	}
	// The narration is stored as it was written.
	for _, step := range view.Steps {
		if step.ID == chapterID && step.Narration != narration {
			t.Fatalf("narration = %q", step.Narration)
		}
	}
}

func TestChapterTurnsDownAnImageItCannotShowAndStoresNothing(t *testing.T) {
	f, imageDir := newReviewImageFixture(t)
	ctx := context.Background()
	shots := t.TempDir()
	writeImageFile(t, shots, "good.png", pngImage("good"))
	writeImageFile(t, shots, "notes.png", []byte("plain text"))

	_, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "The new header", Narration: "![ok](good.png)\n\n![text](notes.png)\n\n![gone](missing.png)\n", NarrationDir: shots,
		Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}},
	})
	if err == nil {
		t.Fatal("made a chapter whose images cannot be shown")
	}
	for _, want := range []string{"narration:3", "notes.png", "not an image", "narration:5", "missing.png", "there is no file"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not say %q", err, want)
		}
	}
	view, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range view.Steps {
		if step.Kind == "chapter" && step.Title == "The new header" {
			t.Fatalf("chapter stored after a refused image: %+v", step)
		}
	}
	if files := keptFiles(t, imageDir); len(files) != 0 {
		t.Fatalf("copies left behind: %v", files)
	}
}

func TestEditingAChaptersNarrationReplacesItsImages(t *testing.T) {
	f, _ := newReviewImageFixture(t)
	ctx := context.Background()
	shots := t.TempDir()
	writeImageFile(t, shots, "first.png", pngImage("first"))
	writeImageFile(t, shots, "second.png", pngImage("second"))
	chapterID, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "Header", Narration: "![one](first.png)", NarrationDir: shots,
		Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}

	narration := "![two](second.png)"
	if _, err := f.svc.EditChapter(ctx, f.tourID, chapterID, ChapterEditArgs{Narration: &narration, NarrationDir: shots}); err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if images := view.StepImages[chapterID]; len(images) != 1 || images[0].Name != "second.png" {
		t.Fatalf("images after the edit = %+v", images)
	}

	// An edit that leaves the narration alone leaves its images alone.
	title := "Header, renamed"
	if _, err := f.svc.EditChapter(ctx, f.tourID, chapterID, ChapterEditArgs{Title: &title}); err != nil {
		t.Fatal(err)
	}
	view, err = f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	if images := view.StepImages[chapterID]; len(images) != 1 || images[0].Name != "second.png" {
		t.Fatalf("images after a title edit = %+v", images)
	}
}

func TestReviewAnswerNamesWhereItsImagesAreServed(t *testing.T) {
	f, _ := newReviewImageFixture(t)
	ctx := context.Background()
	shots := t.TempDir()
	writeImageFile(t, shots, "now.png", pngImage("now"))
	chapterID, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{Title: "Header", Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	// No agent is running here; the question is stored all the same.
	if err := f.svc.AskQuestion(ctx, f.tourID, chapterID, ReviewQuestion{Text: "how does it look?"}); err != nil && !errors.Is(err, ErrAgentOffline) {
		t.Fatal(err)
	}

	if err := f.svc.AnswerFrom(ctx, f.tourID, chapterID, "Like this:\n\n![Now](now.png)\n", shots); err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	var answer *store.ReviewMessage
	for i := range view.Messages {
		if view.Messages[i].Kind == "answer" {
			answer = &view.Messages[i]
		}
	}
	if answer == nil {
		t.Fatalf("no answer in %+v", view.Messages)
	}
	images := view.MessageImages[answer.ID]
	if len(images) != 1 || images[0].Name != "now.png" {
		t.Fatalf("answer images = %+v", images)
	}
	if got := readDocumentImage(t, f.svc, ReviewImageRoute, images[0].URL); string(got) != string(pngImage("now")) {
		t.Fatalf("served image = %q", got)
	}

	if err := f.svc.AnswerFrom(ctx, f.tourID, chapterID, "![gone](missing.png)", shots); err == nil || !strings.Contains(err.Error(), "there is no file") {
		t.Fatalf("error for a missing image = %v", err)
	}
}

func TestRemovingATourRemovesItsImages(t *testing.T) {
	f, imageDir := newReviewImageFixture(t)
	ctx := context.Background()
	shots := t.TempDir()
	writeImageFile(t, shots, "first.png", pngImage("first"))
	if _, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "Header", Narration: "![one](first.png)", NarrationDir: shots,
		Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	if files := keptFiles(t, imageDir); len(files) != 1 {
		t.Fatalf("kept files = %v, want one", files)
	}

	if err := f.svc.Delete(ctx, f.tourID); err != nil {
		t.Fatal(err)
	}
	if files := keptFiles(t, imageDir); len(files) != 0 {
		t.Fatalf("copies left after the tour was removed: %v", files)
	}
}

func TestReviewPassShowsTheCapturedPlansImages(t *testing.T) {
	s, plans, bundle := newPlanFixture(t)
	planImages := filepath.Join(t.TempDir(), "plan-images")
	plans.SetImageDir(planImages)
	writeBundleFile(t, bundle, "assets/board.png", pngImage("board"))
	writeBundleFile(t, bundle, "plan.md", []byte("# Plan\n\n![The board](assets/board.png)\n"))
	ctx := context.Background()
	planView, err := plans.Submit(ctx, "task-1", "shots", bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := plans.Respond(ctx, planView.Plan.ID, "backend", PlanResponseInput{Values: []string{"sqlite"}}); err != nil {
		t.Fatal(err)
	}
	if err := plans.Approve(ctx, planView.Plan.ID, false); err != nil {
		t.Fatal(err)
	}

	reviews := NewReviewService(s, nil, nil)
	reviews.SetImageDirs(filepath.Join(t.TempDir(), "review-images"), planImages)
	tour, err := s.EnsureReviewTour(ctx, "task-1", "")
	if err != nil {
		t.Fatal(err)
	}
	view, err := reviews.Tour(ctx, tour.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Passes) != 1 || view.Passes[0].CapturedPlan == nil {
		t.Fatalf("passes = %+v", view.Passes)
	}
	images := view.PlanImages[view.Passes[0].Pass.ID]
	if len(images) != 1 || images[0] != planView.Images[0] {
		t.Fatalf("captured plan's images = %+v, want %+v", images, planView.Images)
	}
}

func TestSweepRemovesReviewImagesThatNothingNames(t *testing.T) {
	f, imageDir := newReviewImageFixture(t)
	ctx := context.Background()
	shots := t.TempDir()
	writeImageFile(t, shots, "first.png", pngImage("first"))
	writeImageFile(t, shots, "second.png", pngImage("second"))
	chapterID, err := f.svc.CreateChapter(ctx, f.tourID, ChapterArgs{
		Title: "Header", Narration: "![one](first.png)", NarrationDir: shots,
		Includes: []ChapterInclude{{FilePath: "a.go", Hunk: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// The edit leaves the first image's copy behind, named by nothing.
	narration := "![two](second.png)"
	if _, err := f.svc.EditChapter(ctx, f.tourID, chapterID, ChapterEditArgs{Narration: &narration, NarrationDir: shots}); err != nil {
		t.Fatal(err)
	}
	kept := keptFiles(t, imageDir)
	if len(kept) != 2 {
		t.Fatalf("kept files = %v, want two", kept)
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, file := range kept {
		if err := os.Chtimes(file, old, old); err != nil {
			t.Fatal(err)
		}
	}

	if err := f.svc.RemoveOrphanImages(ctx); err != nil {
		t.Fatal(err)
	}

	view, err := f.svc.Tour(ctx, f.tourID)
	if err != nil {
		t.Fatal(err)
	}
	images := view.StepImages[chapterID]
	if len(images) != 1 || images[0].Name != "second.png" {
		t.Fatalf("images after the sweep = %+v", images)
	}
	if kept := keptFiles(t, imageDir); len(kept) != 1 {
		t.Fatalf("kept files after the sweep = %v, want one", kept)
	}
}
