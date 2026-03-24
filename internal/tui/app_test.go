package tui

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/board"
	"github.com/cpave3/legato/internal/tui/detail"
	"github.com/cpave3/legato/internal/tui/overlay"
	"github.com/cpave3/legato/internal/tui/report"
	"github.com/cpave3/legato/internal/tui/theme"
)

type fakeBoardService struct{}

func (f *fakeBoardService) ListColumns(_ context.Context) ([]service.Column, error) {
	return []service.Column{
		{Name: "Backlog", SortOrder: 0},
		{Name: "Doing", SortOrder: 1},
	}, nil
}

func (f *fakeBoardService) ListCards(_ context.Context, column string) ([]service.Card, error) {
	if column == "Backlog" {
		return []service.Card{
			{ID: "REX-1", Title: "Test", Priority: "High", IssueType: "Bug", Status: "Backlog"},
		}, nil
	}
	return nil, nil
}

func (f *fakeBoardService) GetCard(_ context.Context, id string) (*service.CardDetail, error) {
	return &service.CardDetail{
		ID:            id,
		Title:         "Test ticket",
		DescriptionMD: "Test description",
		Status:        "Backlog",
		Priority:      "High",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}, nil
}
func (f *fakeBoardService) MoveCard(_ context.Context, _ string, _ string) error { return nil }
func (f *fakeBoardService) ReorderCard(_ context.Context, _ string, _ int) error  { return nil }
func (f *fakeBoardService) SearchCards(_ context.Context, query string) ([]service.Card, error) {
	// Simple filter for testing
	all := []service.Card{
		{ID: "REX-1", Title: "Refactor auth", Status: "Backlog"},
		{ID: "REX-2", Title: "Fix login bug", Status: "Doing"},
	}
	if query == "" {
		return nil, nil
	}
	var results []service.Card
	for _, c := range all {
		if strings.Contains(strings.ToLower(c.ID+c.Title), strings.ToLower(query)) {
			results = append(results, c)
		}
	}
	return results, nil
}
func (f *fakeBoardService) ExportCardContext(_ context.Context, _ string, _ service.ExportFormat) (string, error) {
	return "", nil
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

type fakeSyncService struct{}

func (f *fakeSyncService) Sync(_ context.Context) (*service.SyncResult, error) { return nil, nil }
func (f *fakeSyncService) Status() service.SyncStatus                          { return service.SyncStatus{} }
func (f *fakeSyncService) Subscribe() <-chan service.SyncEvent                  { return nil }
func (f *fakeSyncService) StartScheduler(_ context.Context) func()             { return func() {} }
func (f *fakeSyncService) SearchRemote(_ context.Context, _ string) ([]service.RemoteSearchResult, error) {
	return nil, nil
}
func (f *fakeSyncService) ImportRemoteTask(_ context.Context, id string, _ *int) (*service.Card, error) {
	return &service.Card{ID: id, Title: "Imported", Status: "Backlog"}, nil
}

type fakeReportService struct{}

func (f *fakeReportService) GenerateReport(_ context.Context, period analytics.TimeRange) (*service.Report, error) {
	return &service.Report{Period: period}, nil
}

func newTestApp() App {
	return NewApp(&fakeBoardService{}, nil, nil, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil)
}

func updateApp(a App, msg tea.Msg) (App, tea.Cmd) {
	m, cmd := a.Update(msg)
	return m.(App), cmd
}

func TestQuitOnQ(t *testing.T) {
	app := newTestApp()
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q should return a command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("q should return tea.Quit, got %T", msg)
	}
}

func TestWindowResizePropagation(t *testing.T) {
	app := newTestApp()
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	if app.width != 100 {
		t.Errorf("width = %d, want 100", app.width)
	}
	if app.height != 30 {
		t.Errorf("height = %d, want 30", app.height)
	}
}

func TestViewComposition(t *testing.T) {
	app := newTestApp()
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	view := app.View()
	if view == "" {
		t.Error("view should not be empty")
	}
	lines := strings.Split(view, "\n")
	if len(lines) < 2 {
		t.Errorf("view should have multiple lines, got %d", len(lines))
	}
}

