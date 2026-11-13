package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/service"
)

type fakeReeveVoice struct{ status service.VoiceReadiness }

func (f *fakeReeveVoice) Status(context.Context) (service.VoiceReadiness, error) {
	return f.status, nil
}

func TestTheVoiceStatusIsServedWithTheWebToken(t *testing.T) {
	srv, _ := reeveServer(t)
	srv.SetReeveVoice(&fakeReeveVoice{status: service.VoiceReadiness{
		Instance: service.VoiceInstance{ID: "srv-1", Name: "tessitura", Role: "main"},
		Reason:   "The Reeve's voice is off on tessitura", Setting: "reeve.voice.enabled"}})

	if rec := callAs(srv, "wrong", http.MethodGet, "/api/reeve/voice", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without the token: %d", rec.Code)
	}
	rec := callAs(srv, "secret", http.MethodGet, "/api/reeve/voice", "")
	var got service.VoiceReadiness
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
	if got.Ready || got.Instance.Role != "main" || got.Setting != "reeve.voice.enabled" {
		t.Fatalf("got %+v", got)
	}
}

func TestTheVoiceStatusSaysWhenThisServerHasNoVoice(t *testing.T) {
	srv, _ := reeveServer(t)
	rec := callAs(srv, "secret", http.MethodGet, "/api/reeve/voice", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

// The desktop widget shows a line playing on the main's speakers from
// reeve_spoke: playing starts it, and the end it waits for is the one under
// the same line number.
func TestWhatBecameOfALineIsBroadcastWithItsNumber(t *testing.T) {
	bus := events.New()
	srv := NewWithSwarm(nil, nil, nil, "", nil, bus, "")
	srv.StartReeveSpokeEvents()

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

	bus.Publish(events.Event{Type: events.EventReeveSpoke, Payload: events.ReeveSpokePayload{TaskID: "reeve-1", Text: "The build passed.", State: "playing", Line: 7}})

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["type"] != "reeve_spoke" || got["status"] != "playing" || got["content"] != "The build passed." || got["task_id"] != "reeve-1" || got["line"] != float64(7) {
		t.Fatalf("broadcast %s", data)
	}
}
