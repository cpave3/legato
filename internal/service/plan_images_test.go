package service

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// The first bytes decide what kind of file Legato takes something for, so
// that is all a test image holds, plus a tail that tells one from another.
func pngImage(tail string) []byte { return []byte("\x89PNG\r\n\x1a\n" + tail) }

// newPlanImageFixture is a plan bundle whose Markdown can name image files,
// and a plan service with a folder to keep its copies in.
func newPlanImageFixture(t *testing.T) (svc *PlanService, bundle, imageDir string) {
	t.Helper()
	_, svc, bundle = newPlanFixture(t)
	imageDir = filepath.Join(t.TempDir(), "plan-images")
	svc.SetImageDir(imageDir)
	return svc, bundle, imageDir
}

func writeBundleFile(t *testing.T, bundle, name string, content []byte) {
	t.Helper()
	target := filepath.Join(bundle, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// keptFiles lists every file under a folder of kept images.
func keptFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(file string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !entry.IsDir() {
			files = append(files, file)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

type imageOpener interface {
	OpenImage(folder, file string) (*AgentUIImageFile, error)
}

// readDocumentImage reads Legato's copy of an image through the door the
// web server uses, from the address a view named.
func readDocumentImage(t *testing.T, opener imageOpener, route, url string) []byte {
	t.Helper()
	if !strings.HasPrefix(url, route) {
		t.Fatalf("image address %q is not under %s", url, route)
	}
	image, err := opener.OpenImage(path.Base(path.Dir(url)), path.Base(url))
	if err != nil {
		t.Fatalf("open %s: %v", url, err)
	}
	defer image.Close()
	content, err := io.ReadAll(image)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func TestSubmittedPlanNamesWhereItsImagesAreServed(t *testing.T) {
	svc, bundle, _ := newPlanImageFixture(t)
	writeBundleFile(t, bundle, "assets/board.png", pngImage("board"))
	writeBundleFile(t, bundle, "plan.md", []byte("# Plan\n\n![The board today](assets/board.png)\n\n![Elsewhere](https://example.com/remote.png)\n"))

	view, err := svc.Submit(context.Background(), "task-1", "shots", bundle)
	if err != nil {
		t.Fatal(err)
	}
	// The Markdown is stored as written: comments are anchored to its bytes.
	if !strings.Contains(view.Revision.Markdown, "![The board today](assets/board.png)") {
		t.Fatalf("markdown = %q", view.Revision.Markdown)
	}
	// An image at a web address is not a file of the plan's.
	if len(view.Images) != 1 || view.Images[0].Name != "assets/board.png" {
		t.Fatalf("images = %+v", view.Images)
	}
	if got := readDocumentImage(t, svc, PlanImageRoute, view.Images[0].URL); string(got) != string(pngImage("board")) {
		t.Fatalf("served image = %q", got)
	}

	// The copy is Legato's own: the bundle can change or go afterwards.
	if err := os.RemoveAll(filepath.Join(bundle, "assets")); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.Plan(context.Background(), view.Plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Images) != 1 || stored.Images[0] != view.Images[0] {
		t.Fatalf("stored images = %+v", stored.Images)
	}
}

func TestSubmitTurnsDownAnImageItCannotShowAndStoresNothing(t *testing.T) {
	elsewhere := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(elsewhere, pngImage("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		markdown string
		arrange  func(t *testing.T, bundle string)
		want     string
	}{
		{name: "a missing file", markdown: "# Plan\n\n![a](assets/missing.png)\n", want: "there is no file"},
		{name: "a file that is not an image", markdown: "# Plan\n\n![a](assets/notes.png)\n", want: "not an image",
			arrange: func(t *testing.T, bundle string) {
				writeBundleFile(t, bundle, "assets/notes.png", []byte("plain text, whatever its name says"))
			}},
		{name: "an SVG", markdown: "# Plan\n\n![a](assets/drawing.svg)\n", want: "not an image",
			arrange: func(t *testing.T, bundle string) {
				writeBundleFile(t, bundle, "assets/drawing.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`))
			}},
		{name: "a path that climbs out of the bundle", markdown: "# Plan\n\n![a](../outside.png)\n", want: "inside the plan bundle"},
		{name: "an absolute path", markdown: "# Plan\n\n![a](" + elsewhere + ")\n", want: "inside the plan bundle"},
		{name: "a link in the bundle that leads out of it", markdown: "# Plan\n\n![a](assets/link.png)\n", want: "inside the plan bundle",
			arrange: func(t *testing.T, bundle string) {
				writeBundleFile(t, bundle, "assets/keep", nil)
				if err := os.Symlink(elsewhere, filepath.Join(bundle, "assets", "link.png")); err != nil {
					t.Fatal(err)
				}
			}},
		{name: "a file over the size limit", markdown: "# Plan\n\n![a](assets/huge.png)\n", want: "larger than",
			arrange: func(t *testing.T, bundle string) {
				writeBundleFile(t, bundle, "assets/huge.png", nil)
				if err := os.Truncate(filepath.Join(bundle, "assets", "huge.png"), AgentUIMaxImageBytes+1); err != nil {
					t.Fatal(err)
				}
			}},
		{name: "a good image beside a bad one", markdown: "# Plan\n\n![a](assets/good.png)\n\n![b](assets/missing.png)\n", want: "there is no file",
			arrange: func(t *testing.T, bundle string) {
				writeBundleFile(t, bundle, "assets/good.png", pngImage("good"))
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, bundle, imageDir := newPlanImageFixture(t)
			if tc.arrange != nil {
				tc.arrange(t, bundle)
			}
			writeBundleFile(t, bundle, "plan.md", []byte(tc.markdown))

			_, err := svc.Submit(context.Background(), "task-1", "shots", bundle)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one that says %q", err, tc.want)
			}
			if _, resolveErr := svc.Resolve(context.Background(), "task-1", "shots"); !errors.Is(resolveErr, store.ErrNotFound) {
				t.Fatalf("plan stored after a refused image: %v", resolveErr)
			}
			if files := keptFiles(t, imageDir); len(files) != 0 {
				t.Fatalf("copies left behind: %v", files)
			}
		})
	}
}

func TestSubmitNamesTheLineOfEachImageItTurnsDown(t *testing.T) {
	svc, bundle, _ := newPlanImageFixture(t)
	writeBundleFile(t, bundle, "plan.md", []byte("# Plan\n\n![a](assets/one.png)\n\nText.\n\n![b](assets/two.png)\n"))

	_, err := svc.Submit(context.Background(), "task-1", "shots", bundle)
	if err == nil {
		t.Fatal("submitted a plan whose images are missing")
	}
	for _, want := range []string{"plan.md:3", "assets/one.png", "plan.md:7", "assets/two.png"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
}

func TestSubmitWithoutAFolderForImagesTurnsDownAPlanThatHasThem(t *testing.T) {
	_, svc, bundle := newPlanFixture(t)
	writeBundleFile(t, bundle, "assets/board.png", pngImage("board"))
	writeBundleFile(t, bundle, "plan.md", []byte("# Plan\n\n![a](assets/board.png)\n"))

	if _, err := svc.Submit(context.Background(), "task-1", "shots", bundle); err == nil || !strings.Contains(err.Error(), "no folder to keep images") {
		t.Fatalf("error = %v", err)
	}
}

func TestRevisionsWithTheSameImageKeepOneCopy(t *testing.T) {
	svc, bundle, imageDir := newPlanImageFixture(t)
	writeBundleFile(t, bundle, "assets/board.png", pngImage("board"))
	writeBundleFile(t, bundle, "plan.md", []byte("# Plan\n\n![a](assets/board.png)\n"))
	first, err := svc.Submit(context.Background(), "task-1", "shots", bundle)
	if err != nil {
		t.Fatal(err)
	}
	// The same picture under another name, and the plan's text revised.
	writeBundleFile(t, bundle, "assets/renamed.png", pngImage("board"))
	writeBundleFile(t, bundle, "plan.md", []byte("# Plan, revised\n\n![a](assets/renamed.png)\n"))
	second, err := svc.Submit(context.Background(), "task-1", "shots", bundle)
	if err != nil {
		t.Fatal(err)
	}

	if second.Plan.LatestRevision != 2 || len(second.Images) != 1 || second.Images[0].Name != "assets/renamed.png" {
		t.Fatalf("second revision = %+v, images = %+v", second.Plan, second.Images)
	}
	if second.Images[0].URL != first.Images[0].URL {
		t.Fatalf("the same content is served from %q and %q", first.Images[0].URL, second.Images[0].URL)
	}
	if files := keptFiles(t, imageDir); len(files) != 1 {
		t.Fatalf("kept files = %v, want one", files)
	}
}

func TestWithdrawingAPlanRemovesItsImages(t *testing.T) {
	svc, bundle, imageDir := newPlanImageFixture(t)
	writeBundleFile(t, bundle, "assets/board.png", pngImage("board"))
	writeBundleFile(t, bundle, "plan.md", []byte("# Plan\n\n![a](assets/board.png)\n"))
	view, err := svc.Submit(context.Background(), "task-1", "shots", bundle)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Withdraw(context.Background(), view.Plan.ID); err != nil {
		t.Fatal(err)
	}
	if files := keptFiles(t, imageDir); len(files) != 0 {
		t.Fatalf("copies left after the plan was withdrawn: %v", files)
	}
}

func TestPlanLeavesOutAnImageWhoseCopyIsGone(t *testing.T) {
	svc, bundle, imageDir := newPlanImageFixture(t)
	writeBundleFile(t, bundle, "assets/board.png", pngImage("board"))
	writeBundleFile(t, bundle, "plan.md", []byte("# Plan\n\n![a](assets/board.png)\n"))
	view, err := svc.Submit(context.Background(), "task-1", "shots", bundle)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range keptFiles(t, imageDir) {
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
	}

	stored, err := svc.Plan(context.Background(), view.Plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Images) != 0 {
		t.Fatalf("images = %+v, want none", stored.Images)
	}
}

func TestOpenImageGivesOutOnlyAPlansOwnCopies(t *testing.T) {
	svc, bundle, imageDir := newPlanImageFixture(t)
	writeBundleFile(t, bundle, "assets/board.png", pngImage("board"))
	writeBundleFile(t, bundle, "plan.md", []byte("# Plan\n\n![a](assets/board.png)\n"))
	view, err := svc.Submit(context.Background(), "task-1", "shots", bundle)
	if err != nil {
		t.Fatal(err)
	}
	folder, file := path.Base(path.Dir(view.Images[0].URL)), path.Base(view.Images[0].URL)
	// Something that is not an image, under a name that says it is one.
	impostor := strings.Repeat("0", 64) + ".png"
	if err := os.WriteFile(filepath.Join(imageDir, folder, impostor), []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, request := range map[string][2]string{
		"another plan's folder":      {"pl-other", file},
		"a name that is not a hash":  {folder, "board.png"},
		"a path that climbs out":     {folder, "../" + folder + "/" + file},
		"a folder that climbs out":   {"..", file},
		"content that is no image":   {folder, impostor},
		"a hash nothing was kept as": {folder, strings.Repeat("a", 64) + ".png"},
	} {
		if image, err := svc.OpenImage(request[0], request[1]); !errors.Is(err, store.ErrNotFound) {
			if image != nil {
				image.Close()
			}
			t.Errorf("%s: error = %v, want not found", name, err)
		}
	}
}

func TestPlanImageProblemsAreFoundWithNoTaskAndNothingStored(t *testing.T) {
	bundle := t.TempDir()
	writeBundleFile(t, bundle, "assets/good.png", pngImage("good"))
	writeBundleFile(t, bundle, "assets/notes.png", []byte("plain text"))
	markdown := []byte("# Plan\n\n![fine](assets/good.png)\n\n![gone](assets/missing.png)\n\n![text](assets/notes.png)\n\n![remote](https://example.com/x.png)\n")

	problems := PlanImageProblems(markdown, bundle)

	if len(problems) != 2 {
		t.Fatalf("problems = %+v", problems)
	}
	if problems[0].Line != 5 || !strings.Contains(problems[0].Message, "assets/missing.png") || !strings.Contains(problems[0].Message, "there is no file") {
		t.Errorf("first problem = %+v", problems[0])
	}
	if problems[1].Line != 7 || !strings.Contains(problems[1].Message, "assets/notes.png") || !strings.Contains(problems[1].Message, "not an image") {
		t.Errorf("second problem = %+v", problems[1])
	}
	if PlanImageProblems([]byte("# Plan\n\n![fine](assets/good.png)\n"), bundle) != nil {
		t.Error("a plan whose images are all there has problems")
	}
}

func TestPlanAnswerNamesWhereItsImagesAreServed(t *testing.T) {
	svc, bundle, imageDir := newPlanImageFixture(t)
	ctx := context.Background()
	view, err := svc.Submit(ctx, "task-1", "shots", bundle)
	if err != nil {
		t.Fatal(err)
	}
	// No agent is running here; the question is stored all the same.
	threadID, err := svc.AskQuestion(ctx, view.Plan.ID, "what will it look like?")
	if err != nil && !errors.Is(err, ErrAgentOffline) {
		t.Fatal(err)
	}
	shots := t.TempDir()
	if err := os.WriteFile(filepath.Join(shots, "mockup.png"), pngImage("mockup"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := svc.AnswerFrom(ctx, view.Plan.ID, threadID, "Like this:\n\n![Mockup](mockup.png)\n", shots); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.Plan(ctx, view.Plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	answer := stored.Messages[len(stored.Messages)-1]
	images := stored.MessageImages[answer.ID]
	if answer.Kind != "answer" || len(images) != 1 || images[0].Name != "mockup.png" {
		t.Fatalf("answer = %+v, images = %+v", answer, images)
	}
	if got := readDocumentImage(t, svc, PlanImageRoute, images[0].URL); string(got) != string(pngImage("mockup")) {
		t.Fatalf("served image = %q", got)
	}

	before := len(keptFiles(t, imageDir))
	if err := svc.AnswerFrom(ctx, view.Plan.ID, threadID, "![gone](missing.png)", shots); err == nil || !strings.Contains(err.Error(), "there is no file") {
		t.Fatalf("error for a missing image = %v", err)
	}
	if after := len(keptFiles(t, imageDir)); after != before {
		t.Fatalf("a refused answer changed the kept files: %d, then %d", before, after)
	}
}

func TestSweepRemovesPlanImagesThatNothingNames(t *testing.T) {
	svc, bundle, imageDir := newPlanImageFixture(t)
	ctx := context.Background()
	writeBundleFile(t, bundle, "assets/board.png", pngImage("board"))
	writeBundleFile(t, bundle, "plan.md", []byte("# Plan\n\n![a](assets/board.png)\n"))
	view, err := svc.Submit(ctx, "task-1", "shots", bundle)
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(imageDir, path.Base(path.Dir(view.Images[0].URL)))
	named := filepath.Join(folder, path.Base(view.Images[0].URL))
	// A copy no revision names, a copy just made that nothing names yet, and
	// the folder of a plan that is gone.
	stale := filepath.Join(folder, strings.Repeat("1", 64)+".png")
	fresh := filepath.Join(folder, strings.Repeat("2", 64)+".png")
	gone := filepath.Join(imageDir, "pl-gone")
	orphan := filepath.Join(gone, strings.Repeat("3", 64)+".png")
	if err := os.MkdirAll(gone, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{stale, fresh, orphan} {
		if err := os.WriteFile(file, pngImage("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, file := range []string{named, stale, orphan, gone} {
		if err := os.Chtimes(file, old, old); err != nil {
			t.Fatal(err)
		}
	}

	if err := svc.RemoveOrphanImages(ctx); err != nil {
		t.Fatal(err)
	}

	for file, want := range map[string]bool{named: true, fresh: true, stale: false, orphan: false, gone: false} {
		_, err := os.Stat(file)
		if exists := err == nil; exists != want {
			t.Errorf("%s: exists = %v, want %v", file, exists, want)
		}
	}
}
