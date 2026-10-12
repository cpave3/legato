package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// A file http.DetectContentType reads as each kind of image: the kind is
// decided by the first bytes, so that is all these hold, plus a tail that
// tells one test image from another.
func testImage(kind, tail string) []byte {
	heads := map[string]string{
		"png":  "\x89PNG\r\n\x1a\n",
		"jpeg": "\xff\xd8\xff\xe0",
		"gif":  "GIF89a",
		"webp": "RIFF\x00\x00\x00\x00WEBPVP8 ",
		"bmp":  "BM\x00\x00",
	}
	return []byte(heads[kind] + tail)
}

func writeTestFile(t *testing.T, name string, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func newAgentUIServiceWithImages(t *testing.T) *service.AgentUIService {
	t.Helper()
	return service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{ImageDir: filepath.Join(t.TempDir(), "agent-ui-images")})
}

func readStoredImage(t *testing.T, ui *service.AgentUIService, url string) (content []byte, mediaType string) {
	t.Helper()
	taskID, file := filepath.Base(filepath.Dir(url)), filepath.Base(url)
	image, err := ui.OpenImage(taskID, file)
	if err != nil {
		t.Fatalf("open %s: %v", url, err)
	}
	defer image.Close()
	content, err = io.ReadAll(image)
	if err != nil {
		t.Fatal(err)
	}
	return content, image.MediaType
}

