package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/tui/overlay"
	"github.com/cpave3/legato/internal/tui/theme"
)

// verdictListener is a conductor waiting for a plan verdict on its socket.
func verdictListener(t *testing.T) (string, <-chan ipc.Message) {
	t.Helper()
	dir, err := os.MkdirTemp("", "lgv")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "c.sock")
	got := make(chan ipc.Message, 1)
	srv, err := ipc.NewServer(socket, func(m ipc.Message) { got <- m })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	return socket, got
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("nothing arrived")
	}
	var zero T
	return zero
}

// Approving or rejecting a plan in the TUI answers the conductor on its
// reply socket and tells the rest of Legato the plan is settled.
func TestPlanVerdictsReachTheConductorAndTheBus(t *testing.T) {
	for _, tc := range []struct {
		name       string
		send       func(App, string) App
		wantStatus string
		wantNotes  string
		wantPath   string
		wantSwarm  string
	}{
		{"approve", func(a App, socket string) App {
			m, _ := a.handlePlanApprove(overlay.PlanApproveMsg{ParentTaskID: "P-1", PlanPath: "/plans/p.md", ReplySocket: socket})
			return m.(App)
		}, "approved", "", "/plans/p.md", "plan_applied"},
		{"reject", func(a App, socket string) App {
			m, _ := a.handlePlanReject(overlay.PlanRejectMsg{ParentTaskID: "P-1", PlanPath: "/plans/p.md", ReplySocket: socket, Notes: "split it"})
			return m.(App)
		}, "rejected", "split it", "", "rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bus := events.New()
			changed := bus.Subscribe(events.EventSwarmChanged)
			socket, verdicts := verdictListener(t)
			app := NewApp(&fakeBoardService{}, nil, nil, nil, &fakeReportService{}, theme.NewIcons("unicode"), bus, "vi", nil, nil, "", &fakeSwarmService{}, nil)
			tc.send(app, socket)
			v := receive(t, verdicts)
			if v.Type != "plan_verdict" || v.TaskID != "P-1" || v.Status != tc.wantStatus || v.Notes != tc.wantNotes || v.PlanPath != tc.wantPath {
				t.Fatalf("verdict = %+v", v)
			}
			e := receive(t, changed)
			p, ok := e.Payload.(events.SwarmChangedPayload)
			if !ok || p.ParentTaskID != "P-1" || p.NewStatus != tc.wantSwarm {
				t.Fatalf("event = %+v", e)
			}
		})
	}
}