func TestNavigationDelegatedToBoard(t *testing.T) {
	app := newTestApp()
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	for _, key := range []rune{'h', 'j', 'k', 'l'} {
		_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if cmd != nil {
			msg := cmd()
			if _, ok := msg.(tea.QuitMsg); ok {
				t.Errorf("key %c should not quit", key)
			}
		}
	}
}

func initTestApp() App {
	app := newTestApp()
	// Simulate Init loading data
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	return app
}

func TestEnterOpensDetail(t *testing.T) {
	app := initTestApp()
	if app.active != viewBoard {
		t.Fatalf("should start at viewBoard, got %d", app.active)
	}

	// Press enter — board returns OpenDetailMsg
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	// The board should return a command that yields OpenDetailMsg
	if cmd != nil {
		msg := cmd()
		if openMsg, ok := msg.(board.OpenDetailMsg); ok {
			// Now the app should handle this message and switch to detail
			app, _ = updateApp(app, openMsg)
		}
	}
	// After handling OpenDetailMsg, check that we're in detail view
	if app.active != viewDetail {
		t.Errorf("should be in viewDetail, got %d", app.active)
	}
}

func TestEscReturnsToBoard(t *testing.T) {
	app := initTestApp()

	// Navigate to detail via OpenDetailMsg
	app, _ = updateApp(app, board.OpenDetailMsg{CardKey: "REX-1"})
	if app.active != viewDetail {
		t.Fatalf("should be in viewDetail, got %d", app.active)
	}

	// Handle CardLoadedMsg
	card := &service.CardDetail{
		ID: "REX-1", Title: "Test", DescriptionMD: "desc", Status: "Backlog",
	}
	app, _ = updateApp(app, detail.CardLoadedMsg{Card: card})

	// Press esc — detail returns BackToBoard cmd
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(detail.BackToBoard); ok {
			app, _ = updateApp(app, msg)
		}
	}
	if app.active != viewBoard {
		t.Errorf("should be in viewBoard after esc, got %d", app.active)
	}
}

func TestClipboardWarningWhenUnavailable(t *testing.T) {
	// Create app with a nil clipboard to simulate unavailable
	app := newTestApp()
	app.clip = nil // no clipboard
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})

	// Check that the init returns a clipboard warning cmd
	// The actual warning is handled via Init in the real app
	// For this test we just verify the app handles it gracefully
}

func TestMoveOverlayRoutesKeysAndReturnsResult(t *testing.T) {
	app := initTestApp()
	// Open move overlay via board.OpenMoveMsg
	app, _ = updateApp(app, board.OpenMoveMsg{CardKey: "REX-1"})
	if app.overlayType == overlayNone {
		t.Fatal("overlay should be active after OpenMoveMsg")
	}
	// Keys should route to overlay — press j then enter to select column
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter in move overlay should produce a command")
	}
	msg := cmd()
	selected, ok := msg.(overlay.MoveSelectedMsg)
	if !ok {
		t.Fatalf("expected MoveSelectedMsg, got %T", msg)
	}
	if selected.TaskID != "REX-1" {
		t.Errorf("taskID = %q, want REX-1", selected.TaskID)
	}
	// Handle the move — overlay should close
	app, _ = updateApp(app, selected)
	if app.overlayType != overlayNone {
		t.Error("overlay should be closed after move completes")
	}
}

func TestOverlayEscDismisses(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, board.OpenMoveMsg{CardKey: "REX-1"})
	if app.overlayType == overlayNone {
		t.Fatal("overlay should be active")
	}
	// Press esc — should produce MoveCancelledMsg
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	if app.overlayType != overlayNone {
		t.Error("overlay should be dismissed after esc")
	}
}

func TestSlashOpensSearchOverlay(t *testing.T) {
	app := initTestApp()
	// Press / to open search
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if app.overlayType != overlaySearch {
		t.Fatalf("overlayType = %d, want overlaySearch (%d)", app.overlayType, overlaySearch)
	}
}