func TestAgentUIShowKeepsACopyOfAnImageFileAndNamesWhereItIsServed(t *testing.T) {
	ui := newAgentUIServiceWithImages(t)
	ctx := context.Background()
	content := testImage("png", "before the change")
	path := writeTestFile(t, "shot.png", content)

	shown, err := ui.Show(ctx, service.AgentUIShowInput{
		TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML,
		Title: "Shots", Content: `<img src="legato-image:before">`,
		Images: []service.AgentUIImageInput{{Name: "before", Path: path}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	want := []service.AgentUIImage{{Name: "before", URL: "/agent-ui-images/task-a/" + hex.EncodeToString(sum[:]) + ".png"}}
	if len(shown.Images) != 1 || shown.Images[0] != want[0] {
		t.Fatalf("shown images = %#v, want %#v", shown.Images, want)
	}

	// The copy is Legato's own: the file the agent named can change or go.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	got, err := ui.Get(ctx, shown.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Images) != 1 || got.Images[0] != want[0] {
		t.Fatalf("stored images = %#v, want %#v", got.Images, want)
	}
	// What the agent wrote is kept as it wrote it.
	if got.Content != `<img src="legato-image:before">` {
		t.Fatalf("content = %q", got.Content)
	}
	served, mediaType := readStoredImage(t, ui, got.Images[0].URL)
	if string(served) != string(content) || mediaType != "image/png" {
		t.Fatalf("served %q as %s", served, mediaType)
	}
}

func storedImageFiles(t *testing.T, ui *service.AgentUIService, dir, taskID string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, taskID))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestAgentUIShowRejectsImagesItCannotShowWithoutStoringAnything(t *testing.T) {
	good := writeTestFile(t, "good.png", testImage("png", "good"))
	notAnImage := writeTestFile(t, "secret.png", []byte("PRIVATE KEY, not a picture"))
	svg := writeTestFile(t, "drawing.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
	huge := writeTestFile(t, "huge.png", testImage("png", ""))
	if err := os.Truncate(huge, service.AgentUIMaxImageBytes+1); err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()

	cases := map[string]struct {
		content string
		images  []service.AgentUIImageInput
		reason  string
	}{
		"a file that is not an image": {images: []service.AgentUIImageInput{{Name: "a", Path: notAnImage}}, reason: "not an image"},
		"an SVG file":                 {images: []service.AgentUIImageInput{{Name: "a", Path: svg}}, reason: "not an image"},
		"a missing file":              {images: []service.AgentUIImageInput{{Name: "a", Path: filepath.Join(folder, "gone.png")}}, reason: "no file at"},
		"a folder":                    {images: []service.AgentUIImageInput{{Name: "a", Path: folder}}, reason: "no file at"},
		"a file over the limit":       {images: []service.AgentUIImageInput{{Name: "a", Path: huge}}, reason: "larger than 25 MB"},
		"a name with a space":         {images: []service.AgentUIImageInput{{Name: "my shot", Path: good}}, reason: "name"},
		"a name used twice":           {images: []service.AgentUIImageInput{{Name: "a", Path: good}, {Name: "a", Path: good}}, reason: "used twice"},
		"an image with no path":       {images: []service.AgentUIImageInput{{Name: "a", Path: " "}}, reason: "no path"},
		"a reference to an image that was not listed": {
			content: `<img src="legato-image:a"><img src="legato-image:after">`,
			images:  []service.AgentUIImageInput{{Name: "a", Path: good}}, reason: `legato-image:after`,
		},
		"a good image beside one that cannot be shown": {
			images: []service.AgentUIImageInput{{Name: "a", Path: good}, {Name: "b", Path: notAnImage}}, reason: `image "b"`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "agent-ui-images")
			ui := service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{ImageDir: dir})
			content := tc.content
			if content == "" {
				content = `<img src="legato-image:a">`
			}
			_, err := ui.Show(context.Background(), service.AgentUIShowInput{
				TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML,
				Title: "Shots", Content: content, Images: tc.images,
			})
			if !errors.Is(err, service.ErrAgentUIInvalid) || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("error = %v, want ErrAgentUIInvalid naming %q", err, tc.reason)
			}
			if items, _ := ui.List(context.Background(), "task-a"); len(items) != 0 {
				t.Fatalf("stored after rejection: %#v", items)
			}
			// Not the good image of a request that had a bad one either.
			if files := storedImageFiles(t, ui, dir, "task-a"); len(files) != 0 {
				t.Fatalf("files kept for a rejected request: %v", files)
			}
		})
	}

	t.Run("a Legato with no folder for images says so", func(t *testing.T) {
		ui := service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{})
		_, err := ui.Show(context.Background(), service.AgentUIShowInput{
			TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML,
			Title: "Shots", Content: "x", Images: []service.AgentUIImageInput{{Name: "a", Path: good}},
		})
		if err == nil || !strings.Contains(err.Error(), "no folder to keep images") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestAgentUIShowKeepsNothingOfARequestTurnedDownForItsContent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent-ui-images")
	ui := service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{ImageDir: dir})
	_, err := ui.Show(context.Background(), service.AgentUIShowInput{
		TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML,
		Title: "", Content: "x", Images: []service.AgentUIImageInput{{Name: "a", Path: writeTestFile(t, "a.png", testImage("png", "a"))}},
	})
	if !errors.Is(err, service.ErrAgentUIInvalid) {
		t.Fatalf("error = %v, want ErrAgentUIInvalid for the missing title", err)
	}
	if files := storedImageFiles(t, ui, dir, "task-a"); len(files) != 0 {
		t.Fatalf("files kept for a rejected request: %v", files)
	}
}

func TestAgentUIShowNamesAnImagesKindByItsContent(t *testing.T) {
	ui := newAgentUIServiceWithImages(t)
	extensions := map[string]string{"png": ".png", "jpeg": ".jpg", "gif": ".gif", "webp": ".webp", "bmp": ".bmp"}
	for kind, extension := range extensions {
		// Named as something else: the name does not decide.
		path := writeTestFile(t, "image.dat", testImage(kind, kind))
		shown, err := ui.Show(context.Background(), service.AgentUIShowInput{
			TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatMarkdown,
			Title: kind, Content: "![it](legato-image:it)", Images: []service.AgentUIImageInput{{Name: "it", Path: path}},
		})
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if got := filepath.Ext(shown.Images[0].URL); got != extension {
			t.Fatalf("%s is served as %s, want %s", kind, got, extension)
		}
	}
}

