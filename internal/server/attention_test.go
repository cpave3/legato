package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"nhooyr.io/websocket"
)

type testAttentionProducer struct {
	items []service.AttentionInput
}

func (p *testAttentionProducer) Producer() string { return "test" }
func (p *testAttentionProducer) Snapshot(context.Context) ([]service.AttentionInput, error) {
	return p.items, nil
}
func (p *testAttentionProducer) Execute(_ context.Context, request service.AttentionActionRequest) error {
	if request.ActionID == "resolve" {
		p.items = nil
	}
	return nil
}

func TestAttentionAPIExecutesSourceActionAndRefreshesQueue(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "attention-api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	attention := service.NewAttentionService(db, nil)
	producer := &testAttentionProducer{items: []service.AttentionInput{{
		SourceKey: "request-1", SourceRevision: "1", Title: "Decision needed", Reason: "Choose a path",
		Category: "approval", Urgency: "high",
		Navigation: service.AttentionNavigation{Kind: "task", Target: "task-1"},
		Actions: []service.AttentionAction{{
			ID: "resolve", Label: "Resolve", Kind: "execute", RequiresConfirm: true,
		}},
	}}}
	if err := attention.RegisterProducer(producer); err != nil {
		t.Fatal(err)
	}
	if err := attention.RegisterHandler(producer.Producer(), producer); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(context.Background(), producer.Producer()); err != nil {
		t.Fatal(err)
	}

	srv := New(nil, nil, nil, "")
	srv.SetAttentionService(attention)

	list := httptest.NewRecorder()
	srv.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/attention", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", list.Code, list.Body.String())
	}
	var response struct {
		Items []service.AttentionItem `json:"items"`
		Total int                     `json:"total"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 || len(response.Items) != 1 {
		t.Fatalf("response = %+v", response)
	}

	body := strings.NewReader(`{"expected_revision":"1","confirmed":true}`)
	action := httptest.NewRecorder()
	path := "/api/attention/" + response.Items[0].ID + "/actions/resolve"
	srv.Handler().ServeHTTP(action, httptest.NewRequest(http.MethodPost, path, body))
	if action.Code != http.StatusNoContent {
		t.Fatalf("action status = %d, body = %s", action.Code, action.Body.String())
	}

	list = httptest.NewRecorder()
	srv.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/attention", nil))
	if err := json.Unmarshal(list.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 0 || len(response.Items) != 0 {
		t.Fatalf("resolved item remains in queue: %+v", response)
	}
}

func TestAttentionAPIDismissesItemAtCurrentRevision(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "attention-dismiss.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	attention := service.NewAttentionService(db, nil)
	producer := &testAttentionProducer{items: []service.AttentionInput{{
		SourceKey: "request-2", SourceRevision: "3", Title: "Decision needed", Reason: "Choose a path",
		Category: "approval", Urgency: "normal",
		Navigation: service.AttentionNavigation{Kind: "task", Target: "task-2"},
		Actions:    []service.AttentionAction{{ID: "open", Label: "Open", Kind: "navigate"}},
	}}}
	if err := attention.RegisterProducer(producer); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(context.Background(), producer.Producer()); err != nil {
		t.Fatal(err)
	}

	srv := New(nil, nil, nil, "")
	srv.SetAttentionService(attention)

	list := httptest.NewRecorder()
	srv.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/attention", nil))
	var response struct {
		Items []service.AttentionItem `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("items = %+v", response.Items)
	}

	dismiss := httptest.NewRecorder()
	body := strings.NewReader(`{"expected_revision":"3"}`)
	srv.Handler().ServeHTTP(dismiss, httptest.NewRequest(http.MethodPost, "/api/attention/"+response.Items[0].ID+"/dismiss", body))
	if dismiss.Code != http.StatusNoContent {
		t.Fatalf("dismiss status = %d, body = %s", dismiss.Code, dismiss.Body.String())
	}

	list = httptest.NewRecorder()
	srv.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/attention", nil))
	if err := json.Unmarshal(list.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 0 {
		t.Fatalf("dismissed item remains active: %+v", response.Items)
	}
}

func TestAttentionAPIRejectsStaleActionWithConflict(t *testing.T) {
	db, err := store.New(filepath.Join(t.TempDir(), "attention-stale.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	attention := service.NewAttentionService(db, nil)
	producer := &testAttentionProducer{items: []service.AttentionInput{{
		SourceKey: "request-3", SourceRevision: "2", Title: "Decision needed", Reason: "Choose a path",
		Category: "approval", Urgency: "high",
		Navigation: service.AttentionNavigation{Kind: "task", Target: "task-3"},
		Actions: []service.AttentionAction{{
			ID: "resolve", Label: "Resolve", Kind: "execute", RequiresConfirm: true,
		}},
	}}}
	if err := attention.RegisterProducer(producer); err != nil {
		t.Fatal(err)
	}
	if err := attention.RegisterHandler(producer.Producer(), producer); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(context.Background(), producer.Producer()); err != nil {
		t.Fatal(err)
	}

	srv := New(nil, nil, nil, "")
	srv.SetAttentionService(attention)

	list := httptest.NewRecorder()
	srv.Handler().ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/attention", nil))
	var response struct {
		Items []service.AttentionItem `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}

	body := strings.NewReader(`{"expected_revision":"1","confirmed":true}`)
	action := httptest.NewRecorder()
	srv.Handler().ServeHTTP(action, httptest.NewRequest(http.MethodPost, "/api/attention/"+response.Items[0].ID+"/actions/resolve", body))
	if action.Code != http.StatusConflict {
		t.Fatalf("stale action status = %d, body = %s", action.Code, action.Body.String())
	}
}

func TestAttentionChangedEventBroadcastsToWebSocket(t *testing.T) {
	bus := events.New()
	srv := NewWithSwarm(nil, nil, nil, "", nil, bus, "")
	srv.StartAttentionEvents()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() { _ = srv.Serve(ln) }()
	defer srv.Stop(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://%s/ws", ln.Addr()), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if _, _, err := conn.Read(ctx); err != nil { // initial agent_list
		t.Fatal(err)
	}

	bus.Publish(events.Event{Type: events.EventAttentionChanged, Payload: events.AttentionChangedPayload{
		ItemID: "att-1", Producer: "plans", SourceKey: "pl-1", Kind: "upserted",
	}})

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != MsgAttentionChanged || msg.ItemID != "att-1" || msg.Producer != "plans" || msg.SourceKey != "pl-1" || msg.Kind != "upserted" {
		t.Fatalf("unexpected message: %+v", msg)
	}
}