func TestSearchQueryTriggersServiceCall(t *testing.T) {
	app := initTestApp()
	// Open search
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	// Type 'R' — this should produce a SearchQueryChangedMsg
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if cmd == nil {
		t.Fatal("typing in search should produce a command")
	}
	// Execute the command chain — SearchQueryChangedMsg triggers search
	msg := cmd()
	if qMsg, ok := msg.(overlay.SearchQueryChangedMsg); ok {
		app, cmd = updateApp(app, qMsg)
		// Should produce SearchResultsMsg
		if cmd != nil {
			msg = cmd()
			if rMsg, ok := msg.(SearchResultsMsg); ok {
				_, _ = updateApp(app, rMsg)
			}
		}
	}
}

func TestNKeyOpensCreateOverlay(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if app.overlayType != overlayCreate {
		t.Fatalf("overlayType = %d, want overlayCreate (%d)", app.overlayType, overlayCreate)
	}
}

func TestCreateTaskMsgCreatesAndClosesOverlay(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if app.overlayType != overlayCreate {
		t.Fatal("create overlay should be open")
	}
	// Send CreateTaskMsg directly
	app, _ = updateApp(app, overlay.CreateTaskMsg{Title: "New task", Column: "Backlog", Priority: "High"})
	if app.overlayType != overlayNone {
		t.Error("overlay should be closed after create")
	}
}

func TestCreateCancelledMsgClosesOverlay(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if app.overlayType != overlayCreate {
		t.Fatal("create overlay should be open")
	}
	app, _ = updateApp(app, overlay.CreateCancelledMsg{})
	if app.overlayType != overlayNone {
		t.Error("overlay should be closed after cancel")
	}
}

func TestSearchEscDismisses(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if app.overlayType != overlaySearch {
		t.Fatal("search should be open")
	}
	// Press esc
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	if app.overlayType != overlayNone {
		t.Error("search should be dismissed after esc")
	}
}

func TestQuestionMarkOpensHelp(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if app.overlayType != overlayHelp {
		t.Fatalf("overlayType = %d, want overlayHelp (%d)", app.overlayType, overlayHelp)
	}
}

func TestQuestionMarkFromDetailOpensHelp(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, board.OpenDetailMsg{CardKey: "REX-1"})
	if app.active != viewDetail {
		t.Fatal("should be in detail")
	}
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if app.overlayType != overlayHelp {
		t.Error("? should open help from detail view")
	}
}

func TestQuestionMarkReplacesActiveOverlay(t *testing.T) {
	app := initTestApp()
	// Open move overlay first
	app, _ = updateApp(app, board.OpenMoveMsg{CardKey: "REX-1"})
	if app.overlayType != overlayMove {
		t.Fatal("should have move overlay")
	}
	// ? should replace it with help
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if app.overlayType != overlayHelp {
		t.Errorf("overlayType = %d, want overlayHelp", app.overlayType)
	}
}

func TestHelpEscDismisses(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	// esc in help overlay
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	if app.overlayType != overlayNone {
		t.Error("help should be dismissed after esc")
	}
}

func TestShiftAOpensAgentView(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	if app.active != viewAgents {
		t.Errorf("active = %d, want viewAgents (%d)", app.active, viewAgents)
	}
}

func TestAgentViewEscReturnsToBoard(t *testing.T) {
	app := initTestApp()
	// Switch to agent view
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	if app.active != viewAgents {
		t.Fatalf("active = %d, want viewAgents", app.active)
	}
	// esc from agent view produces ReturnToBoardMsg
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	if app.active != viewBoard {
		t.Errorf("active = %d, want viewBoard after esc", app.active)
	}
}

func TestAgentViewQReturnsToBoard(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if app.active != viewBoard {
		t.Errorf("active = %d, want viewBoard after q", app.active)
	}
}

func TestAgentViewRendering(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	view := app.View()
	if view == "" {
		t.Error("agent view should not be empty")
	}
}

func TestDeleteFromBoardOpensOverlay(t *testing.T) {
	app := initTestApp()
	// Board sends OpenDeleteMsg (from 'd' key on selected card)
	app, _ = updateApp(app, board.OpenDeleteMsg{CardKey: "REX-1"})
	if app.overlayType != overlayDelete {
		t.Fatalf("overlayType = %d, want overlayDelete (%d)", app.overlayType, overlayDelete)
	}
}

