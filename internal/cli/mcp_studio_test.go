package cli_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/libretto"
	"github.com/cpave3/legato/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// librettoIs stands in for asking the installed libretto what it can do.
type librettoIs struct{ err error }

func (l librettoIs) Probe(context.Context) (libretto.Info, error) {
	return libretto.Info{Version: "0.4.0", Manifest: 1, Studio: 5}, l.err
}

func connectMCPWithLibretto(t *testing.T, probe error) (*mcp.ClientSession, *service.AgentUIService) {
	t.Helper()
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID, Libretto: librettoIs{err: probe}})
	return session, service.NewAgentUIService(s, nil, service.AgentUIOptions{})
}

func writeMCPStoryboard(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "storyboard.json")
	if err := os.WriteFile(path, []byte(`{"scenes":[{"id":"intro","card":{"title":"Hello"},"notes":"Say hello."}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMCPShowStudioOpensAStudioOnAStoryboard(t *testing.T) {
	session, ui := connectMCPWithLibretto(t, nil)
	storyboard := writeMCPStoryboard(t)

	result := callMCP(t, session, "show_studio", map[string]any{"storyboard": storyboard, "title": "Restack demo", "key": "demo"})
	item := shownItem(t, ui, result)
	if item.Format != service.AgentUIFormatStudio || item.Placement != service.AgentUIPanel || item.Title != "Restack demo" {
		t.Fatalf("item = %+v", item)
	}
	if where, ok := service.AgentUIStoryboard(item); !ok || where != storyboard {
		t.Errorf("the studio is on %q, %v; want %q", where, ok, storyboard)
	}
	if !strings.Contains(mcpText(result), `the studio "Restack demo"`) {
		t.Errorf("result = %q", mcpText(result))
	}
	// Called again with a scene, it is the same studio, turned to that
	// scene: how an agent follows "let's look at the restack part".
	again := shownItem(t, ui, callMCP(t, session, "show_studio", map[string]any{"storyboard": storyboard, "title": "Restack demo", "key": "demo", "scene": "intro"}))
	if again.ID != item.ID || !strings.Contains(again.Content, `"scene":"intro"`) {
		t.Errorf("shown again it is item %s with content %s; want %s, on the scene intro", again.ID, again.Content, item.ID)
	}
}

func TestMCPShowStudioSaysWhenLibrettoCannotServeOne(t *testing.T) {
	tests := []struct {
		name  string
		probe error
		want  []string
	}{
		{"libretto is not installed", libretto.ErrNotInstalled, []string{"libretto is not installed", "install libretto"}},
		{"libretto is too old", &libretto.TooOldError{Version: "0.3.0", Studio: 0}, []string{"too old", "0.3.0", "update libretto"}},
		{"libretto cannot be asked", errors.New("permission denied"), []string{"permission denied"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			session, ui := connectMCPWithLibretto(t, tt.probe)
			result := callMCP(t, session, "show_studio", map[string]any{"storyboard": writeMCPStoryboard(t), "title": "Demo"})
			if !result.IsError {
				t.Fatalf("the call succeeded: %s", mcpText(result))
			}
			for _, want := range tt.want {
				if !strings.Contains(mcpText(result), want) {
					t.Errorf("the agent is told %q; want %q in it", mcpText(result), want)
				}
			}
			// The agent is told what to say to the person, since only they
			// can install anything.
			if !strings.Contains(mcpText(result), "Tell the person") {
				t.Errorf("the agent is not told to pass it on: %q", mcpText(result))
			}
			if items, _ := ui.List(context.Background(), "task1"); len(items) != 0 {
				t.Errorf("a studio that cannot open left %d items", len(items))
			}
		})
	}
}

func TestMCPShowStudioNeedsAStoryboardThatIsThere(t *testing.T) {
	session, _ := connectMCPWithLibretto(t, nil)
	for name, arguments := range map[string]map[string]any{
		"no storyboard":     {"title": "Demo", "storyboard": " "},
		"a missing file":    {"title": "Demo", "storyboard": filepath.Join(t.TempDir(), "gone.json")},
		"a relative path":   {"title": "Demo", "storyboard": "storyboard.json"},
		"a studio untitled": {"storyboard": writeMCPStoryboard(t), "title": ""},
	} {
		if result := callMCP(t, session, "show_studio", arguments); !result.IsError {
			t.Errorf("%s: the call succeeded: %s", name, mcpText(result))
		}
	}
}
