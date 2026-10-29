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

func credentialItem() []ApprovalItem {
	return []ApprovalItem{{ID: "ap-9", Kind: "Approval", Title: "Use legato_default", Detail: "ssh.login on ssh://cameron@psi.pavey.network",
		Allows: []ApprovalChoice{{ID: "window", Label: "For 30 minutes"}, {ID: "once", Label: "Once"}}}}
}

func TestApprovalOverlayAllowsWithTheOptionOfANumber(t *testing.T) {
	model := NewApprovalOverlay(credentialItem())
	for keyName, want := range map[string]string{"1": "window", "2": "once", "y": "window"} {
		_, cmd := model.Update(key(keyName))
		msg, ok := cmd().(ApprovalDecideMsg)
		if !ok || msg.ID != "ap-9" || !msg.Allow || msg.Option != want {
			t.Errorf("%s: message = %#v, want option %s", keyName, cmd(), want)
		}
	}
	if _, cmd := model.Update(key("3")); cmd != nil {
		t.Errorf("3 with two options sent %#v", cmd())
	}
}

func TestApprovalOverlayListsTheWaysToAllow(t *testing.T) {
	view := NewApprovalOverlay(credentialItem()).View()
	for _, want := range []string{"1 for 30 minutes", "2 once", "n deny"} {
		if !strings.Contains(view, want) {
			t.Errorf("the view does not say %q:\n%s", want, view)
		}
	}
}

func TestASignedApprovalIsAllowedOnThePhone(t *testing.T) {
	items := credentialItem()
	items[0].Signed = true
	model := NewApprovalOverlay(items)
	for _, k := range []string{"y", "1"} {
		next, cmd := model.Update(key(k))
		if cmd != nil {
			t.Fatalf("%s sent %#v", k, cmd())
		}
		if !strings.Contains(next.View(), "phone") {
			t.Fatalf("%s: the overlay does not say to use the phone", k)
		}
	}
	if _, cmd := model.Update(key("n")); cmd == nil {
		t.Fatal("a signed approval cannot be denied here")
	}
	if !strings.Contains(model.View(), "signed on your phone") {
		t.Fatalf("view does not say it needs the phone:\n%s", model.View())
	}
}