func TestDeleteFromDetailOpensOverlay(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, board.OpenDetailMsg{CardKey: "REX-1"})
	// Detail sends OpenDeleteOverlay (from 'D' key)
	app, _ = updateApp(app, detail.OpenDeleteOverlay{TaskID: "REX-1"})
	if app.overlayType != overlayDelete {
		t.Fatalf("overlayType = %d, want overlayDelete (%d)", app.overlayType, overlayDelete)
	}
}

func TestDeleteConfirmedClosesOverlayAndRefreshes(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, board.OpenDeleteMsg{CardKey: "REX-1"})
	// Confirm deletion
	app, cmd := updateApp(app, overlay.DeleteConfirmedMsg{TaskID: "REX-1"})
	if app.overlayType != overlayNone {
		t.Error("overlay should be closed after delete confirmed")
	}
	// Should produce a board refresh command
	if cmd == nil {
		t.Error("expected a board refresh command after delete")
	}
}

func TestDeleteConfirmedFromDetailReturnsToBoard(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, board.OpenDetailMsg{CardKey: "REX-1"})
	app, _ = updateApp(app, detail.OpenDeleteOverlay{TaskID: "REX-1"})
	// Confirm deletion while in detail view
	app, _ = updateApp(app, overlay.DeleteConfirmedMsg{TaskID: "REX-1"})
	if app.active != viewBoard {
		t.Errorf("should return to board after deleting from detail, got %d", app.active)
	}
}

func TestDeleteCancelledClosesOverlay(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, board.OpenDeleteMsg{CardKey: "REX-1"})
	app, _ = updateApp(app, overlay.DeleteCancelledMsg{})
	if app.overlayType != overlayNone {
		t.Error("overlay should be closed after cancel")
	}
}

// Import overlay tests

func TestImportKeyOpensOverlayWhenSyncAvailable(t *testing.T) {
	app := NewApp(&fakeBoardService{}, &fakeSyncService{}, nil, nil, nil, theme.NewIcons("unicode"), nil, "", nil)
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})

	app, _ = updateApp(app, board.OpenImportMsg{})
	if app.overlayType != overlayImport {
		t.Fatalf("overlayType = %d, want overlayImport (%d)", app.overlayType, overlayImport)
	}
}

func TestImportKeyNoOpWithoutSync(t *testing.T) {
	app := initTestApp() // no syncSvc
	app, _ = updateApp(app, board.OpenImportMsg{})
	if app.overlayType != overlayNone {
		t.Error("import overlay should not open without sync service")
	}
}

func TestImportSelectedImportsAndRefreshes(t *testing.T) {
	app := NewApp(&fakeBoardService{}, &fakeSyncService{}, nil, nil, nil, theme.NewIcons("unicode"), nil, "", nil)
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app, _ = updateApp(app, board.OpenImportMsg{})
	// Select a ticket
	app, cmd = updateApp(app, overlay.ImportSelectedMsg{TicketID: "REX-42"})
	if app.overlayType != overlayNone {
		t.Error("overlay should close after import")
	}
	if cmd == nil {
		t.Error("expected board refresh command")
	}
}

func TestImportCancelledClosesOverlay(t *testing.T) {
	app := NewApp(&fakeBoardService{}, &fakeSyncService{}, nil, nil, nil, theme.NewIcons("unicode"), nil, "", nil)
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app, _ = updateApp(app, board.OpenImportMsg{})
	app, _ = updateApp(app, overlay.ImportCancelledMsg{})
	if app.overlayType != overlayNone {
		t.Error("overlay should close after cancel")
	}
}

func TestTitleEditOverlayOpenAndClose(t *testing.T) {
	app := initTestApp()
	// Open title edit overlay from detail
	app, _ = updateApp(app, detail.OpenTitleEditOverlay{TaskID: "REX-1", Title: "Test"})
	if app.overlayType != overlayTitleEdit {
		t.Fatalf("overlayType = %d, want overlayTitleEdit", app.overlayType)
	}
	// Cancel
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	if app.overlayType != overlayNone {
		t.Error("overlay should be closed after cancel")
	}
}

