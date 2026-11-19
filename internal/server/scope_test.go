package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
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

type scopeTestServer struct {
	srv   *Server
	scope *service.ScopeService
	db    *store.Store
	sent  chan WSMessage
}

func newScopeTestServer(t *testing.T) *scopeTestServer {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	bus := events.New()
	scope := service.NewScopeService(service.ScopeServiceOptions{Store: db, Dir: filepath.Join(t.TempDir(), "scope"), Bus: bus})
	srv := NewWithSwarm(nil, nil, nil, "", nil, bus, "")
	srv.SetAuthToken("secret")
	srv.SetScopeService(scope)
	sent := make(chan WSMessage, 16)
	srv.hub.add(&wsClient{ctx: context.Background(), sendFn: func(message WSMessage) { sent <- message }})
	srv.StartScopeEvents()
	return &scopeTestServer{srv: srv, scope: scope, db: db, sent: sent}
}

func (s *scopeTestServer) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case []byte:
		reader = bytes.NewReader(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	s.srv.Handler().ServeHTTP(rec, req)
	return rec
}

func (s *scopeTestServer) expectChanged(t *testing.T, origin string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case message := <-s.sent:
			if message.Type == MsgScopeChanged && message.Origin == origin {
				return
			}
		case <-deadline:
			t.Fatalf("no scope_changed for %s", origin)
		}
	}
}

