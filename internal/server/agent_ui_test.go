package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func newAgentUIAPITestServer(t *testing.T) (*Server, *service.AgentUIService, *events.Bus) {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "agent-ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bus := events.New()
	ui := service.NewAgentUIService(db, bus, service.AgentUIOptions{ResourceDomains: []string{"cdn.example.com"}})
	srv := NewWithSwarm(nil, nil, nil, "", nil, bus, "")
	srv.SetAgentUIService(ui)
	return srv, ui, bus
}

func agentUIRequest(srv *Server, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestAgentUIAPIListsASessionsItemsAndReadsOneWithItsContent(t *testing.T) {
	srv, ui, _ := newAgentUIAPITestServer(t)
	ctx := context.Background()
	artifact, err := ui.Show(ctx, service.AgentUIShowInput{TaskID: "task1", Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML, Title: "Sync design", Key: "design", Content: "<h1>One</h1>"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ui.Show(ctx, service.AgentUIShowInput{TaskID: "task1", Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML, Title: "Sync design", Key: "design", Content: "<h1>Two</h1>"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ui.Show(ctx, service.AgentUIShowInput{TaskID: "task1", Placement: service.AgentUIInline, Format: service.AgentUIFormatHTML, Title: "Weather", Content: "<b>27 C</b>", FallbackText: "27 C"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ui.Show(ctx, service.AgentUIShowInput{TaskID: "other", Placement: service.AgentUIPanel, Format: service.AgentUIFormatMarkdown, Title: "Other", Content: "# Other"}); err != nil {
		t.Fatal(err)
	}

	list := agentUIRequest(srv, http.MethodGet, "/api/agents/task1/ui-items")
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	var listed struct {
		Items []service.AgentUIItem `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Items) != 2 || listed.Items[0].ID != artifact.ID || listed.Items[0].Revision != 2 || listed.Items[0].OldestRevision != 1 || listed.Items[1].Title != "Weather" {
		t.Fatalf("listed = %#v", listed.Items)
	}
	if strings.Contains(list.Body.String(), "<h1>") || strings.Contains(list.Body.String(), "27 C</b>") {
		t.Fatalf("the list carries item content: %s", list.Body.String())
	}

	latest := agentUIRequest(srv, http.MethodGet, "/api/ui-items/"+artifact.ID)
	if latest.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", latest.Code, latest.Body.String())
	}
	// Agent-written HTML must reach the browser as data, never as a page the
	// browser would run with Legato's own origin.
	if contentType := latest.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("content type = %q, want JSON", contentType)
	}
	if latest.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing nosniff header: %v", latest.Header())
	}
	var item service.AgentUIItem
	if err := json.Unmarshal(latest.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.Content != "<h1>Two</h1>" || item.Revision != 2 || len(item.ResourceDomains) != 1 || item.ResourceDomains[0] != "cdn.example.com" {
		t.Fatalf("latest item = %#v", item)
	}

	first := agentUIRequest(srv, http.MethodGet, "/api/ui-items/"+artifact.ID+"?revision=1")
	if err := json.Unmarshal(first.Body.Bytes(), &item); err != nil || first.Code != http.StatusOK || item.Content != "<h1>One</h1>" || item.Revision != 1 {
		t.Fatalf("revision 1 = %d %s", first.Code, first.Body.String())
	}

	empty := agentUIRequest(srv, http.MethodGet, "/api/agents/nobody/ui-items")
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != `{"items":[]}` {
		t.Fatalf("empty list = %d %s", empty.Code, empty.Body.String())
	}
	for path, want := range map[string]int{
		"/api/ui-items/ui-0000000000":                  http.StatusNotFound,
		"/api/ui-items/" + artifact.ID + "?revision=9": http.StatusNotFound,
		"/api/ui-items/" + artifact.ID + "?revision=x": http.StatusBadRequest,
	} {
		if got := agentUIRequest(srv, http.MethodGet, path); got.Code != want {
			t.Fatalf("GET %s = %d, want %d (%s)", path, got.Code, want, got.Body.String())
		}
	}
}

func TestAgentUIAPIDeletesAnItemAndTellsWebClients(t *testing.T) {
	srv, ui, _ := newAgentUIAPITestServer(t)
	messages := make(chan WSMessage, 4)
	srv.hub.add(&wsClient{ctx: context.Background(), sendFn: func(message WSMessage) { messages <- message }})
	srv.StartAgentUIEvents()
	ctx := context.Background()
	shown, err := ui.Show(ctx, service.AgentUIShowInput{TaskID: "task1", Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML, Title: "Design", Content: "<p>x</p>"})
	if err != nil {
		t.Fatal(err)
	}
	expect := func(kind string) {
		t.Helper()
		select {
		case message := <-messages:
			if message.Type != MsgAgentUIChanged || message.TaskID != "task1" || message.ItemID != shown.ID || message.Placement != "panel" || message.Revision != 1 || message.Kind != kind {
				t.Fatalf("message = %+v, want %s of %s", message, kind, shown.ID)
			}
		case <-time.After(time.Second):
			t.Fatalf("no %s message reached web clients", kind)
		}
	}
	expect("shown")

	deleted := agentUIRequest(srv, http.MethodDelete, "/api/ui-items/"+shown.ID)
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
	expect("deleted")
	if again := agentUIRequest(srv, http.MethodDelete, "/api/ui-items/"+shown.ID); again.Code != http.StatusNotFound {
		t.Fatalf("second delete = %d", again.Code)
	}
	if got := agentUIRequest(srv, http.MethodGet, "/api/ui-items/"+shown.ID); got.Code != http.StatusNotFound {
		t.Fatalf("get after delete = %d", got.Code)
	}
}

func TestAgentUIAPIIsUnavailableWithoutItsService(t *testing.T) {
	srv := New(nil, nil, nil, "")
	if got := agentUIRequest(srv, http.MethodGet, "/api/agents/task1/ui-items"); got.Code != http.StatusServiceUnavailable {
		t.Fatalf("list without the service = %d", got.Code)
	}
}

func TestAgentUIAPIRequiresTheAuthToken(t *testing.T) {
	srv, _, _ := newAgentUIAPITestServer(t)
	srv.SetAuthToken("secret")
	if got := agentUIRequest(srv, http.MethodGet, "/api/agents/task1/ui-items"); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list = %d", got.Code)
	}
}