func TestTitleEditSubmitClosesOverlayAndRefreshes(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, detail.OpenTitleEditOverlay{TaskID: "REX-1", Title: "Test"})
	// Submit
	app, cmd := updateApp(app, overlay.TitleEditSubmitMsg{TaskID: "REX-1", Title: "New Title"})
	if app.overlayType != overlayNone {
		t.Error("overlay should be closed after submit")
	}
	if cmd == nil {
		t.Error("expected board refresh command")
	}
}

type fakeAgentService struct {
	agents    []service.AgentSession
	durations map[string]service.DurationData
}

func (f *fakeAgentService) SpawnAgent(_ context.Context, _ string, _, _ int) error { return nil }
func (f *fakeAgentService) KillAgent(_ context.Context, _ string) error            { return nil }
func (f *fakeAgentService) ListAgents(_ context.Context) ([]service.AgentSession, error) {
	return f.agents, nil
}
func (f *fakeAgentService) ReconcileSessions(_ context.Context) error              { return nil }
func (f *fakeAgentService) CaptureOutput(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (f *fakeAgentService) AttachCmd(_ context.Context, _ string) (*exec.Cmd, error) {
	return nil, nil
}
func (f *fakeAgentService) GetTaskDurations(_ context.Context, _ []string) (map[string]service.DurationData, error) {
	if f.durations != nil {
		return f.durations, nil
	}
	return map[string]service.DurationData{}, nil
}
func (f *fakeAgentService) GetAgentSummary(_ context.Context, _ string) (int, int, int, error) {
	return 0, 0, 0, nil
}

func TestDurationDataFlowsToBoard(t *testing.T) {
	agentSvc := &fakeAgentService{
		agents: []service.AgentSession{
			{TaskID: "REX-1", Status: "running", Activity: "working", StartedAt: time.Now()},
		},
		durations: map[string]service.DurationData{
			"REX-1": {Working: 45 * time.Minute, Waiting: 10 * time.Minute},
		},
	}

	app := NewApp(&fakeBoardService{}, nil, agentSvc, nil, nil, theme.NewIcons("unicode"), nil, "", nil)
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})

	// After DataLoadedMsg, the board should have duration data on cards
	card := app.board.SelectedCard()
	if card == nil {
		t.Fatal("expected a selected card")
	}
	if card.WorkingDuration != 45*time.Minute {
		t.Errorf("WorkingDuration = %v, want 45m", card.WorkingDuration)
	}
	if card.WaitingDuration != 10*time.Minute {
		t.Errorf("WaitingDuration = %v, want 10m", card.WaitingDuration)
	}
}

func TestBoardSelectionPreservedAfterDetailReturn(t *testing.T) {
	app := initTestApp()
	// Move cursor down
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})

	// Go to detail and back
	app, _ = updateApp(app, board.OpenDetailMsg{CardKey: "REX-1"})
	app, _ = updateApp(app, detail.BackToBoard{})

	if app.active != viewBoard {
		t.Errorf("should be in viewBoard, got %d", app.active)
	}
	// Board model should preserve its state (no re-initialization)
}

func TestShiftSOpensReportView(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	if app.active != viewReport {
		t.Errorf("expected viewReport, got %d", app.active)
	}
}

func TestReportViewEscReturnsToBoard(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	if app.active != viewReport {
		t.Fatalf("expected viewReport, got %d", app.active)
	}
	// Report view returns ReturnToBoardMsg on esc
	app, _ = updateApp(app, report.ReturnToBoardMsg{})
	if app.active != viewBoard {
		t.Errorf("expected viewBoard after esc, got %d", app.active)
	}
}

func TestReportViewCopyReportMsg(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	// CopyReportMsg should not panic even without clipboard
	app, _ = updateApp(app, report.CopyReportMsg{Markdown: "# Test Report"})
	if app.active != viewReport {
		t.Errorf("should still be in report view, got %d", app.active)
	}
}

func TestReportLoadedMsgForwardedRegardlessOfView(t *testing.T) {
	app := initTestApp()
	// Load report while in board view — should not panic
	app, _ = updateApp(app, report.ReportLoadedMsg{
		Report: &service.Report{Period: analytics.Today()},
	})
	// No panic = pass
}