func scopeTestPNG(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func scopeTestPin(comment, screenshot string) map[string]any {
	pin := map[string]any{
		"comment": comment,
		"page":    map[string]any{"url": "http://localhost:5173/settings", "title": "App", "viewport": map[string]any{"width": 1280, "height": 800}, "scroll": map[string]any{"x": 0, "y": 120}, "dpr": 2},
		"target":  map[string]any{"selectors": []string{"#save"}, "text": "Save", "box": map[string]any{"x": 1, "y": 2, "width": 3, "height": 4}},
		"styles":  map[string]any{"computed": map[string]string{"color": "red"}},
		"source":  map[string]any{"name": "SaveButton", "file": "src/Save.tsx", "line": 9, "confidence": "exact"},
	}
	if screenshot != "" {
		pin["screenshot"] = screenshot
	}
	return pin
}

type scopeBatchAnswer struct {
	BatchID    string   `json:"batch_id"`
	PinIDs     []string `json:"pin_ids"`
	Subscribed bool     `json:"subscribed"`
	Delivered  bool     `json:"delivered"`
}

func TestScopeAPISendsABatchAndListsItsPins(t *testing.T) {
	s := newScopeTestServer(t)
	rec := s.do(t, http.MethodPost, "/api/scope/batches", map[string]any{
		"origin": "http://localhost:5173",
		"pins":   []any{scopeTestPin("Too tight", scopeTestPNG(t)), scopeTestPin("Wrong colour", "")},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("send = %d %s", rec.Code, rec.Body.String())
	}
	var answer scopeBatchAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.BatchID == "" || len(answer.PinIDs) != 2 || answer.Subscribed || answer.Delivered {
		t.Fatalf("answer = %+v", answer)
	}
	s.expectChanged(t, "http://localhost:5173")

	list := s.do(t, http.MethodGet, "/api/scope/pins?origin="+"http%3A%2F%2Flocalhost%3A5173", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list = %d %s", list.Code, list.Body.String())
	}
	var listed struct {
		Pins []map[string]any `json:"pins"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Pins) != 2 || listed.Pins[0]["id"] != answer.PinIDs[0] || listed.Pins[0]["state"] != "sent" || listed.Pins[0]["has_screenshot"] != true || listed.Pins[1]["has_screenshot"] != false || listed.Pins[0]["path"] != "/settings" {
		t.Fatalf("listed = %+v", listed.Pins)
	}

	shot := s.do(t, http.MethodGet, "/api/scope/pins/"+answer.PinIDs[0]+"/screenshot", nil)
	if shot.Code != http.StatusOK || shot.Header().Get("Content-Type") != "image/png" || shot.Body.Len() == 0 {
		t.Fatalf("screenshot = %d %q", shot.Code, shot.Header().Get("Content-Type"))
	}
	if none := s.do(t, http.MethodGet, "/api/scope/pins/"+answer.PinIDs[1]+"/screenshot", nil); none.Code != http.StatusNotFound {
		t.Fatalf("a pin with no screenshot = %d", none.Code)
	}
	if empty := s.do(t, http.MethodGet, "/api/scope/pins?origin=http://other:1", nil); empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"pins":[]`) {
		t.Fatalf("an origin with no pins = %d %s", empty.Code, empty.Body.String())
	}
}

func TestScopeAPIReopensAndWithdrawsPins(t *testing.T) {
	s := newScopeTestServer(t)
	ctx := context.Background()
	result, err := s.scope.SendBatch(ctx, "http://localhost:5173", []service.ScopePinInput{
		{Comment: "a", Page: service.ScopePage{URL: "http://localhost:5173/"}},
		{Comment: "b", Page: service.ScopePage{URL: "http://localhost:5173/"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, second := result.PinIDs[0], result.PinIDs[1]

	if rec := s.do(t, http.MethodPost, "/api/scope/pins/"+first+"/reopen", map[string]string{"comment": "again"}); rec.Code != http.StatusConflict {
		t.Fatalf("reopening a sent pin = %d %s", rec.Code, rec.Body.String())
	}
	if rec := s.do(t, http.MethodPost, "/api/scope/pins/"+second+"/withdraw", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("withdraw = %d %s", rec.Code, rec.Body.String())
	}
	if rec := s.do(t, http.MethodPost, "/api/scope/pins/"+second+"/withdraw", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("withdrawing again = %d", rec.Code)
	}

	if err := s.db.MarkScopePinsDelivered(ctx, []string{first}, "2026-11-19T10:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if rec := s.do(t, http.MethodPost, "/api/scope/pins/"+first+"/withdraw", nil); rec.Code != http.StatusConflict {
		t.Fatalf("withdrawing a delivered pin = %d %s", rec.Code, rec.Body.String())
	}
	if _, err := s.scope.Resolve(ctx, first, "fixed", "Done", "task1"); err != nil {
		t.Fatal(err)
	}
	s.expectChanged(t, "http://localhost:5173")

	rec := s.do(t, http.MethodPost, "/api/scope/pins/"+first+"/reopen", map[string]string{"comment": "Still wrong"})
	if rec.Code != http.StatusOK {
		t.Fatalf("reopen = %d %s", rec.Code, rec.Body.String())
	}
	var pin service.ScopePin
	if err := json.Unmarshal(rec.Body.Bytes(), &pin); err != nil || pin.State != "sent" || pin.Comment != "Still wrong" {
		t.Fatalf("reopened = %+v, %v", pin, err)
	}
	if rec := s.do(t, http.MethodPost, "/api/scope/pins/"+first+"/reopen", map[string]string{"comment": " "}); rec.Code != http.StatusBadRequest {
		t.Fatalf("reopen with no comment = %d", rec.Code)
	}
}

func TestScopeAPIRefusesWhatItCannotKeep(t *testing.T) {
	s := newScopeTestServer(t)
	send := func(body any) *httptest.ResponseRecorder {
		return s.do(t, http.MethodPost, "/api/scope/batches", body)
	}
	if rec := send(map[string]any{"origin": "localhost:5173", "pins": []any{scopeTestPin("x", "")}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("an invalid origin = %d %s", rec.Code, rec.Body.String())
	}
	if rec := s.do(t, http.MethodGet, "/api/scope/pins?origin=nope", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("listing an invalid origin = %d", rec.Code)
	}
	many := make([]any, service.ScopeMaxPinsPerBatch+1)
	for i := range many {
		many[i] = scopeTestPin("x", "")
	}
	if rec := send(map[string]any{"origin": "http://localhost:5173", "pins": many}); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too many pins = %d %s", rec.Code, rec.Body.String())
	}
	big := "data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, service.ScopeMaxScreenshotBytes+1))
	if rec := send(map[string]any{"origin": "http://localhost:5173", "pins": []any{scopeTestPin("x", big)}}); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a large screenshot = %d %s", rec.Code, rec.Body.String())
	}
	if rec := send(map[string]any{"origin": "http://localhost:5173", "pins": []any{scopeTestPin("x", "data:text/plain,hello")}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("a screenshot that is not base64 image data = %d %s", rec.Code, rec.Body.String())
	}
	if rec := send([]byte(`{"origin": `)); rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON = %d", rec.Code)
	}
	huge := bytes.Repeat([]byte(" "), scopeMaxBodyBytes+1)
	if rec := send(huge); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("a body past the limit = %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/scope/pins?origin=http://localhost:5173", nil)
	rec := httptest.NewRecorder()
	s.srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d", rec.Code)
	}
	if rec := s.do(t, http.MethodGet, "/api/scope/pins/sp-missing/screenshot", nil); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown pin's screenshot = %d", rec.Code)
	}
}

func TestScopeAPIIsUnavailableWithoutItsService(t *testing.T) {
	srv := New(nil, nil, nil, "")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/scope/pins?origin=http://localhost:5173", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("without a scope service = %d", rec.Code)
	}
}
