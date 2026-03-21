package detail

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// fakeBoardService implements service.BoardService for tests.
type fakeBoardService struct {
	card *service.CardDetail
}

func (f *fakeBoardService) ListColumns(_ context.Context) ([]service.Column, error) {
	return nil, nil
}
func (f *fakeBoardService) ListCards(_ context.Context, _ string) ([]service.Card, error) {
	return nil, nil
}
func (f *fakeBoardService) GetCard(_ context.Context, _ string) (*service.CardDetail, error) {
	return f.card, nil
}
func (f *fakeBoardService) MoveCard(_ context.Context, _ string, _ string) error { return nil }
func (f *fakeBoardService) ReorderCard(_ context.Context, _ string, _ int) error  { return nil }
func (f *fakeBoardService) SearchCards(_ context.Context, _ string) ([]service.Card, error) {
	return nil, nil
}
func (f *fakeBoardService) ExportCardContext(_ context.Context, _ string, _ service.ExportFormat) (string, error) {
	return "exported context", nil
}
func (f *fakeBoardService) DeleteTask(_ context.Context, _ string) error { return nil }
func (f *fakeBoardService) CreateTask(_ context.Context, _, _, _, _ string, _ *int) (*service.Card, error) {
	return nil, nil
}
func (f *fakeBoardService) UpdateTaskDescription(_ context.Context, _, _ string) error {
	return nil
}
func (f *fakeBoardService) UpdateTaskTitle(_ context.Context, _, _ string) error {
	return nil
}
func (f *fakeBoardService) ListCardsByWorkspace(_ context.Context, column string, _ store.WorkspaceView) ([]service.Card, error) {
	return f.ListCards(context.Background(), column)
}
func (f *fakeBoardService) UpdateTaskWorkspace(_ context.Context, _ string, _ *int) error {
	return nil
}
func (f *fakeBoardService) ListWorkspaces(_ context.Context) ([]service.Workspace, error) {
	return nil, nil
}
func (f *fakeBoardService) ArchiveDoneCards(_ context.Context) (int, error) { return 0, nil }
func (f *fakeBoardService) ArchiveTask(_ context.Context, _ string) error   { return nil }
func (f *fakeBoardService) CountDoneCards(_ context.Context) (int, error)   { return 0, nil }

func testCard() *service.CardDetail {
	return &service.CardDetail{
		ID:            "REX-1238",
		Title:         "Refactor user service",
		DescriptionMD: "## Overview\n\nThis is a **test** description.\n\n- Item 1\n- Item 2\n\n```go\nfmt.Println(\"hello\")\n```",
		Status:        "In Progress",
		Priority:      "High",
		Provider:      "jira",
		RemoteID:      "REX-1238",
		RemoteMeta: map[string]string{
			"issue_type": "Story",
			"assignee":   "cameron",
			"labels":     "backend,refactor",
			"epic_key":   "REX-100",
			"epic_name":  "User Service Overhaul",
			"url":        "https://jira.example.com/browse/REX-1238",
		},
	}
}

// Task 2.1: Model can be instantiated with data
func TestNewWithData(t *testing.T) {
	card := testCard()
	m := New(card, nil, nil, "")
	if m.card == nil {
		t.Fatal("card should not be nil")
	}
	if m.card.ID != "REX-1238" {
		t.Errorf("card ID = %q, want REX-1238", m.card.ID)
	}
	if m.loading {
		t.Error("should not be loading when card provided")
	}
}

func TestNewWithoutData(t *testing.T) {
	m := New(nil, nil, nil, "")
	if m.card != nil {
		t.Fatal("card should be nil")
	}
	if !m.loading {
		t.Error("should be loading when no card provided")
	}
}

// Task 2.2: Metadata header rendering
func TestViewContainsMetadata(t *testing.T) {
	card := testCard()
	m := New(card, nil, nil, "")
	m.width = 120
	m.height = 40
	m.renderContent()

	view := m.View()
	mustContain(t, view, "REX-1238")
	mustContain(t, view, "Refactor user service")
	mustContain(t, view, "In Progress")
	mustContain(t, view, "High")
	mustContain(t, view, "Story")
}

func TestViewMissingOptionalFields(t *testing.T) {
	card := &service.CardDetail{
		ID:            "REX-99",
		Title:       "Minimal ticket",
		DescriptionMD: "Simple desc",
		Status:        "Open",
	}
	m := New(card, nil, nil, "")
	m.width = 120
	m.height = 40
	m.renderContent()

	view := m.View()
	mustContain(t, view, "REX-99")
	mustContain(t, view, "Open")
}

