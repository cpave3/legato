package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const mcpTestManifest = `{"version":1,"video":"tour.mp4","durationMs":4000,"chapters":[],"scenes":[{"id":"intro","kind":"card","startMs":0,"durationMs":4000}]}`

// writeMCPTestVideo writes a manifest and the video it names into one
// folder, as libretto render does, and returns the manifest's path.
func writeMCPTestVideo(t *testing.T, tail string) string {
	t.Helper()
	dir := t.TempDir()
	// A file that reads as an MP4: its kind is decided by its first bytes.
	if err := os.WriteFile(filepath.Join(dir, "tour.mp4"), []byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isommp41"+tail), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "tour.manifest.json")
	if err := os.WriteFile(manifest, []byte(mcpTestManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func connectMCPWithVideos(t *testing.T) (*mcp.ClientSession, *service.AgentUIService) {
	t.Helper()
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	videoDir := filepath.Join(t.TempDir(), "agent-ui-videos")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID, VideoDir: videoDir})
	return session, service.NewAgentUIService(s, nil, service.AgentUIOptions{VideoDir: videoDir})
}

func TestMCPShowVideoStoresTheManifestAndTheVideoItNames(t *testing.T) {
	session, ui := connectMCPWithVideos(t)
	manifest := writeMCPTestVideo(t, "first cut")

	result := callMCP(t, session, "show_video", map[string]any{"manifest": manifest, "title": "Tour of the change", "key": "tour"})
	item := shownItem(t, ui, result)
	if item.Format != service.AgentUIFormatVideo || item.Placement != service.AgentUIPanel || item.Title != "Tour of the change" {
		t.Fatalf("item = %+v", item)
	}
	if item.Content != mcpTestManifest {
		t.Errorf("the manifest was not stored as written: %q", item.Content)
	}
	if item.Video == nil || !strings.HasPrefix(item.Video.URL, "/agent-ui-videos/task1/") || !strings.HasSuffix(item.Video.URL, ".mp4") {
		t.Fatalf("video = %+v", item.Video)
	}
	video, err := ui.OpenVideo("task1", filepath.Base(item.Video.URL))
	if err != nil {
		t.Fatalf("the video was not kept: %v", err)
	}
	video.Close()
	if !strings.Contains(mcpText(result), `the video "Tour of the change"`) {
		t.Errorf("result = %q", mcpText(result))
	}

	// A new render under the same key is a revision of the same item.
	again := shownItem(t, ui, callMCP(t, session, "show_video", map[string]any{"manifest": writeMCPTestVideo(t, "second cut"), "title": "Tour of the change", "key": "tour"}))
	if again.ID != item.ID || again.Revision != 2 || again.Video.URL == item.Video.URL {
		t.Errorf("second = id %s revision %d; first = id %s", again.ID, again.Revision, item.ID)
	}
}

func TestMCPShowVideoTakesAVideoFileNamedByItself(t *testing.T) {
	session, ui := connectMCPWithVideos(t)
	manifest := writeMCPTestVideo(t, "a")
	elsewhere := filepath.Join(t.TempDir(), "final.mp4")
	if err := os.Rename(filepath.Join(filepath.Dir(manifest), "tour.mp4"), elsewhere); err != nil {
		t.Fatal(err)
	}
	item := shownItem(t, ui, callMCP(t, session, "show_video", map[string]any{"manifest": manifest, "video": elsewhere, "title": "Tour"}))
	if item.Video == nil {
		t.Fatal("the video was not stored")
	}
}

func TestMCPShowVideoSaysWhatIsWrong(t *testing.T) {
	session, _ := connectMCPWithVideos(t)
	manifest := writeMCPTestVideo(t, "a")

	noVideoName := filepath.Join(t.TempDir(), "bare.manifest.json")
	if err := os.WriteFile(noVideoName, []byte(`{"version":1,"durationMs":4000,"scenes":[{"id":"a"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	gone := writeMCPTestVideo(t, "b")
	if err := os.Remove(filepath.Join(filepath.Dir(gone), "tour.mp4")); err != nil {
		t.Fatal(err)
	}
	for name, try := range map[string]struct {
		arguments map[string]any
		want      string
	}{
		"an empty manifest path":    {map[string]any{"manifest": " ", "title": "Tour"}, "show_video needs manifest"},
		"a manifest that is absent": {map[string]any{"manifest": filepath.Join(t.TempDir(), "x.json"), "title": "Tour"}, "there is no manifest file at"},
		"a manifest with no video":  {map[string]any{"manifest": noVideoName, "title": "Tour"}, "names no video file"},
		"a video that is absent":    {map[string]any{"manifest": gone, "title": "Tour"}, "there is no file at"},
		"an empty title":            {map[string]any{"manifest": manifest, "title": " "}, "an artifact needs a title"},
	} {
		result := callMCP(t, session, "show_video", try.arguments)
		if !result.IsError || !strings.Contains(mcpText(result), try.want) {
			t.Errorf("%s: error=%v text=%q, want an error that says %q", name, result.IsError, mcpText(result), try.want)
		}
	}
}
