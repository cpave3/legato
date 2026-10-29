package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/overlay"
)

type fakeApprovals struct {
	pending   []service.Approval
	decisions []service.ApprovalDecision
	decided   []string
	dismissed []string
}

func (f *fakeApprovals) Pending(context.Context) ([]service.Approval, error) {
	return append([]service.Approval{}, f.pending...), nil
}

func (f *fakeApprovals) remove(id string) {
	for i, a := range f.pending {
		if a.ID == id {
			f.pending = append(f.pending[:i], f.pending[i+1:]...)
			return
		}
	}
}

func (f *fakeApprovals) Decide(_ context.Context, id string, d service.ApprovalDecision) (*service.Approval, error) {
	f.decided = append(f.decided, id)
	f.decisions = append(f.decisions, d)
	f.remove(id)
	return &service.Approval{}, nil
}

func (f *fakeApprovals) Dismiss(_ context.Context, id, _, _ string) (*service.Approval, error) {
	f.dismissed = append(f.dismissed, id)
	f.remove(id)
	return &service.Approval{}, nil
}

func pendingPermission(id string) service.Approval {
	return service.Approval{
		Approval: store.Approval{ID: id, Source: service.ApprovalSourceAgentPermission, TaskID: "t1", Title: "Run a command", Summary: "go test ./..."},
		Details:  json.RawMessage(`{"task_title":"Fix sort","agent_kind":"claude-code"}`),
		Options:  []service.ApprovalOption{{ID: "once", Label: "Allow once", Kind: service.ApprovalAllow}, {ID: "deny", Label: "Deny", Kind: service.ApprovalDeny}},
	}
}

// run feeds msg to the app and then every message its commands produce, as
// Bubble Tea would, until nothing more comes back.
func run(t *testing.T, app App, msg tea.Msg) App {
	t.Helper()
	queue := []tea.Msg{msg}
	for i := 0; len(queue) > 0 && i < 20; i++ {
		next := queue[0]
		queue = queue[1:]
		model, cmd := app.Update(next)
		app = model.(App)
		for _, m := range messagesOf(cmd) {
			switch m.(type) {
			case approvalsLoadedMsg, overlay.ApprovalDecideMsg, overlay.ApprovalOverlayClosedMsg:
				queue = append(queue, m)
			}
		}
	}
	return app
}

func newApprovalsApp(t *testing.T, approvals *fakeApprovals) App {
	t.Helper()
	app := newTestApp()
	app.SetApprovalService(approvals)
	model, _ := app.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	app = model.(App)
	return run(t, app, app.loadApprovals()())
}

func TestTheBarNamesPendingApprovals(t *testing.T) {
	app := newApprovalsApp(t, &fakeApprovals{pending: []service.Approval{pendingPermission("ap-1")}})

	if view := app.View(); !strings.Contains(view, "1 approval needs you") {
		t.Fatalf("view does not name the approval:\n%s", view)
	}
}

func TestBangOpensTheApprovalsAndYAllowsTheSelectedOne(t *testing.T) {
	approvals := &fakeApprovals{pending: []service.Approval{pendingPermission("ap-1")}}
	app := newApprovalsApp(t, approvals)

	app = run(t, app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
	if app.overlayType != overlayApproval || !strings.Contains(app.View(), "Fix sort · claude-code") {
		t.Fatalf("overlay not open:\n%s", app.View())
	}

	app = run(t, app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})

	if len(approvals.decided) != 1 || approvals.decided[0] != "ap-1" || approvals.decisions[0].Decision != service.ApprovalAllow || approvals.decisions[0].By != "tui" {
		t.Fatalf("decided %v %+v", approvals.decided, approvals.decisions)
	}
	if app.overlayType != overlayNone {
		t.Fatal("the overlay stayed open with nothing left to answer")
	}
	if strings.Contains(app.View(), "approval needs you") {
		t.Fatal("the bar still names an approval")
	}
}

func TestNDeniesAPermissionAndDismissesAQuestion(t *testing.T) {
	question := pendingPermission("ap-2")
	question.Source = service.ApprovalSourceAgentQuestion
	question.Options = nil
	approvals := &fakeApprovals{pending: []service.Approval{pendingPermission("ap-1"), question}}
	app := newApprovalsApp(t, approvals)
	app = run(t, app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})

	app = run(t, app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	app = run(t, app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})

	if len(approvals.decided) != 1 || approvals.decisions[0].Decision != service.ApprovalDeny {
		t.Fatalf("decisions = %+v", approvals.decisions)
	}
	if len(approvals.dismissed) != 1 || approvals.dismissed[0] != "ap-2" {
		t.Fatalf("dismissed = %v", approvals.dismissed)
	}
}