func TestAgentUIShowKeepsOneCopyOfAnImageShownAgain(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent-ui-images")
	ui := service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{ImageDir: dir})
	content := testImage("png", "same")
	show := func(path string) *service.AgentUIItem {
		t.Helper()
		shown, err := ui.Show(context.Background(), service.AgentUIShowInput{
			TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML, Key: "shots",
			Title: "Shots", Content: `<img src="legato-image:a">`, Images: []service.AgentUIImageInput{{Name: "a", Path: path}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return shown
	}
	first := show(writeTestFile(t, "one.png", content))
	second := show(writeTestFile(t, "two.png", content))
	if second.Revision != 2 || first.Images[0].URL != second.Images[0].URL {
		t.Fatalf("first = %#v, second = %#v", first, second)
	}
	if files := storedImageFiles(t, ui, dir, "task-a"); len(files) != 1 {
		t.Fatalf("copies kept = %v, want one", files)
	}
}

func TestAgentUIShowKeepsATasksMostRecentlyUsedImages(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent-ui-images")
	ui := service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{ImageDir: dir})
	source := t.TempDir()
	show := func(from, to int) *service.AgentUIItem {
		t.Helper()
		var images []service.AgentUIImageInput
		for i := from; i < to; i++ {
			path := filepath.Join(source, fmt.Sprintf("%03d.png", i))
			if err := os.WriteFile(path, testImage("png", fmt.Sprint(i)), 0o644); err != nil {
				t.Fatal(err)
			}
			images = append(images, service.AgentUIImageInput{Name: fmt.Sprintf("i%d", i), Path: path})
		}
		shown, err := ui.Show(context.Background(), service.AgentUIShowInput{
			TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML,
			Title: "Shots", Content: "<p>many</p>", Images: images,
		})
		if err != nil {
			t.Fatal(err)
		}
		return shown
	}
	oldest := show(0, 50)
	// The copies must be told apart by when they were used.
	old := time.Now().Add(-time.Hour)
	for _, name := range storedImageFiles(t, ui, dir, "task-a") {
		if err := os.Chtimes(filepath.Join(dir, "task-a", name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	// Shown again, so used now.
	show(0, 10)
	newest := show(50, 110)

	files := storedImageFiles(t, ui, dir, "task-a")
	if len(files) != 100 {
		t.Fatalf("a task keeps %d images, want 100", len(files))
	}
	kept := map[string]bool{}
	for _, name := range files {
		kept[name] = true
	}
	for _, image := range append(append([]service.AgentUIImage{}, oldest.Images[:10]...), newest.Images...) {
		if !kept[filepath.Base(image.URL)] {
			t.Fatalf("%s was removed although it was used recently", image.Name)
		}
	}
	// Ten of the forty that were not used again made room for the newest.
	stale := 0
	for _, image := range oldest.Images[10:] {
		if !kept[filepath.Base(image.URL)] {
			stale++
		}
	}
	if stale != 10 {
		t.Fatalf("%d images used long ago were removed, want 10", stale)
	}
}

func TestAgentUIOpenImageGivesOutOnlyStoredImages(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent-ui-images")
	ui := service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{ImageDir: dir})
	shown, err := ui.Show(context.Background(), service.AgentUIShowInput{
		TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML,
		Title: "Shots", Content: "x", Images: []service.AgentUIImageInput{{Name: "a", Path: writeTestFile(t, "a.png", testImage("png", "a"))}},
	})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Base(shown.Images[0].URL)
	// Something that is not an image, under a name that says it is one.
	planted := strings.Repeat("0", 64) + ".png"
	if err := os.WriteFile(filepath.Join(dir, "task-a", planted), []byte("not a picture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.png"), testImage("png", "outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string][2]string{
		"another task":             {"task-b", file},
		"a name that is no hash":   {"task-a", "shot.png"},
		"a path out of the folder": {"task-a", "../secret.png"},
		"a task that climbs out":   {"..", "secret.png"},
		"a file that is no image":  {"task-a", planted},
	} {
		if image, err := ui.OpenImage(request[0], request[1]); !errors.Is(err, store.ErrNotFound) {
			image.Close()
			t.Fatalf("%s: error = %v, want ErrNotFound", name, err)
		}
	}
}

func TestAgentUIShowKeepsTheImagesOfImageComponents(t *testing.T) {
	ui := newAgentUIServiceWithImages(t)
	ctx := context.Background()
	hero := writeTestFile(t, "hero.png", testImage("png", "hero"))
	before := writeTestFile(t, "before.jpg", testImage("jpeg", "before"))
	after := writeTestFile(t, "after.gif", testImage("gif", "after"))

	shown, err := ui.Show(ctx, service.AgentUIShowInput{
		TaskID: "task-a", Placement: service.AgentUIInline, Format: service.AgentUIFormatComponents,
		FallbackText: "Three screenshots of the board",
		Components: []service.UIComponent{
			{Type: "image", Path: hero, Alt: "The board", Caption: "After the restyle"},
			{Type: "image_grid", Items: []service.UIComponentItem{
				{Path: before, Alt: "Before", Caption: "Monday"},
				{Path: after, Alt: "After"},
				// The same file again is one image.
				{Path: hero},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ui.Get(ctx, shown.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	// A component names its image by the path the agent gave.
	served := map[string]string{}
	for _, image := range got.Images {
		served[image.Name] = image.URL
	}
	if len(got.Images) != 3 || filepath.Ext(served[hero]) != ".png" || filepath.Ext(served[before]) != ".jpg" || filepath.Ext(served[after]) != ".gif" {
		t.Fatalf("images = %#v", got.Images)
	}
	if content, _ := readStoredImage(t, ui, served[before]); string(content) != string(testImage("jpeg", "before")) {
		t.Fatalf("stored copy of before.jpg = %q", content)
	}
	var stored []service.UIComponent
	if err := json.Unmarshal([]byte(got.Content), &stored); err != nil {
		t.Fatal(err)
	}
	if stored[0].Path != hero || stored[0].Alt != "The board" || stored[0].Caption != "After the restyle" {
		t.Fatalf("image component = %#v", stored[0])
	}
	if first := stored[1].Items[0]; first.Path != before || first.Alt != "Before" || first.Caption != "Monday" {
		t.Fatalf("grid item = %#v", first)
	}
}

func TestAgentUIShowRejectsImageComponentsThatCannotBeDrawn(t *testing.T) {
	good := writeTestFile(t, "good.png", testImage("png", "good"))
	text := writeTestFile(t, "notes.png", []byte("just text"))
	many := make([]service.UIComponentItem, 25)
	for i := range many {
		many[i] = service.UIComponentItem{Path: good}
	}
	cases := map[string]struct {
		components []service.UIComponent
		images     []service.AgentUIImageInput
		wantInErr  string
	}{
		"image no path":     {components: []service.UIComponent{{Type: "image", Alt: "x"}}, wantInErr: "components[0] (image): path is required"},
		"grid empty":        {components: []service.UIComponent{{Type: "image_grid"}}, wantInErr: "components[0] (image_grid): items is required"},
		"grid item no path": {components: []service.UIComponent{{Type: "image_grid", Items: []service.UIComponentItem{{Path: good}, {Alt: "x"}}}}, wantInErr: "components[0] (image_grid): items[1]: path is required"},
		"grid too large":    {components: []service.UIComponent{{Type: "image_grid", Items: many}}, wantInErr: "components[0] (image_grid): at most 24 items"},
		"not an image":      {components: []service.UIComponent{{Type: "image", Path: text}}, wantInErr: "is not an image Legato can show"},
		"images beside components": {
			components: []service.UIComponent{{Type: "image", Path: good}},
			images:     []service.AgentUIImageInput{{Name: "a", Path: good}}, wantInErr: "image components",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ui := newAgentUIServiceWithImages(t)
			_, err := ui.Show(context.Background(), service.AgentUIShowInput{
				TaskID: "task-a", Placement: service.AgentUIInline, Format: service.AgentUIFormatComponents,
				FallbackText: "fallback", Components: tc.components, Images: tc.images,
			})
			if !errors.Is(err, service.ErrAgentUIInvalid) || !strings.Contains(err.Error(), tc.wantInErr) {
				t.Fatalf("error = %v, want ErrAgentUIInvalid containing %q", err, tc.wantInErr)
			}
		})
	}
}

func TestAgentUIShowTakesComponentsAsAnArtifact(t *testing.T) {
	ui := newAgentUIServiceWithImages(t)
	ctx := context.Background()
	path := writeTestFile(t, "a.png", testImage("png", "a"))
	components := []service.UIComponent{
		{Type: "heading", Text: "Screenshots"},
		{Type: "image_grid", Items: []service.UIComponentItem{{Path: path, Alt: "The board"}}},
	}

	shown, err := ui.Show(ctx, service.AgentUIShowInput{
		TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatComponents,
		Title: "Screenshots", Components: components,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ui.Get(ctx, shown.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Placement != service.AgentUIPanel || got.Format != service.AgentUIFormatComponents || len(got.Images) != 1 {
		t.Fatalf("artifact = %#v", got)
	}

	// An artifact is known by its title, whatever it is made of.
	_, err = ui.Show(ctx, service.AgentUIShowInput{
		TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatComponents, Components: components,
	})
	if !errors.Is(err, service.ErrAgentUIInvalid) || !strings.Contains(err.Error(), "title") {
		t.Fatalf("error = %v, want a missing title", err)
	}
}

func TestAgentUIRemoveOrphanImagesClearsWhatNoItemUsesAnyMore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent-ui-images")
	ui := service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{ImageDir: dir})
	ctx := context.Background()
	show := func(taskID string) *service.AgentUIItem {
		t.Helper()
		shown, err := ui.Show(ctx, service.AgentUIShowInput{
			TaskID: taskID, Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML,
			Title: "Shots", Content: "x", Images: []service.AgentUIImageInput{{Name: "a", Path: writeTestFile(t, "a.png", testImage("png", taskID))}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return shown
	}
	age := func(taskID string) {
		t.Helper()
		old := time.Now().Add(-2 * time.Hour)
		if err := os.Chtimes(filepath.Join(dir, taskID), old, old); err != nil {
			t.Fatal(err)
		}
	}
	show("kept")
	removed := show("removed")
	// Shown a moment ago and then removed: another process may be half way
	// through showing something for this task.
	recent := show("recent")
	for _, item := range []*service.AgentUIItem{removed, recent} {
		if err := ui.Delete(ctx, item.ID); err != nil {
			t.Fatal(err)
		}
	}
	age("kept")
	age("removed")

	if err := ui.RemoveOrphanImages(ctx); err != nil {
		t.Fatal(err)
	}
	if files := storedImageFiles(t, ui, dir, "removed"); files != nil {
		t.Fatalf("images of a task with nothing shown are still there: %v", files)
	}
	for _, taskID := range []string{"kept", "recent"} {
		if files := storedImageFiles(t, ui, dir, taskID); len(files) != 1 {
			t.Fatalf("%s: images = %v, want its one image", taskID, files)
		}
	}
}

func TestAgentUIGetLeavesOutAnImageWhoseCopyIsGone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent-ui-images")
	ui := service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{ImageDir: dir})
	ctx := context.Background()
	shown, err := ui.Show(ctx, service.AgentUIShowInput{
		TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML, Title: "Shots", Content: "x",
		Images: []service.AgentUIImageInput{
			{Name: "kept", Path: writeTestFile(t, "a.png", testImage("png", "kept"))},
			{Name: "gone", Path: writeTestFile(t, "b.png", testImage("png", "gone"))},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// As when the task has shown many images since: its oldest copies go.
	if err := os.Remove(filepath.Join(dir, "task-a", filepath.Base(shown.Images[1].URL))); err != nil {
		t.Fatal(err)
	}

	got, err := ui.Get(ctx, shown.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	// A client told of no image says so; told of one that is gone, it would
	// draw a broken picture.
	if len(got.Images) != 1 || got.Images[0].Name != "kept" {
		t.Fatalf("images = %#v, want only the one still stored", got.Images)
	}
}
