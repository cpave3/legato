package overlay

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
)

func samplePlan() *service.SwarmPlan {
	return &service.SwarmPlan{
		Header: service.SwarmPlanHeader{
			ParentTaskID: "p-1",
			WorkingDir:   "/tmp/work",
			Summary:      "Wire up frontend + backend",
		},
		Subtasks: []service.SwarmPlanSubtask{
			{Title: "Backend", Role: "backend", Agent: "claude-code"},
			{Title: "Frontend", Role: "frontend"},
		},
	}
}

func TestPlanApprovalRendersSubtasks(t *testing.T) {
	m := NewPlanApproval("p-1", "/tmp/plan.yaml", "/tmp/sock", "vi", samplePlan(), nil)
	view := m.View()
	mustContainAll(t, view, []string{"Plan proposed for p-1", "Backend", "Frontend", "Wire up frontend"})
}

func TestPlanApprovalLoadErrorRenders(t *testing.T) {
	m := NewPlanApproval("p-1", "/tmp/plan.yaml", "/tmp/sock", "vi", nil, errors.New("boom"))
	view := m.View()
	mustContain(t, view, "Failed to load plan")
}

func TestPlanApprovalYesEmitsApprove(t *testing.T) {
	m := NewPlanApproval("p-1", "/tmp/plan.yaml", "/tmp/sock", "vi", samplePlan(), nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd == nil {
		t.Fatal("expected command")
	}
	got, ok := cmd().(PlanApproveMsg)
	if !ok {
		t.Fatalf("got %T, want PlanApproveMsg", cmd())
	}
	if got.ParentTaskID != "p-1" || got.PlanPath != "/tmp/plan.yaml" || got.ReplySocket != "/tmp/sock" {
		t.Errorf("approve msg fields = %+v", got)
	}
}

func TestPlanApprovalYesNoOpWhenPlanNil(t *testing.T) {
	m := NewPlanApproval("p-1", "/tmp/plan.yaml", "/tmp/sock", "vi", nil, errors.New("load failed"))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if cmd != nil {
		t.Fatal("y on a load-failed plan should be a no-op")
	}
}

func TestPlanApprovalNEntersRejectMode(t *testing.T) {
	m := NewPlanApproval("p-1", "/tmp/plan.yaml", "/tmp/sock", "vi", samplePlan(), nil)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m2 := updated.(PlanApprovalOverlay)
	if m2.mode != planModeRejecting {
		t.Errorf("mode = %d, want planModeRejecting", m2.mode)
	}
}

func TestPlanApprovalEscFromReviewEmitsCancel(t *testing.T) {
	m := NewPlanApproval("p-1", "/tmp/plan.yaml", "/tmp/sock", "vi", samplePlan(), nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected command on esc")
	}
	if _, ok := cmd().(PlanCancelMsg); !ok {
		t.Fatalf("got %T, want PlanCancelMsg", cmd())
	}
}

func TestPlanApprovalEscFromRejectReturnsToReview(t *testing.T) {
	m := NewPlanApproval("p-1", "/tmp/plan.yaml", "/tmp/sock", "vi", samplePlan(), nil)
	m.mode = planModeRejecting
	m.notes = "stale"
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Errorf("esc in reject mode should not emit a command; got %T", cmd())
	}
	m2 := updated.(PlanApprovalOverlay)
	if m2.mode != planModeReview {
		t.Errorf("mode = %d, want planModeReview", m2.mode)
	}
	if m2.notes != "" {
		t.Errorf("notes = %q, want empty after esc-back", m2.notes)
	}
}

func TestPlanApprovalRejectEnterEmitsRejectMsgWithNotes(t *testing.T) {
	m := NewPlanApproval("p-1", "/tmp/plan.yaml", "/tmp/sock", "vi", samplePlan(), nil)
	m.mode = planModeRejecting
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	m = updated.(PlanApprovalOverlay)
	if m.notes != "hello" {
		t.Errorf("notes after typing = %q, want %q", m.notes, "hello")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got, ok := cmd().(PlanRejectMsg)
	if !ok {
		t.Fatalf("got %T, want PlanRejectMsg", cmd())
	}
	if got.Notes != "hello" {
		t.Errorf("Notes = %q, want %q", got.Notes, "hello")
	}
}

func TestPlanApprovalReloadedMsgUpdatesPlan(t *testing.T) {
	m := NewPlanApproval("p-1", "/tmp/plan.yaml", "/tmp/sock", "vi", nil, errors.New("old err"))
	fresh := samplePlan()
	updated, _ := m.Update(PlanReloadedMsg{ParentTaskID: "p-1", PlanPath: "/tmp/plan.yaml", Plan: fresh, Err: nil})
	m2 := updated.(PlanApprovalOverlay)
	if m2.plan == nil {
		t.Fatal("plan should be set after reload")
	}
	if m2.loadErr != nil {
		t.Errorf("loadErr should be cleared after successful reload, got %v", m2.loadErr)
	}
}

func TestPlanApprovalReloadedMsgKeepsErrorOnFailure(t *testing.T) {
	m := NewPlanApproval("p-1", "/tmp/plan.yaml", "/tmp/sock", "vi", samplePlan(), nil)
	updated, _ := m.Update(PlanReloadedMsg{ParentTaskID: "p-1", PlanPath: "/tmp/plan.yaml", Plan: nil, Err: errors.New("read fail")})
	m2 := updated.(PlanApprovalOverlay)
	if m2.loadErr == nil {
		t.Fatal("loadErr should be set after failed reload")
	}
}

func mustContainAll(t *testing.T, s string, needles []string) {
	t.Helper()
	for _, n := range needles {
		if !contains(s, n) {
			t.Errorf("expected %q in view, view=%q", n, s)
		}
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && indexOf(s, sub) >= 0 }
func indexOf(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
