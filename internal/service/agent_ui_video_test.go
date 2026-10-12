package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// testVideo is a file http.DetectContentType reads as an MP4: an ftyp box
// that names an mp4 brand, and a tail that tells one test video from
// another.
func testVideo(tail string) []byte {
	return []byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isommp41" + tail)
}

const testVideoManifest = `{"version":1,"video":"tour.mp4","durationMs":4000,"chapters":[{"title":"Start","startMs":0,"endMs":4000}],"scenes":[{"id":"intro","kind":"card","startMs":0,"durationMs":4000}]}`

func newAgentUIServiceWithVideos(t *testing.T) (*service.AgentUIService, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "agent-ui-videos")
	return service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{VideoDir: dir}), dir
}

func showVideo(ui *service.AgentUIService, path, manifest, key string) (*service.AgentUIItem, error) {
	return ui.Show(context.Background(), service.AgentUIShowInput{
		TaskID: "task-1", Placement: service.AgentUIPanel, Format: service.AgentUIFormatVideo,
		Title: "Tour", Key: key, Content: manifest, Video: path,
	})
}

func TestAgentUIShowKeepsACopyOfAVideoAndNamesWhereItIsServed(t *testing.T) {
	ui, dir := newAgentUIServiceWithVideos(t)
	content := testVideo("first cut")
	path := writeTestFile(t, "tour.mp4", content)

	shown, err := showVideo(ui, path, testVideoManifest, "tour")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	want := service.AgentUIVideoRoute + "task-1/" + hex.EncodeToString(sum[:]) + ".mp4"
	if shown.Video == nil || shown.Video.URL != want {
		t.Fatalf("video = %+v, want the address %s", shown.Video, want)
	}
	if len(shown.Images) != 0 {
		t.Errorf("a video item lists images: %+v", shown.Images)
	}

	// The file can go: Legato has its own copy.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	item, err := ui.Get(context.Background(), shown.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if item.Format != service.AgentUIFormatVideo || item.Content != testVideoManifest || item.Video == nil || item.Video.URL != want {
		t.Fatalf("item = format %q, video %+v", item.Format, item.Video)
	}
	video, err := ui.OpenVideo("task-1", filepath.Base(want))
	if err != nil {
		t.Fatal(err)
	}
	defer video.Close()
	got, _ := io.ReadAll(video)
	if string(got) != string(content) || video.MediaType != "video/mp4" {
		t.Errorf("served %d bytes of %s", len(got), video.MediaType)
	}
	info, err := os.Stat(filepath.Join(dir, "task-1", filepath.Base(want)))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the copy has mode %v, err %v; want 0600", info.Mode().Perm(), err)
	}

	// The list of what a session showed names the video too.
	items, err := ui.List(context.Background(), "task-1")
	if err != nil || len(items) != 1 || items[0].Video == nil || len(items[0].Images) != 0 {
		t.Errorf("list = %+v, err %v", items, err)
	}
}

func TestAgentUIShowingAVideoAgainUnderItsKeyMakesARevision(t *testing.T) {
	ui, _ := newAgentUIServiceWithVideos(t)
	first, err := showVideo(ui, writeTestFile(t, "a.mp4", testVideo("one")), testVideoManifest, "tour")
	if err != nil {
		t.Fatal(err)
	}
	second, err := showVideo(ui, writeTestFile(t, "b.mp4", testVideo("two")), testVideoManifest, "tour")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Revision != 2 || second.Video.URL == first.Video.URL {
		t.Fatalf("second = id %s revision %d video %s; first = id %s video %s", second.ID, second.Revision, second.Video.URL, first.ID, first.Video.URL)
	}
	// The first cut can still be played.
	old, err := ui.Get(context.Background(), first.ID, 1)
	if err != nil || old.Video == nil || old.Video.URL != first.Video.URL {
		t.Errorf("revision 1 = %+v, err %v", old, err)
	}
}

