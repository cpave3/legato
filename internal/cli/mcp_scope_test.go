package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/mcpserver"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type scopeMessages struct{ sent []string }

func (s *scopeMessages) QueueMessage(_ context.Context, taskID, message string) error {
	s.sent = append(s.sent, taskID+": "+message)
	return nil
}

func newMCPScope(t *testing.T, s *store.Store) (*service.ScopeService, *scopeMessages) {
	t.Helper()
	messages := &scopeMessages{}
	return service.NewScopeService(service.ScopeServiceOptions{Store: s, Dir: filepath.Join(t.TempDir(), "scope"), Messenger: messages}), messages
}

func scopeTestScreenshot(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3, 3))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sendScopePin(t *testing.T, scope *service.ScopeService, origin, comment string, screenshot []byte) string {
	t.Helper()
	result, err := scope.SendBatch(context.Background(), origin, []service.ScopePinInput{{
		Comment: comment, Page: service.ScopePage{URL: origin + "/settings", Title: "Settings"},
		Target:     service.ScopeTarget{Selectors: []string{"#save"}, Box: service.ScopeBox{Width: 10, Height: 5}},
		Source:     &service.ScopeSource{File: "src/Save.tsx", Line: 12, Confidence: "exact"},
		Screenshot: screenshot,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return result.PinIDs[0]
}

func TestMCPPagePinToolsAreRefusedOutsideARunningSession(t *testing.T) {
	s := newTestStore(t)
	seedTask(t, s, "idle-task", "Doing")
	scope, _ := newMCPScope(t, s)
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "idle-task", Scope: scope})
	for tool, arguments := range map[string]map[string]any{
		"page_pin_subscribe":   {"origin": "http://localhost:5173"},
		"page_pin_unsubscribe": {"origin": "http://localhost:5173"},
		"page_pin_list":        {},
		"page_pin_get":         {"pin_id": "sp-1"},
		"page_pin_resolve":     {"pin_id": "sp-1", "outcome": "fixed", "note": "x"},
	} {
		if result := callMCP(t, session, tool, arguments); !result.IsError {
			t.Errorf("%s was accepted outside a running session: %s", tool, mcpText(result))
		}
	}
	if subs, _ := s.ListScopeSubscriptions(context.Background()); len(subs) != 0 {
		t.Fatalf("a refused call subscribed: %+v", subs)
	}
}

func TestMCPPagePinSubscribeAndListAreScopedToTheCallingSlot(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	other := seedMCPAgent(t, s, "task2")
	scope, _ := newMCPScope(t, s)
	waiting := sendScopePin(t, scope, "http://localhost:5173", "Button overlaps", nil)
	elsewhere := sendScopePin(t, scope, "http://localhost:3000", "Not mine", nil)

	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID, Scope: scope})
	result := callMCP(t, session, "page_pin_subscribe", map[string]any{"origin": "http://localhost:5173/"})
	if result.IsError || !strings.Contains(mcpText(result), waiting) || !strings.Contains(mcpText(result), "page_pin_get") {
		t.Fatalf("subscribe = %s", mcpText(result))
	}
	if origins, _ := scope.SubscriptionsOf(context.Background(), "task1", ""); len(origins) != 1 || origins[0] != "http://localhost:5173" {
		t.Fatalf("task1 subscribes to %v", origins)
	}

	listed := mcpText(callMCP(t, session, "page_pin_list", map[string]any{}))
	if !strings.Contains(listed, waiting) || strings.Contains(listed, elsewhere) {
		t.Fatalf("list = %s", listed)
	}
	named := mcpText(callMCP(t, session, "page_pin_list", map[string]any{"origin": "http://localhost:3000"}))
	if !strings.Contains(named, elsewhere) {
		t.Fatalf("list of a named origin = %s", named)
	}

	// Another task's agent lists its own origins: none.
	second := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task2", ExecutionID: other, Scope: scope})
	if text := mcpText(callMCP(t, second, "page_pin_list", map[string]any{})); strings.Contains(text, waiting) {
		t.Fatalf("task2 sees task1's pins: %s", text)
	}
	if result := callMCP(t, second, "page_pin_unsubscribe", map[string]any{"origin": "http://localhost:5173"}); !result.IsError {
		t.Fatalf("task2 unsubscribed task1's origin: %s", mcpText(result))
	}
	if result := callMCP(t, session, "page_pin_unsubscribe", map[string]any{"origin": "http://localhost:5173"}); result.IsError {
		t.Fatalf("unsubscribe = %s", mcpText(result))
	}
	if result := callMCP(t, session, "page_pin_subscribe", map[string]any{"origin": "localhost"}); !result.IsError {
		t.Fatalf("an invalid origin was accepted: %s", mcpText(result))
	}
}

