package overlay

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func approvalItems() []ApprovalItem {
	return []ApprovalItem{
		{ID: "ap-1", Kind: "Permission", Title: "Run a command", Asker: "Fix sort · claude-code", Detail: "go test ./...", AllowLabel: "Allow once"},
		{ID: "ap-2", Kind: "Question", Title: "Which database?", Asker: "Add search · codex", Question: true},
	}
}

func key(r string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)}
}

func TestApprovalOverlayAllowsTheSelectedRequestWithY(t *testing.T) {
	model := NewApprovalOverlay(approvalItems())

	_, cmd := model.Update(key("y"))

	msg, ok := cmd().(ApprovalDecideMsg)
	if !ok || msg.ID != "ap-1" || !msg.Allow {
		t.Fatalf("message = %#v", cmd())
	}
}

func TestApprovalOverlayDeniesWithNAfterMovingDown(t *testing.T) {
	var model tea.Model = NewApprovalOverlay(approvalItems())

	model, _ = model.Update(key("j"))
	_, cmd := model.Update(key("n"))

	msg, ok := cmd().(ApprovalDecideMsg)
	if !ok || msg.ID != "ap-2" || msg.Allow {
		t.Fatalf("message = %#v", cmd())
	}
}

func TestApprovalOverlayDoesNotAllowAQuestionWithY(t *testing.T) {
	var model tea.Model = NewApprovalOverlay(approvalItems())
	model, _ = model.Update(key("j"))

	_, cmd := model.Update(key("y"))

	if cmd != nil {
		t.Fatalf("y on a question sent %#v; a question is answered in the web UI", cmd())
	}
	if !strings.Contains(model.View(), "web UI") {
		t.Fatal("the overlay does not say where to answer a question")
	}
}

func TestApprovalOverlayClosesWithEscape(t *testing.T) {
	_, cmd := NewApprovalOverlay(approvalItems()).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if _, ok := cmd().(ApprovalOverlayClosedMsg); !ok {
		t.Fatalf("message = %#v", cmd())
	}
}

func TestApprovalOverlayKeepsTheSelectionWhenTheListChanges(t *testing.T) {
	var model tea.Model = NewApprovalOverlay(approvalItems())
	model, _ = model.Update(key("j"))

	model = model.(ApprovalOverlay).SetItems(approvalItems()[1:])

	_, cmd := model.Update(key("n"))
	if msg := cmd().(ApprovalDecideMsg); msg.ID != "ap-2" {
		t.Fatalf("decided %s, want the request that stayed selected", msg.ID)
	}
	if view := model.View(); !strings.Contains(view, "Which database?") || !strings.Contains(view, "Add search · codex") {
		t.Fatalf("view = %s", view)
	}
}
