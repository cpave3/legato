package cli_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
)

func TestIPCMessageTriggersEventBusRefresh(t *testing.T) {
	s := newTestStore(t)
	seedColumns(t, s)
	seedTask(t, s, "ipc-test", "Backlog")

	bus := events.New()
	ch := bus.Subscribe(events.EventCardUpdated)

	// Set up a socket in a temp dir that Broadcast will discover.
	sockDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", sockDir)

	sockPath := filepath.Join(sockDir, "legato", "legato-test.sock")
	srv, err := ipc.NewServer(sockPath, func(msg ipc.Message) {
		switch msg.Type {
		case "task_update", "task_note", "agent_state":
			bus.Publish(events.Event{
				Type: events.EventCardUpdated,
				At:   time.Now(),
			})
		}
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	// CLI update — writes to DB and broadcasts IPC.
	if err := cli.TaskUpdate(s, "ipc-test", "Done"); err != nil {
		t.Fatalf("TaskUpdate: %v", err)
	}

	select {
	case evt := <-ch:
		if evt.Type != events.EventCardUpdated {
			t.Errorf("event type = %v, want EventCardUpdated", evt.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for EventCardUpdated on bus")
	}
}
