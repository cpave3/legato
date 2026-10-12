package cli_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// A file that reads as a PNG image: its kind is decided by its first bytes.
func writeMCPTestImage(t *testing.T, name, tail string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("\x89PNG\r\n\x1a\n"+tail), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// connectMCPWithImages is an agent session whose Legato has a folder to keep
// image files in.
func connectMCPWithImages(t *testing.T) (*mcp.ClientSession, *service.AgentUIService) {
	t.Helper()
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	imageDir := filepath.Join(t.TempDir(), "agent-ui-images")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID, ImageDir: imageDir})
	return session, service.NewAgentUIService(s, nil, service.AgentUIOptions{ImageDir: imageDir})
}

func shownItem(t *testing.T, ui *service.AgentUIService, result *mcp.CallToolResult) *service.AgentUIItem {
	t.Helper()
	if result.IsError {
		t.Fatalf("the call failed: %s", mcpText(result))
	}
	match := uiMarker.FindStringSubmatch(mcpText(result))
	if match == nil {
		t.Fatalf("result has no item marker: %q", mcpText(result))
	}
	item, err := ui.Get(context.Background(), match[1], 0)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestMCPShowArtifactTakesImageFilesForItsContent(t *testing.T) {
	session, ui := connectMCPWithImages(t)
	path := writeMCPTestImage(t, "board.png", "the board")

	item := shownItem(t, ui, callMCP(t, session, "show_artifact", map[string]any{
		"title": "The board", "content": `<img src="legato-image:board" alt="The board">`,
		"images": []any{map[string]any{"name": "board", "path": path}},
	}))
	if len(item.Images) != 1 || item.Images[0].Name != "board" {
		t.Fatalf("images = %#v", item.Images)
	}
	image, err := ui.OpenImage("task1", filepath.Base(item.Images[0].URL))
	if err != nil {
		t.Fatalf("the image was not kept: %v", err)
	}
	image.Close()

	notAnImage := filepath.Join(t.TempDir(), "notes.png")
	if err := os.WriteFile(notAnImage, []byte("just text"), 0o644); err != nil {
		t.Fatal(err)
	}
	refused := callMCP(t, session, "show_artifact", map[string]any{
		"title": "Notes", "content": `<img src="legato-image:notes">`,
		"images": []any{map[string]any{"name": "notes", "path": notAnImage}},
	})
	// The agent is told which file and why, so it can correct the call.
	if !refused.IsError || !strings.Contains(mcpText(refused), notAnImage) || !strings.Contains(mcpText(refused), "not an image") {
		t.Fatalf("a file that is not an image = %q (error %v)", mcpText(refused), refused.IsError)
	}
}

func TestMCPShowCardTakesImageFilesForItsHTML(t *testing.T) {
	session, ui := connectMCPWithImages(t)
	item := shownItem(t, ui, callMCP(t, session, "show_card", map[string]any{
		"html": `<img src="legato-image:chart">`, "fallback_text": "The chart of this week's builds",
		"images": []any{map[string]any{"name": "chart", "path": writeMCPTestImage(t, "chart.png", "chart")}},
	}))
	if item.Placement != service.AgentUIInline || len(item.Images) != 1 || item.Images[0].Name != "chart" {
		t.Fatalf("card = %#v", item)
	}
}

func TestMCPShowComponentsTakesImageComponents(t *testing.T) {
	session, ui := connectMCPWithImages(t)
	before, after := writeMCPTestImage(t, "before.png", "before"), writeMCPTestImage(t, "after.png", "after")

	item := shownItem(t, ui, callMCP(t, session, "show_components", map[string]any{
		"fallback_text": "The board before and after the restyle",
		"components": []any{
			map[string]any{"type": "image", "path": before, "alt": "The board before", "caption": "Before"},
			map[string]any{"type": "image_grid", "items": []any{
				map[string]any{"path": before, "alt": "Before"},
				map[string]any{"path": after, "alt": "After", "caption": "After the restyle"},
			}},
		},
	}))
	if len(item.Images) != 2 || item.Images[0].Name != before || item.Images[1].Name != after {
		t.Fatalf("images = %#v", item.Images)
	}
	var stored []service.UIComponent
	if err := json.Unmarshal([]byte(item.Content), &stored); err != nil {
		t.Fatal(err)
	}
	if stored[0].Caption != "Before" || stored[1].Items[1].Caption != "After the restyle" {
		t.Fatalf("stored components = %#v", stored)
	}
}

func TestMCPShowArtifactTakesComponentsInPlaceOfContent(t *testing.T) {
	session, ui := connectMCPWithImages(t)
	components := []any{
		map[string]any{"type": "heading", "text": "Screenshots"},
		map[string]any{"type": "image_grid", "items": []any{map[string]any{"path": writeMCPTestImage(t, "a.png", "a"), "alt": "The board"}}},
	}

	item := shownItem(t, ui, callMCP(t, session, "show_artifact", map[string]any{"title": "Screenshots", "components": components}))
	if item.Placement != service.AgentUIPanel || item.Format != service.AgentUIFormatComponents || len(item.Images) != 1 {
		t.Fatalf("artifact = %#v", item)
	}

	for name, arguments := range map[string]map[string]any{
		"content and components together":  {"title": "Both", "content": "<p>x</p>", "components": components},
		"components with a format of HTML": {"title": "Format", "format": "html", "components": components},
		"neither content nor components":   {"title": "Nothing"},
	} {
		refused := callMCP(t, session, "show_artifact", arguments)
		if !refused.IsError || !strings.Contains(mcpText(refused), "content") {
			t.Errorf("%s = %q (error %v)", name, mcpText(refused), refused.IsError)
		}
	}
}

// An agent that has an image file should show it, not describe it, and has
// to learn how from what reaches its model: the guidance and each tool's own
// description.
func TestMCPToolsTeachHowToShowImageFiles(t *testing.T) {
	for _, want := range []string{"image file", "screenshot", "image_grid", "legato-image:"} {
		if !strings.Contains(cli.MCPGuidance, want) {
			t.Errorf("the guidance does not mention %q", want)
		}
	}
	descriptions := mcpDescriptions(t, []string{"cdn.jsdelivr.net"})
	for _, tool := range []string{"show_artifact", "show_card"} {
		for _, want := range []string{"images", "legato-image:", "legato.image(", "full size", "legato-image-grid", "data-legato-zoom"} {
			if !strings.Contains(descriptions[tool], want) {
				t.Errorf("%s does not mention %q: %q", tool, want, descriptions[tool])
			}
		}
	}
	for _, want := range []string{"image:", "image_grid:", "path", "absolute"} {
		if !strings.Contains(descriptions["show_components"], want) {
			t.Errorf("show_components does not mention %q: %q", want, descriptions["show_components"])
		}
	}
	if d := descriptions["show_artifact"]; !strings.Contains(d, "components") || !strings.Contains(d, "show_components") {
		t.Errorf("show_artifact does not say it takes components: %q", d)
	}
}