func TestAgentUIShowRejectsAVideoItCannotShowWithoutStoringAnything(t *testing.T) {
	good := func(t *testing.T) string { return writeTestFile(t, "ok.mp4", testVideo("ok")) }
	tests := []struct {
		name     string
		path     func(t *testing.T) string
		manifest string
		want     string
	}{
		{"no file", func(*testing.T) string { return "" }, testVideoManifest, "needs the path of its video file"},
		{"a file that is not there", func(t *testing.T) string { return filepath.Join(t.TempDir(), "gone.mp4") }, testVideoManifest, "there is no file at"},
		{"a file that is not a video", func(t *testing.T) string { return writeTestFile(t, "fake.mp4", []byte("<html>not a video</html>")) }, testVideoManifest, "is not a video Legato can show"},
		{"a manifest that is not JSON", good, "version 1", "the manifest is not JSON"},
		{"a manifest of another version", good, `{"version":7,"durationMs":1,"scenes":[{"id":"a"}]}`, "the manifest is version 7"},
		{"a manifest with no scenes", good, `{"version":1,"durationMs":4000,"scenes":[]}`, "describes no video"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ui, dir := newAgentUIServiceWithVideos(t)
			_, err := showVideo(ui, tt.path(t), tt.manifest, "")
			if !errors.Is(err, service.ErrAgentUIInvalid) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one that says %q", err, tt.want)
			}
			if items, _ := ui.List(context.Background(), "task-1"); len(items) != 0 {
				t.Errorf("an item was stored: %+v", items)
			}
			if entries, _ := os.ReadDir(filepath.Join(dir, "task-1")); len(entries) != 0 {
				t.Errorf("files were left behind: %v", entries)
			}
		})
	}
}

func TestAgentUIVideoIsForThePanelAndOnlyAVideoItemTakesOne(t *testing.T) {
	ui, _ := newAgentUIServiceWithVideos(t)
	path := writeTestFile(t, "a.mp4", testVideo("x"))
	_, err := ui.Show(context.Background(), service.AgentUIShowInput{
		TaskID: "task-1", Placement: service.AgentUIInline, Format: service.AgentUIFormatVideo,
		FallbackText: "a video", Content: testVideoManifest, Video: path,
	})
	if !errors.Is(err, service.ErrAgentUIInvalid) || !strings.Contains(err.Error(), "cannot be shown here") {
		t.Errorf("a video as a card: err = %v", err)
	}
	_, err = ui.Show(context.Background(), service.AgentUIShowInput{
		TaskID: "task-1", Placement: service.AgentUIPanel, Format: service.AgentUIFormatMarkdown,
		Title: "Notes", Content: "# Notes", Video: path,
	})
	if !errors.Is(err, service.ErrAgentUIInvalid) || !strings.Contains(err.Error(), "only a video item takes a video file") {
		t.Errorf("a video file on a Markdown item: err = %v", err)
	}
}

func TestAgentUIOpenVideoGivesOutOnlyStoredVideos(t *testing.T) {
	ui, dir := newAgentUIServiceWithVideos(t)
	shown, err := showVideo(ui, writeTestFile(t, "a.mp4", testVideo("x")), testVideoManifest, "")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Base(shown.Video.URL)
	// Something that is not a video, under a name that says it is one.
	fake := strings.Repeat("a", 64) + ".mp4"
	if err := os.WriteFile(filepath.Join(dir, "task-1", fake), []byte("#!/bin/sh\necho hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, try := range map[string][2]string{
		"another task":              {"task-2", file},
		"a path that climbs out":    {"task-1", "../task-1/" + file},
		"a name that is not a hash": {"task-1", "notes.mp4"},
		"a file that is no video":   {"task-1", fake},
		"a task ID that is a path":  {"../agent-ui-videos/task-1", file},
	} {
		if _, err := ui.OpenVideo(try[0], try[1]); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s: err = %v, want not found", name, err)
		}
	}
}

func TestAgentUIGetLeavesOutAVideoWhoseCopyIsGone(t *testing.T) {
	ui, dir := newAgentUIServiceWithVideos(t)
	shown, err := showVideo(ui, writeTestFile(t, "a.mp4", testVideo("x")), testVideoManifest, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "task-1", filepath.Base(shown.Video.URL))); err != nil {
		t.Fatal(err)
	}
	item, err := ui.Get(context.Background(), shown.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if item.Video != nil || item.Content == "" {
		t.Errorf("item = video %+v; the manifest must stay and the video go", item.Video)
	}
}

