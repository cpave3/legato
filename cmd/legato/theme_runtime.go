package main

import (
	"context"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

func wireServerTheme(srv *server.Server, db *store.Store, bus *events.Bus) func() {
	srv.SetThemePreferenceService(service.NewThemePreferenceService(db, bus))
	return srv.StartServerThemeEvents(bus)
}

// Local changes notify other processes without exporting appearance or actors.
// Received invalidations use a distinct event and never echo back into IPC.
func startServerThemeIPC(bus *events.Bus) func() {
	changes, unsubscribe := bus.SubscribeInvalidations(events.EventServerThemeChanged)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-changes:
				sendCtx, stop := context.WithTimeout(ctx, time.Second)
				ipc.BroadcastContext(sendCtx, ipc.Message{Type: "server_theme_changed"})
				stop()
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { cancel(); <-done; unsubscribe() }) }
}