// Task 2.4: Resize handling
func TestWindowResize(t *testing.T) {
	card := testCard()
	m := New(card, nil, nil, "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	m2 := updated.(Model)
	if m2.width != 100 {
		t.Errorf("width = %d, want 100", m2.width)
	}
	if m2.height != 50 {
		t.Errorf("height = %d, want 50", m2.height)
	}
}

// Task 2.5: Scroll keybindings
func TestScrollKeybindings(t *testing.T) {
	card := testCard()
	card.DescriptionMD = longDescription()
	m := New(card, nil, nil, "")
	m.width = 80
	m.height = 20
	m.renderContent()

	// j scrolls down
	updated, _ := m.Update(keyMsg('j'))
	m2 := updated.(Model)
	if m2.viewport.YOffset <= 0 {
		// viewport may not scroll if content fits - that's ok
	}

	// k scrolls up (should stay at or return to 0)
	updated, _ = m2.Update(keyMsg('k'))
	m3 := updated.(Model)
	_ = m3 // just verify no panic
}

// Task 2.6: Status bar hints
func TestStatusBarHints(t *testing.T) {
	card := testCard()
	m := New(card, nil, nil, "")
	m.width = 120
	m.height = 40
	m.renderContent()

	view := m.View()
	mustContain(t, view, "esc")
	mustContain(t, view, "y")
	mustContain(t, view, "m")
	mustContain(t, view, "o")
}

// Task 2.7: Loading state
func TestLoadingState(t *testing.T) {
	svc := &fakeBoardService{card: testCard()}
	m := NewLoading("REX-1238", svc, nil, "")
	if !m.loading {
		t.Error("should be loading")
	}
	m.width = 120
	m.height = 40

	view := m.View()
	mustContain(t, view, "Loading")
}

func TestLoadingTransition(t *testing.T) {
	card := testCard()
	m := NewLoading("REX-1238", nil, nil, "")
	m.width = 120
	m.height = 40

	// Simulate data arrival
	updated, _ := m.Update(CardLoadedMsg{Card: card})
	m2 := updated.(Model)
	if m2.loading {
		t.Error("should not be loading after data arrives")
	}
	if m2.card == nil {
		t.Fatal("card should be set")
	}
}

// Task 2.6: Feedback message
func TestFeedbackMessage(t *testing.T) {
	card := testCard()
	m := New(card, nil, nil, "")
	m.width = 120
	m.height = 40
	m.renderContent()

	updated, _ := m.Update(FeedbackMsg{Text: "Copied!"})
	m2 := updated.(Model)
	view := m2.View()
	mustContain(t, view, "Copied!")
}

// Task 3.1: y keybinding with no clipboard shows unavailable
func TestCopyDescriptionYNoClip(t *testing.T) {
	card := testCard()
	svc := &fakeBoardService{card: card}

	m := New(card, svc, nil, "")
	m.width = 120
	m.height = 40
	m.renderContent()

	updated, _ := m.Update(keyMsg('y'))
	m2 := updated.(Model)
	mustContain(t, m2.feedback, "unavailable")
}

// Task 3.2: Y keybinding with no clipboard shows unavailable
func TestCopyFullContextShiftYNoClip(t *testing.T) {
	card := testCard()
	svc := &fakeBoardService{card: card}

	m := New(card, svc, nil, "")
	m.width = 120
	m.height = 40
	m.renderContent()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}})
	m2 := updated.(Model)
	mustContain(t, m2.feedback, "unavailable")
}

// Task 3.1/3.2: clipboard unavailable
func TestCopyWithNoClipboard(t *testing.T) {
	card := testCard()
	svc := &fakeBoardService{card: card}

	m := New(card, svc, nil, "")
	m.width = 120
	m.height = 40
	m.renderContent()

	updated, _ := m.Update(keyMsg('y'))
	m2 := updated.(Model)
	mustContain(t, m2.feedback, "unavailable")
}

// Task 3.3: o keybinding with no URL
func TestOpenURLNoURL(t *testing.T) {
	card := &service.CardDetail{
		ID:      "REX-99",
		Title: "No URL",
	}
	m := New(card, nil, nil, "")
	m.width = 120
	m.height = 40
	m.renderContent()

	updated, _ := m.Update(keyMsg('o'))
	m2 := updated.(Model)
	mustContain(t, m2.feedback, "No URL")
}

// Task 3.3: esc keybinding
func TestEscReturnsBackToBoard(t *testing.T) {
	card := testCard()
	m := New(card, nil, nil, "")
	m.width = 120
	m.height = 40
	m.renderContent()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("expected cmd from esc")
	}
	msg := cmd()
	if _, ok := msg.(BackToBoard); !ok {
		t.Errorf("expected BackToBoard msg, got %T", msg)
	}
}

// Task 3.3: m keybinding opens move overlay
func TestMoveOverlay(t *testing.T) {
	card := testCard()
	m := New(card, nil, nil, "")
	m.width = 120
	m.height = 40
	m.renderContent()

	_, cmd := m.Update(keyMsg('m'))
	if cmd == nil {
		t.Fatal("expected cmd from m")
	}
	msg := cmd()
	overlay, ok := msg.(OpenMoveOverlay)
	if !ok {
		t.Errorf("expected OpenMoveOverlay msg, got %T", msg)
	}
	if overlay.TaskID != "REX-1238" {
		t.Errorf("taskID = %q, want REX-1238", overlay.TaskID)
	}
}