func TestAgentUIShowKeepsATasksMostRecentVideos(t *testing.T) {
	ui, dir := newAgentUIServiceWithVideos(t)
	for i := 0; i < 15; i++ {
		if _, err := showVideo(ui, writeTestFile(t, "v.mp4", testVideo(strings.Repeat("x", i+1))), testVideoManifest, ""); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "task-1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 12 {
		t.Errorf("the task keeps %d videos, want 12", len(entries))
	}
}

// testBox builds one box of an MP4 file: its length, its name, its content.
func testBox(name string, content ...[]byte) []byte {
	body := bytes.Join(content, nil)
	out := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(out[:4], uint32(8+len(body)))
	copy(out[4:8], name)
	return append(out, body...)
}

// testVideoWithCover is an MP4 that carries picture as its cover.
func testVideoWithCover(picture []byte) []byte {
	ilst := testBox("ilst", testBox("covr", testBox("data", []byte{0, 0, 0, 14, 0, 0, 0, 0}, picture)))
	meta := testBox("meta", []byte{0, 0, 0, 0}, testBox("hdlr", make([]byte, 25)), ilst)
	return bytes.Join([][]byte{
		testBox("ftyp", []byte("isom\x00\x00\x02\x00isommp41")),
		testBox("moov", testBox("mvhd", make([]byte, 100)), testBox("udta", meta)),
		testBox("mdat", []byte("frames")),
	}, nil)
}

func TestAgentUIShowKeepsTheCoverPictureOfAVideoAsItsPoster(t *testing.T) {
	root := t.TempDir()
	ui := service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{
		VideoDir: filepath.Join(root, "agent-ui-videos"), ImageDir: filepath.Join(root, "agent-ui-images"),
	})
	picture := testImage("png", "the title card")
	shown, err := showVideo(ui, writeTestFile(t, "tour.mp4", testVideoWithCover(picture)), testVideoManifest, "tour")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(picture)
	want := service.AgentUIImageRoute + "task-1/" + hex.EncodeToString(sum[:]) + ".png"
	if shown.Video == nil || shown.Video.Poster != want {
		t.Fatalf("video = %+v, want the poster at %s", shown.Video, want)
	}
	// The poster is given out where images are, and is the picture itself.
	got, mediaType := readStoredImage(t, ui, shown.Video.Poster)
	if !bytes.Equal(got, picture) || mediaType != "image/png" {
		t.Errorf("the poster is %d bytes of %s", len(got), mediaType)
	}
	// It is not listed as one of the item's images: a video item has none.
	item, err := ui.Get(context.Background(), shown.ID, 0)
	if err != nil || item.Video == nil || item.Video.Poster != want || len(item.Images) != 0 {
		t.Fatalf("item = video %+v, images %+v, err %v", item.Video, item.Images, err)
	}

	// With its copy gone, the video is shown by its first frame.
	if err := os.Remove(filepath.Join(root, "agent-ui-images", "task-1", filepath.Base(want))); err != nil {
		t.Fatal(err)
	}
	item, err = ui.Get(context.Background(), shown.ID, 0)
	if err != nil || item.Video == nil || item.Video.Poster != "" {
		t.Errorf("after the poster's copy went: video %+v, err %v", item.Video, err)
	}
}

func TestAgentUIShowsAVideoWithNoPosterWhenThereIsNoneToKeep(t *testing.T) {
	root := t.TempDir()
	both := service.AgentUIOptions{VideoDir: filepath.Join(root, "videos"), ImageDir: filepath.Join(root, "images")}
	for name, try := range map[string]struct {
		opts  service.AgentUIOptions
		video []byte
	}{
		"a video that carries no cover":      {both, testVideo("plain")},
		"a cover that is not an image":       {both, testVideoWithCover([]byte("#!/bin/sh\necho not a picture"))},
		"a Legato with no folder for images": {service.AgentUIOptions{VideoDir: filepath.Join(root, "only-videos")}, testVideoWithCover(testImage("png", "x"))},
	} {
		ui := service.NewAgentUIService(newTestStoreForReport(t), nil, try.opts)
		shown, err := showVideo(ui, writeTestFile(t, "v.mp4", try.video), testVideoManifest, "")
		if err != nil {
			t.Errorf("%s: the video was turned down: %v", name, err)
			continue
		}
		if shown.Video == nil || shown.Video.Poster != "" {
			t.Errorf("%s: video = %+v, want a video with no poster", name, shown.Video)
		}
	}
}