func TestMCPPagePinGetReturnsTheContextAndTheScreenshot(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	scope, _ := newMCPScope(t, s)
	shot := scopeTestScreenshot(t)
	pinID := sendScopePin(t, scope, "http://localhost:5173", "Too tight", shot)

	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID, Scope: scope})
	result := callMCP(t, session, "page_pin_get", map[string]any{"pin_id": pinID})
	if result.IsError {
		t.Fatalf("get = %s", mcpText(result))
	}
	text := mcpText(result)
	for _, want := range []string{pinID, "Too tight", "#save", "src/Save.tsx:12"} {
		if !strings.Contains(text, want) {
			t.Errorf("get lacks %q: %s", want, text)
		}
	}
	var images []*mcp.ImageContent
	for _, content := range result.Content {
		if img, ok := content.(*mcp.ImageContent); ok {
			images = append(images, img)
		}
	}
	if len(images) != 1 || images[0].MIMEType != "image/png" || !bytes.Equal(images[0].Data, shot) {
		t.Fatalf("images = %+v", images)
	}

	if unknown := callMCP(t, session, "page_pin_get", map[string]any{"pin_id": "sp-nope"}); !unknown.IsError || !strings.Contains(mcpText(unknown), "sp-nope") {
		t.Fatalf("an unknown pin = %s", mcpText(unknown))
	}
}

func TestMCPPagePinResolveRecordsTheOutcomeAndNote(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	scope, _ := newMCPScope(t, s)
	pinID := sendScopePin(t, scope, "http://localhost:5173", "Too tight", nil)
	session := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID, Scope: scope})

	result := callMCP(t, session, "page_pin_resolve", map[string]any{"pin_id": pinID, "outcome": "wont_fix", "note": "The design asks for it"})
	if result.IsError {
		t.Fatalf("resolve = %s", mcpText(result))
	}
	pins, _ := scope.ListOrigin(context.Background(), "http://localhost:5173")
	if pins[0].State != "wont_fix" || pins[0].Note != "The design asks for it" {
		t.Fatalf("pin = %+v", pins[0])
	}
	pin, _ := s.GetScopePin(context.Background(), pinID)
	if pin.ResolvedByTask != "task1" {
		t.Fatalf("resolved by %q", pin.ResolvedByTask)
	}
	if again := callMCP(t, session, "page_pin_resolve", map[string]any{"pin_id": pinID, "outcome": "fixed", "note": "x"}); !again.IsError {
		t.Fatalf("resolving twice = %s", mcpText(again))
	}
	if unknown := callMCP(t, session, "page_pin_resolve", map[string]any{"pin_id": "sp-nope", "outcome": "fixed", "note": "x"}); !unknown.IsError {
		t.Fatalf("an unknown pin = %s", mcpText(unknown))
	}
	if bad := callMCP(t, session, "page_pin_resolve", map[string]any{"pin_id": pinID, "outcome": "maybe", "note": "x"}); !bad.IsError {
		t.Fatalf("an unknown outcome = %s", mcpText(bad))
	}
}

func TestMCPPagePinToolsAreNotOfferedWithoutScope(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	listed, err := connectMCP(t, cli.MCPOptions{Store: s, TaskID: "task1", ExecutionID: executionID}).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if strings.HasPrefix(tool.Name, "page_pin_") {
			t.Fatalf("%s offered without Scope", tool.Name)
		}
	}
}

// The broker relays a tool's images, so page_pin_get shows the screenshot
// to a sandboxed agent too.
func TestMCPBrokerRelaysAToolsImages(t *testing.T) {
	s := newTestStore(t)
	executionID := seedMCPAgent(t, s, "task1")
	scope, _ := newMCPScope(t, s)
	shot := scopeTestScreenshot(t)
	pinID := sendScopePin(t, scope, "http://localhost:5173", "Too tight", shot)
	ops := cli.MCPBrokerOps(func(cli.BrokerCaller) cli.MCPOptions { return cli.MCPOptions{Store: s, Scope: scope} })
	raw, _ := json.Marshal(map[string]any{"name": "page_pin_get", "arguments": map[string]any{"pin_id": pinID}})
	out, err := ops["mcp.call"](context.Background(), cli.BrokerCaller{TaskID: "task1", ExecutionID: executionID}, raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(out)
	var decoded struct {
		Images []mcpserver.Image `json:"images"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil || len(decoded.Images) != 1 || !bytes.Equal(decoded.Images[0].Data, shot) {
		t.Fatalf("relayed = %s, %v", encoded, err)
	}
}

func TestMCPGuidanceTellsAgentsToSubscribeTheirDevServer(t *testing.T) {
	for _, want := range []string{"page_pin_subscribe", "page_pin_get", "page_pin_resolve", "page_pin_list", "dev server", "[legato scope]"} {
		if !strings.Contains(cli.MCPGuidance, want) {
			t.Errorf("guidance does not mention %q", want)
		}
	}
}
