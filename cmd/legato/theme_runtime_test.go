package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

func TestServerThemeIPCRefreshesOtherRuntimeWithoutEcho(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	for _, serve := range []bool{false, true} {
		source, receiver := events.New(), events.New()
		received := make(chan ipc.Message, 4)
		listener, err := ipc.NewServer(filepath.Join(ipc.SocketDir(), "theme-test.sock"), func(msg ipc.Message) {
			received <- msg
			if serve {
				handleServeIPCMessage(msg, receiver, &recordingServeNotifier{})
			} else {
				handleIPCMessage(msg, receiver, nil)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		stopSource, stopReceiver := startServerThemeIPC(source), startServerThemeIPC(receiver)
		changes, unsubscribe := receiver.SubscribeInvalidations(events.EventServerThemeInvalidated)
		db := channelsTestDB(t)
		srv := server.New(nil, nil, nil, ":0")
		stopWeb := wireServerTheme(srv, db, receiver)
		saved, err := service.NewThemePreferenceService(db, source).Change(context.Background(), 0, "ocular-dark", "web")
		if err != nil {
			t.Fatal(err)
		}
		select {
		case msg := <-received:
			if msg.Type != "server_theme_changed" || msg.Content != "" || msg.TaskID != "" {
				t.Fatalf("IPC leaked appearance/actor: %+v", msg)
			}
		case <-time.After(time.Second):
			t.Fatal("no cross-process signal")
		}
		select {
		case <-changes:
		case <-time.After(time.Second):
			t.Fatal("other runtime did not invalidate")
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/meta", nil))
		var metadata struct {
			ThemePreference service.ThemePreference `json:"theme_preference"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &metadata); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || metadata.ThemePreference != saved {
			t.Fatalf("other runtime metadata: %d %s", rec.Code, rec.Body)
		}
		// Stop flushes pending source work; the received event cannot become a source.
		stopSource()
		stopReceiver()
		select {
		case msg := <-received:
			t.Fatalf("IPC echoed invalidation: %+v", msg)
		default:
		}
		stopWeb()
		unsubscribe()
		listener.Close()
	}
}