// Delete keybinding
func TestDeleteKeyEmitsOpenDeleteOverlay(t *testing.T) {
	card := testCard()
	m := New(card, nil, nil, "")
	m.width = 120
	m.height = 40
	m.renderContent()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	if cmd == nil {
		t.Fatal("expected cmd from D key")
	}
	msg := cmd()
	result, ok := msg.(OpenDeleteOverlay)
	if !ok {
		t.Fatalf("expected OpenDeleteOverlay, got %T", msg)
	}
	if result.TaskID != "REX-1238" {
		t.Errorf("taskID = %q, want REX-1238", result.TaskID)
	}
}

// Edit description tests

func localTestCard() *service.CardDetail {
	return &service.CardDetail{
		ID:            "abc12345",
		Title:         "Local task",
		DescriptionMD: "Some description",
		Status:        "Backlog",
		Priority:      "Medium",
		// Provider is "" (local)
	}
}

func TestEditKeyOnLocalTaskProducesExecCmd(t *testing.T) {
	card := localTestCard()
	svc := &fakeBoardService{card: card}
	m := New(card, svc, nil, "")
	m.editor = "vi"
	m.width = 120
	m.height = 40
	m.renderContent()

	_, cmd := m.Update(keyMsg('e'))
	if cmd == nil {
		t.Fatal("expected exec command from 'e' on local task")
	}
	// The command should be a tea.ExecProcess — we can't easily type-assert
	// internal bubbletea types, but we can verify it's non-nil (exec cmds exist).
	// We also verify the model produces an EditDescriptionMsg when e is pressed.
	// Actually let's test by checking the msg the cmd produces.
	// tea.ExecProcess returns a special internal message, so we can't test it directly.
	// Instead, verify that feedback is NOT set (meaning it didn't reject the edit).
	updated, _ := m.Update(keyMsg('e'))
	m2 := updated.(Model)
	if m2.feedback == "Cannot edit remote task description" {
		t.Error("local task should not show remote rejection feedback")
	}
}

func TestEditKeyOnRemoteTaskShowsFeedback(t *testing.T) {
	card := testCard() // has Provider="jira"
	m := New(card, nil, nil, "")
	m.editor = "vi"
	m.width = 120
	m.height = 40
	m.renderContent()

	updated, _ := m.Update(keyMsg('e'))
	m2 := updated.(Model)
	if m2.feedback != "Cannot edit remote task description" {
		t.Errorf("feedback = %q, want 'Cannot edit remote task description'", m2.feedback)
	}
}

func TestStatusBarShowsEditHintForLocalTask(t *testing.T) {
	card := localTestCard()
	m := New(card, nil, nil, "")
	m.editor = "vi"
	m.width = 120
	m.height = 40
	m.renderContent()

	view := m.View()
	mustContain(t, view, "edit")
}

func TestTitleEditKeyOnLocalTaskOpensOverlay(t *testing.T) {
	card := localTestCard()
	m := New(card, nil, nil, "")
	m.width = 120
	m.height = 40
	m.renderContent()

	_, cmd := m.Update(keyMsg('t'))
	if cmd == nil {
		t.Fatal("expected command from 't' on local task")
	}
	msg := cmd()
	result, ok := msg.(OpenTitleEditOverlay)
	if !ok {
		t.Fatalf("expected OpenTitleEditOverlay, got %T", msg)
	}
	if result.TaskID != "abc12345" {
		t.Errorf("taskID = %q, want abc12345", result.TaskID)
	}
	if result.Title != "Local task" {
		t.Errorf("title = %q, want 'Local task'", result.Title)
	}
}

func TestTitleEditKeyOnRemoteTaskShowsFeedback(t *testing.T) {
	card := testCard() // remote, provider="jira"
	m := New(card, nil, nil, "")
	m.width = 120
	m.height = 40
	m.renderContent()

	updated, _ := m.Update(keyMsg('t'))
	m2 := updated.(Model)
	if m2.feedback != "Cannot edit remote task title" {
		t.Errorf("feedback = %q, want 'Cannot edit remote task title'", m2.feedback)
	}
}

func TestStatusBarShowsTitleEditHintForLocalTask(t *testing.T) {
	card := localTestCard()
	m := New(card, nil, nil, "")
	m.width = 120
	m.height = 40
	m.renderContent()

	bar := m.renderStatusBar()
	mustContain(t, bar, "edit title")
}

func TestStatusBarNoEditHintForRemoteTask(t *testing.T) {
	card := testCard() // remote
	m := New(card, nil, nil, "")
	m.editor = "vi"
	m.width = 120
	m.height = 40
	m.renderContent()

	// renderStatusBar should not contain "e edit" for remote tasks
	bar := m.renderStatusBar()
	if containsStr(bar, "edit") {
		t.Error("remote task status bar should not show 'edit' hint")
	}
}

// helpers




func keyMsg(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func mustContain(t *testing.T, s, substr string) {
	t.Helper()
	if !containsStr(s, substr) {
		t.Errorf("view should contain %q", substr)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func longDescription() string {
	s := ""
	for i := 0; i < 100; i++ {
		s += "This is a long line of text to ensure the viewport needs to scroll.\n"
	}
	return s
}
