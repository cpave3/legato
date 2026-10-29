package tui

import (
	"context"
	"encoding/json"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/overlay"
)

// ApprovalService is what the TUI asks of the approval service.
type ApprovalService interface {
	Pending(ctx context.Context) ([]service.Approval, error)
	Decide(ctx context.Context, id string, decision service.ApprovalDecision) (*service.Approval, error)
	Dismiss(ctx context.Context, id, note, by string) (*service.Approval, error)
}

// approvalsLoadedMsg carries the pending approvals.
type approvalsLoadedMsg struct {
	approvals []service.Approval
	note      string
}

type approvalUpdateMsg struct{}

// SetApprovalService lets the TUI count pending approvals on every view and
// answer them with ! (an overlay of y, n, j/k).
func (a *App) SetApprovalService(svc ApprovalService) {
	a.approvalSvc = svc
}

func (a App) loadApprovals() tea.Cmd {
	svc := a.approvalSvc
	return func() tea.Msg {
		if svc == nil {
			return nil
		}
		pending, err := svc.Pending(context.Background())
		if err != nil {
			return approvalsLoadedMsg{note: err.Error()}
		}
		return approvalsLoadedMsg{approvals: pending}
	}
}

func (a App) listenApprovalUpdates() tea.Cmd {
	ch := a.approvalSub
	return func() tea.Msg {
		if _, ok := <-ch; !ok {
			return nil
		}
		return approvalUpdateMsg{}
	}
}

// updateApprovals handles the messages of approvals. ok is false for a
// message that is not about them.
func (a App) updateApprovals(msg tea.Msg) (App, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case approvalUpdateMsg:
		return a, tea.Batch(a.loadApprovals(), a.listenApprovalUpdates()), true
	case approvalsLoadedMsg:
		if msg.note == "" {
			a.approvals = msg.approvals
		}
		a.statusBar = a.statusBar.SetApprovals(len(a.approvals))
		if a.overlayType == overlayApproval {
			if len(a.approvals) == 0 {
				a.overlayType, a.activeOverlay = overlayNone, nil
				return a, nil, true
			}
			if current, ok := a.activeOverlay.(overlay.ApprovalOverlay); ok {
				current = current.SetItems(approvalItems(a.approvals))
				if msg.note != "" {
					current = current.SetNote(msg.note)
				}
				a.activeOverlay = current
			}
		}
		return a, nil, true
	case overlay.ApprovalOverlayClosedMsg:
		a.overlayType, a.activeOverlay = overlayNone, nil
		return a, nil, true
	case overlay.ApprovalDecideMsg:
		return a, a.decideApproval(msg), true
	}
	return a, nil, false
}

func (a App) openApprovalOverlay() (tea.Model, tea.Cmd) {
	if a.approvalSvc == nil || len(a.approvals) == 0 {
		return a, a.loadApprovals()
	}
	model, _ := overlay.NewApprovalOverlay(approvalItems(a.approvals)).Update(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	a.activeOverlay, a.overlayType = model, overlayApproval
	return a, nil
}

// decideApproval allows with the first way to allow, denies a permission, or
// dismisses a question, then reads the list again.
func (a App) decideApproval(msg overlay.ApprovalDecideMsg) tea.Cmd {
	svc := a.approvalSvc
	var target *service.Approval
	for i := range a.approvals {
		if a.approvals[i].ID == msg.ID {
			target = &a.approvals[i]
		}
	}
	if svc == nil || target == nil {
		return a.loadApprovals()
	}
	question := target.Source == service.ApprovalSourceAgentQuestion
	return func() tea.Msg {
		ctx := context.Background()
		var err error
		switch {
		case msg.Allow:
			_, err = svc.Decide(ctx, msg.ID, service.ApprovalDecision{Decision: service.ApprovalAllow, By: "tui"})
		case question:
			_, err = svc.Dismiss(ctx, msg.ID, "", "tui")
		default:
			_, err = svc.Decide(ctx, msg.ID, service.ApprovalDecision{Decision: service.ApprovalDeny, By: "tui"})
		}
		pending, listErr := svc.Pending(ctx)
		loaded := approvalsLoadedMsg{approvals: pending}
		if listErr != nil {
			loaded = approvalsLoadedMsg{note: listErr.Error()}
		}
		if err != nil {
			loaded.note = err.Error()
		}
		return loaded
	}
}

func approvalItems(approvals []service.Approval) []overlay.ApprovalItem {
	items := make([]overlay.ApprovalItem, 0, len(approvals))
	for _, approval := range approvals {
		var details struct {
			TaskTitle string `json:"task_title"`
			AgentKind string `json:"agent_kind"`
			Command   string `json:"command"`
			Plan      string `json:"plan"`
		}
		_ = json.Unmarshal(approval.Details, &details)
		who := details.TaskTitle
		if who == "" {
			who = approval.TaskID
		}
		asker := strings.Trim(strings.Join([]string{who, details.AgentKind}, " · "), " ·")
		item := overlay.ApprovalItem{ID: approval.ID, Title: approval.Title, Asker: asker, Detail: approval.Summary}
		switch approval.Source {
		case service.ApprovalSourceAgentQuestion:
			item.Kind, item.Question = "Question", true
		case service.ApprovalSourceAgentPermission:
			item.Kind = "Permission"
			if details.Plan != "" {
				item.Kind, item.Detail = "Plan", details.Plan
			}
		default:
			item.Kind = "Approval"
		}
		for _, option := range approval.Options {
			if option.Kind == service.ApprovalAllow {
				item.AllowLabel = option.Label
				break
			}
		}
		items = append(items, item)
	}
	return items
}
