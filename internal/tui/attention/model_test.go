package attention

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
)

type fakeService struct {
	items   []service.AttentionItem
	execErr error
	calls   int
}

func (f *fakeService) List(context.Context, service.AttentionFilter) ([]service.AttentionItem, error) {
	return f.items, nil
}
func (f *fakeService) Execute(context.Context, string, service.AttentionActionRequest) error {
	f.calls++
	return f.execErr
}
func (f *fakeService) Dismiss(context.Context, string, string) error {
	f.calls++
	return f.execErr
}

func TestQueueListsItemsAndExecutesPrimaryAction(t *testing.T) {
	svc := &fakeService{items: []service.AttentionItem{
		{ID: "att-1", Title: "Approve plan", Reason: "Plan is ready", Producer: "plans",
			SourceRevision: "1", Urgency: "normal",
			Actions: []service.AttentionAction{
				{ID: "open", Label: "Open", Kind: "navigate"},
				{ID: "approve", Label: "Approve", Kind: "execute", RequiresConfirm: true},
			}},
		{ID: "att-2", Title: "Review tour", Reason: "Tour is ready", Producer: "reviews",
			SourceRevision: "2", Urgency: "high",
			Actions: []service.AttentionAction{
				{ID: "open", Label: "Open", Kind: "navigate"},
				{ID: "complete", Label: "Complete", Kind: "execute"},
			}},
	}}
	m := New(svc)
	m, _ = m.Update(queueMsg{items: svc.items})

	view := m.View()
	if view == "" || !contains(view, "Approve plan") || !contains(view, "Review tour") {
		t.Fatalf("view = %q", view)
	}

	// Move to second item and execute its primary action.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("enter")})
	if cmd == nil {
		t.Fatal("enter on execute action produced no command")
	}
	msg := cmd()
	if am, ok := msg.(actionMsg); ok && am.err != nil {
		t.Fatalf("action err = %v", am.err)
	}
	if svc.calls != 1 {
		t.Fatalf("exec calls = %d", svc.calls)
	}
}

func TestQueueDismissesSelectedItem(t *testing.T) {
	svc := &fakeService{items: []service.AttentionItem{
		{ID: "att-1", Title: "Approve plan", Reason: "ready", Producer: "plans",
			SourceRevision: "1", Actions: []service.AttentionAction{{ID: "open", Label: "Open", Kind: "navigate"}}},
	}}
	m := New(svc)
	m, _ = m.Update(queueMsg{items: svc.items})

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if cmd == nil {
		t.Fatal("dismiss produced no command")
	}
	msg := cmd()
	if am, ok := msg.(actionMsg); ok && am.err != nil {
		t.Fatalf("dismiss err = %v", am.err)
	}
	if svc.calls != 1 {
		t.Fatalf("dismiss calls = %d", svc.calls)
	}
}

func TestQueueReturnsToBoardOnEscape(t *testing.T) {
	m := New(&fakeService{})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q produced no command")
	}
	if _, ok := cmd().(ReturnToBoardMsg); !ok {
		t.Fatal("q did not return ReturnToBoardMsg")
	}
}

func TestQueueShowsError(t *testing.T) {
	m := New(&fakeService{})
	m, _ = m.Update(queueMsg{err: errors.New("connection failed")})
	view := m.View()
	if !contains(view, "connection failed") {
		t.Fatalf("view = %q", view)
	}
}

func TestQueueShowsEmpty(t *testing.T) {
	m := New(&fakeService{})
	m, _ = m.Update(queueMsg{items: nil})
	view := m.View()
	if !contains(view, "No items") {
		t.Fatalf("view = %q", view)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(s) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestEnterOnAnArgusCaseOpensItInTheBrowser(t *testing.T) {
	svc := &fakeService{items: []service.AttentionItem{{
		ID: "att-1", Title: "CI failing", Reason: "flaky", Producer: "argus", SourceRevision: "r1", Urgency: "high",
		Navigation: service.AttentionNavigation{Kind: "external", Target: "https://argus.lan/cases/1"},
	}}}
	var opened string
	m := New(svc)
	m.openURL = func(url string) error { opened = url; return nil }
	m, _ = m.Update(queueMsg{items: svc.items})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did nothing")
	}
	m, _ = m.Update(cmd())
	if opened != "https://argus.lan/cases/1" || svc.calls != 0 {
		t.Fatalf("opened %q, service calls %d", opened, svc.calls)
	}
	if !contains(m.View(), "Opened in the browser") {
		t.Fatalf("view = %q", m.View())
	}
}

func TestTheTUIListsAnAwayTasksItemsButRefersTheirActionsToTheWebUI(t *testing.T) {
	svc := &fakeService{items: []service.AttentionItem{{
		ID: "att-1", Title: "Fix it is done on vm-1", Reason: "Return it", Producer: service.HandoffAttentionProducer,
		SignalKind: service.AttentionSignalHandoffDone, SourceRevision: "done", Urgency: "normal",
		Navigation: service.AttentionNavigation{Kind: "task", Target: "abc"},
		Actions: []service.AttentionAction{
			{ID: "return", Label: "Return", Kind: "execute"},
			{ID: "open", Label: "Open task", Kind: "navigate"},
		},
	}}}
	m := New(svc)
	m, _ = m.Update(queueMsg{items: svc.items})
	if !contains(m.View(), "Fix it is done on vm-1") {
		t.Fatalf("view = %q", m.View())
	}
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		m, _ = m.Update(cmd())
	}
	if svc.calls != 0 {
		t.Fatalf("the TUI returned an away task: %d calls", svc.calls)
	}
	if !contains(m.View(), "Return, recall and take back from the web UI") {
		t.Fatalf("view = %q", m.View())
	}
}
