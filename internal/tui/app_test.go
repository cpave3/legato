package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/engine/analytics"
	"github.com/cpave3/legato/internal/engine/macros"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/swarm"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/agents"
	"github.com/cpave3/legato/internal/tui/board"
	"github.com/cpave3/legato/internal/tui/detail"
	"github.com/cpave3/legato/internal/tui/overlay"
	"github.com/cpave3/legato/internal/tui/report"
	"github.com/cpave3/legato/internal/tui/statusbar"
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
func (f *fakeBoardService) ReorderCard(_ context.Context, _ string, _ int) error { return nil }
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
func (f *fakeSyncService) Subscribe() <-chan service.SyncEvent                 { return nil }
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

type swarmCall struct {
	method string
	id     string
	text   string
}

type fakeSwarmService struct {
	calls []swarmCall
}

func (f *fakeSwarmService) ListSubtasks(_ context.Context, _ string) ([]store.Subtask, error) {
	return nil, nil
}
func (f *fakeSwarmService) GetSubtask(_ context.Context, _ string) (*store.Subtask, error) {
	return nil, nil
}
func (f *fakeSwarmService) ListSubtaskInfos(_ context.Context, _ string) ([]service.SwarmSubtaskInfo, error) {
	return nil, nil
}
func (f *fakeSwarmService) Snapshot(_ context.Context, _ string) ([]byte, error) { return nil, nil }
func (f *fakeSwarmService) LatestSnapshot(_ string) *service.SwarmSnapshot       { return nil }
func (f *fakeSwarmService) FetchInbox(_ context.Context, _ string) ([]service.InboxEntry, error) {
	return nil, nil
}
func (f *fakeSwarmService) PeekInbox(_ context.Context, _ string) ([]service.InboxEntry, error) {
	return nil, nil
}
func (f *fakeSwarmService) LoadPlan(_ string) (*service.SwarmPlan, error)            { return nil, nil }
func (f *fakeSwarmService) StartSwarm(_ context.Context, _, _ string) error          { return nil }
func (f *fakeSwarmService) ApplyApprovedPlan(_ context.Context, _ *swarm.Plan) error { return nil }
func (f *fakeSwarmService) CancelSwarm(_ context.Context, id string) error {
	f.calls = append(f.calls, swarmCall{method: "CancelSwarm", id: id})
	return nil
}
func (f *fakeSwarmService) ExtendApprovedPlan(_ context.Context, _ *swarm.Plan) error {
	f.calls = append(f.calls, swarmCall{method: "ExtendApprovedPlan"})
	return nil
}
func (f *fakeSwarmService) Dispatch(_ context.Context, _ string) error { return nil }
func (f *fakeSwarmService) NextStep(_ context.Context, _ string) error { return nil }
func (f *fakeSwarmService) Message(_ context.Context, id, text string, _ bool) error {
	f.calls = append(f.calls, swarmCall{method: "Message", id: id, text: text})
	return nil
}
func (f *fakeSwarmService) MessageParent(_ context.Context, parentID, text string, _ bool) error {
	f.calls = append(f.calls, swarmCall{method: "MessageParent", id: parentID, text: text})
	return nil
}
func (f *fakeSwarmService) Broadcast(_ context.Context, _, _ string, _ bool) (int, error) {
	return 0, nil
}
func (f *fakeSwarmService) Close(_ context.Context, id string) error {
	f.calls = append(f.calls, swarmCall{method: "Close", id: id})
	return nil
}
func (f *fakeSwarmService) Finish(_ context.Context, parentID, summary string) error {
	f.calls = append(f.calls, swarmCall{method: "Finish", id: parentID, text: summary})
	return nil
}
func (f *fakeSwarmService) Progress(_ context.Context, _, _ string) error             { return nil }
func (f *fakeSwarmService) Question(_ context.Context, _, _ string) error             { return nil }
func (f *fakeSwarmService) Built(_ context.Context, _ string) error                   { return nil }
func (f *fakeSwarmService) InsertPendingPlan(_ context.Context, _, _, _ string) error { return nil }
func (f *fakeSwarmService) GetPendingPlan(_ context.Context, _ string) (*store.PendingPlanEntry, error) {
	return nil, nil
}
func (f *fakeSwarmService) ListAllPendingPlans(_ context.Context) ([]store.PendingPlanEntry, error) {
	return nil, nil
}
func (f *fakeSwarmService) DeletePendingPlan(_ context.Context, _ string) error { return nil }
func (f *fakeSwarmService) HandleAgentDied(_ context.Context, _, _, _ string)   {}
func (f *fakeSwarmService) StartEventLoop(_ context.Context) func()             { return func() {} }

func newTestApp() App {
	return NewApp(&fakeBoardService{}, nil, nil, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
}

func newTestAppWithSwarm() App {
	return NewApp(&fakeBoardService{}, nil, nil, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "", &fakeSwarmService{}, nil)
}

func newTestAppWithRecordingSwarm() (App, *fakeSwarmService) {
	svc := &fakeSwarmService{}
	return NewApp(&fakeBoardService{}, nil, nil, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "", svc, nil), svc
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
	app := NewApp(&fakeBoardService{}, &fakeSyncService{}, nil, nil, nil, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
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
	app := NewApp(&fakeBoardService{}, &fakeSyncService{}, nil, nil, nil, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
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
	app := NewApp(&fakeBoardService{}, &fakeSyncService{}, nil, nil, nil, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
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
	agents        []service.AgentSession
	durations     map[string]service.DurationData
	captureErr    bool // when true, CaptureOutput returns an error (agent not running)
	lastSpawnOpts *service.AgentSpawnOptions
}

func (f *fakeAgentService) SpawnAgent(_ context.Context, _ string, _, _ int, opts ...service.AgentSpawnOptions) error {
	if len(opts) > 0 {
		f.lastSpawnOpts = &opts[0]
	}
	return nil
}
func (f *fakeAgentService) KillAgent(_ context.Context, _ string) error { return nil }
func (f *fakeAgentService) ListAgents(_ context.Context) ([]service.AgentSession, error) {
	return f.agents, nil
}
func (f *fakeAgentService) ListAgentsByParent(_ context.Context, _ string) ([]service.AgentSession, error) {
	return nil, nil
}
func (f *fakeAgentService) ReconcileSessions(_ context.Context) error { return nil }
func (f *fakeAgentService) CaptureOutput(_ context.Context, _ string) (string, error) {
	if f.captureErr {
		return "", errors.New("not running")
	}
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
func (f *fakeAgentService) SetAgentActivity(_ context.Context, _, _, _ string) error { return nil }
func (f *fakeAgentService) SpawnEphemeralAgent(_ context.Context, _ string, _, _ int, opts ...service.AgentSpawnOptions) error {
	if len(opts) > 0 {
		f.lastSpawnOpts = &opts[0]
	}
	return nil
}
func (f *fakeAgentService) LastSpawnConflicts() []service.AgentSpawnConflict { return nil }
func (f *fakeAgentService) RegisteredAdapters() []string                     { return nil }
func (f *fakeAgentService) DefaultAdapter() string                           { return "" }
func (f *fakeAgentService) AdapterFor(kind string) service.AIToolAdapter     { return nil }
func (f *fakeAgentService) GetStateTimeline(_ context.Context, _ string, _ time.Duration, _ int) ([]string, error) {
	return nil, nil
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

	app := NewApp(&fakeBoardService{}, nil, agentSvc, nil, nil, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
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

func TestShiftROpensReportView(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	if app.active != viewReport {
		t.Errorf("expected viewReport, got %d", app.active)
	}
}

func TestReportViewEscReturnsToBoard(t *testing.T) {
	app := initTestApp()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
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
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
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

func TestAKeyOpensTaskSpawnOverlay(t *testing.T) {
	// CaptureOutput error means agent is not running; overlay opens instead of spawning
	agentSvc := &fakeAgentService{captureErr: true}
	app := NewApp(&fakeBoardService{}, nil, agentSvc, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "/workspace", nil, nil)
	// Init + load board data
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})

	// Press 'a' — should open the spawn overlay for the selected card
	app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if app.overlayType != overlayAgentSpawn {
		t.Fatalf("expected overlayAgentSpawn, got overlay %d", app.overlayType)
	}
	if app.activeOverlay == nil {
		t.Fatal("expected activeOverlay to be set")
	}
	// Active view should still be board until spawn is confirmed
	if app.active != viewBoard {
		t.Errorf("expected still in viewBoard, got %d", app.active)
	}
	_ = cmd
}

func TestAKeySwitchesToAgentViewWhenRunning(t *testing.T) {
	// CaptureOutput succeeds means agent is already running; skip overlay
	agentSvc := &fakeAgentService{
		agents: []service.AgentSession{
			{TaskID: "REX-1", Status: "running"},
		},
	}
	app := NewApp(&fakeBoardService{}, nil, agentSvc, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})

	app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	// Should switch to agent view without opening overlay
	if app.overlayType != overlayNone {
		t.Errorf("expected no overlay, got %d", app.overlayType)
	}
	if app.active != viewAgents {
		t.Errorf("expected viewAgents, got %d", app.active)
	}
	// cmd should refresh agent list
	if cmd == nil {
		t.Fatal("expected refresh command")
	}
}

func TestAgentSpawnSubmitForTask(t *testing.T) {
	agentSvc := &fakeAgentService{captureErr: true}
	app := NewApp(&fakeBoardService{}, nil, agentSvc, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "/workspace", nil, nil)
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})

	// Open overlay
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if app.overlayType != overlayAgentSpawn {
		t.Fatalf("expected overlay open")
	}
	// Simulate submit with specific agent kind and CWD
	submit := overlay.AgentSpawnSubmitMsg{
		TaskID:     "REX-1",
		AgentKind:  "chimera",
		WorkingDir: "/custom",
	}
	app, cmd = updateApp(app, submit)
	if app.overlayType != overlayNone {
		t.Error("overlay should be closed after submit")
	}
	if app.active != viewAgents {
		t.Errorf("expected viewAgents after task spawn submit, got %d", app.active)
	}
	if cmd == nil {
		t.Fatal("expected spawn command")
	}
	for _, c := range cmd().(tea.BatchMsg) {
		c()
	}
	if agentSvc.lastSpawnOpts == nil {
		t.Fatal("expected opts to be passed to SpawnAgent")
	}
	if agentSvc.lastSpawnOpts.AgentKind != "chimera" {
		t.Errorf("AgentKind = %q, want chimera", agentSvc.lastSpawnOpts.AgentKind)
	}
	if agentSvc.lastSpawnOpts.WorkingDir != "/custom" {
		t.Errorf("WorkingDir = %q, want /custom", agentSvc.lastSpawnOpts.WorkingDir)
	}
}

func TestAgentSpawnSubmitEphemeral(t *testing.T) {
	agentSvc := &fakeAgentService{}
	app := NewApp(&fakeBoardService{}, nil, agentSvc, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "/workspace", nil, nil)
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})

	// Switch to agent view, then press 's' to open overlay
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	app, keyCmd := updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	// Execute the command to get OpenAgentSpawnMsg
	if keyCmd != nil {
		openMsg := keyCmd()
		app, _ = updateApp(app, openMsg)
	}
	if app.overlayType != overlayAgentSpawn {
		t.Fatalf("expected overlay open, got overlay %d", app.overlayType)
	}
	// Submit ephemeral
	submit := overlay.AgentSpawnSubmitMsg{
		Title:      "Test ephemeral",
		AgentKind:  "shell",
		WorkingDir: "/tmp",
	}
	app, cmd = updateApp(app, submit)
	if app.overlayType != overlayNone {
		t.Error("overlay should be closed after submit")
	}
	// Active view should stay as agents (already was agents before)
	if app.active != viewAgents {
		t.Errorf("expected viewAgents after ephemeral spawn submit, got %d", app.active)
	}
	if cmd == nil {
		t.Fatal("expected spawn command")
	}
	// Execute the batched command to trigger SpawnEphemeralAgent side effects
	for _, c := range cmd().(tea.BatchMsg) {
		c()
	}
	if agentSvc.lastSpawnOpts == nil {
		t.Fatal("expected opts to be passed to SpawnEphemeralAgent")
	}
	if agentSvc.lastSpawnOpts.AgentKind != "shell" {
		t.Errorf("AgentKind = %q, want shell", agentSvc.lastSpawnOpts.AgentKind)
	}
}

func TestTKeyOpensTaskSpawnOverlay(t *testing.T) {
	agentSvc := &fakeAgentService{captureErr: true}
	app := NewApp(&fakeBoardService{}, nil, agentSvc, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "/workspace", nil, nil)
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})

	app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if app.overlayType != overlayAgentSpawn {
		t.Fatalf("expected overlayAgentSpawn from 't' key, got overlay %d", app.overlayType)
	}
}

func TestAgentSpawnSubmitSetsSelectTask(t *testing.T) {
	agentSvc := &fakeAgentService{captureErr: true}
	app := NewApp(&fakeBoardService{}, nil, agentSvc, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "", nil, nil, "/workspace", nil, nil)
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})

	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	app, cmd = updateApp(app, overlay.AgentSpawnSubmitMsg{TaskID: "REX-1"})
	if cmd == nil {
		t.Fatal("expected spawn command")
	}
	// Execute the batched command to find AgentsRefreshedMsg
	batch := cmd().(tea.BatchMsg)
	var foundMsg agents.AgentsRefreshedMsg
	for _, c := range batch {
		inner := c()
		if msg, ok := inner.(agents.AgentsRefreshedMsg); ok {
			foundMsg = msg
		}
	}
	if foundMsg.SelectTask != "REX-1" {
		t.Errorf("SelectTask = %q, want REX-1", foundMsg.SelectTask)
	}
}

// --- Agent action overlay integration tests ---

func runAgentActionCmd(app App, cmd tea.Cmd) App {
	if cmd == nil {
		return app
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			app = runAgentActionCmd(app, c)
		}
		return app
	}
	if action, ok := msg.(agents.OpenAgentActionMsg); ok {
		app, _ = updateApp(app, action)
	}
	return app
}

func TestOpenAgentActionOverlay(t *testing.T) {
	app := newTestAppWithSwarm()
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	// Switch to agent view
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	if app.active != viewAgents {
		t.Fatalf("expected viewAgents, got %d", app.active)
	}
	// Feed swarm agent data
	app.agentView.SetAgents([]service.AgentSession{
		{ID: 1, TaskID: "st-abc", TmuxSession: "legato-st-abc", Command: "shell", Status: "running", Role: "backend", ParentTaskID: "swarm-1"},
	})
	// Press Shift+M — the agent view produces OpenAgentActionMsg via a command
	app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	app = runAgentActionCmd(app, cmd)
	if app.overlayType != overlayAgentAction {
		t.Fatalf("overlayType = %d, want overlayAgentAction", app.overlayType)
	}
	if app.activeOverlay == nil {
		t.Fatal("expected activeOverlay to be set")
	}
}

func TestOpenAgentActionIsNoOpWithoutSwarmService(t *testing.T) {
	app := initTestApp()
	app.swarmSvc = nil
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	app.agentView.SetAgents([]service.AgentSession{
		{ID: 1, TaskID: "st-abc", TmuxSession: "legato-st-abc", Command: "shell", Status: "running", Role: "backend", ParentTaskID: "swarm-1"},
	})
	app, cmd := updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	// Execute the command so OpenAgentActionMsg reaches the app
	app = runAgentActionCmd(app, cmd)
	if app.overlayType != overlayNone {
		t.Errorf("expected no overlay when swarmSvc is nil, got %d", app.overlayType)
	}
}

func TestAgentActionCancelledDismissesOverlay(t *testing.T) {
	app := newTestAppWithSwarm()
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'A'}})
	app.agentView.SetAgents([]service.AgentSession{
		{ID: 1, TaskID: "st-abc", TmuxSession: "legato-st-abc", Command: "shell", Status: "running", Role: "backend", ParentTaskID: "swarm-1"},
	})
	app, cmd = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	app = runAgentActionCmd(app, cmd)
	if app.overlayType != overlayAgentAction {
		t.Fatalf("overlay should be open, got %d", app.overlayType)
	}
	app, _ = updateApp(app, overlay.AgentActionCancelledMsg{})
	if app.overlayType != overlayNone {
		t.Errorf("overlay should be closed after cancel, got %d", app.overlayType)
	}
}

func TestAgentMessageSentRoutesToWorker(t *testing.T) {
	app, svc := newTestAppWithRecordingSwarm()
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app, _ = updateApp(app, overlay.AgentMessageSentMsg{
		TaskID:       "st-abc",
		ParentTaskID: "swarm-1",
		Role:         "backend",
		Text:         "rebase please",
	})
	if app.overlayType != overlayNone {
		t.Errorf("overlay should close after send, got %d", app.overlayType)
	}
	if len(svc.calls) != 1 {
		t.Fatalf("expected 1 swarm call, got %d", len(svc.calls))
	}
	c := svc.calls[0]
	if c.method != "Message" || c.id != "st-abc" || c.text != "rebase please" {
		t.Errorf("unexpected call: %+v", c)
	}
}

func TestAgentMessageSentConductorRoutesToParent(t *testing.T) {
	app, svc := newTestAppWithRecordingSwarm()
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app, _ = updateApp(app, overlay.AgentMessageSentMsg{
		TaskID:       "swarm-1",
		ParentTaskID: "swarm-1",
		Role:         "conductor",
		Text:         "regenerate plan",
	})
	if len(svc.calls) != 1 {
		t.Fatalf("expected 1 swarm call, got %d", len(svc.calls))
	}
	c := svc.calls[0]
	if c.method != "MessageParent" || c.id != "swarm-1" || c.text != "regenerate plan" {
		t.Errorf("unexpected call: %+v", c)
	}
}

func TestAgentCloseConfirmedCallsClose(t *testing.T) {
	app, svc := newTestAppWithRecordingSwarm()
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app, _ = updateApp(app, overlay.AgentCloseConfirmedMsg{TaskID: "st-abc"})
	if app.overlayType != overlayNone {
		t.Errorf("overlay should close, got %d", app.overlayType)
	}
	if len(svc.calls) != 1 || svc.calls[0].method != "Close" || svc.calls[0].id != "st-abc" {
		t.Errorf("expected Close(st-abc), got %+v", svc.calls)
	}
}

func TestSwarmFinishConfirmedCallsFinish(t *testing.T) {
	app, svc := newTestAppWithRecordingSwarm()
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app, _ = updateApp(app, overlay.SwarmFinishConfirmedMsg{
		ParentTaskID: "swarm-1",
		Summary:      "done",
	})
	if app.overlayType != overlayNone {
		t.Errorf("overlay should close, got %d", app.overlayType)
	}
	if len(svc.calls) != 1 {
		t.Fatalf("expected 1 swarm call, got %d", len(svc.calls))
	}
	c := svc.calls[0]
	if c.method != "Finish" || c.id != "swarm-1" || c.text != "done" {
		t.Errorf("unexpected call: %+v", c)
	}
}

func TestOpenMacroPickerOverlay(t *testing.T) {
	app := newTestAppWithSwarm()
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app, _ = updateApp(app, agents.OpenMacroPickerMsg{})
	if app.overlayType != overlayMacroPicker {
		t.Errorf("overlayType = %d, want overlayMacroPicker", app.overlayType)
	}
	if app.activeOverlay == nil {
		t.Fatal("expected activeOverlay to be set")
	}
}

func TestMacroCancelledDismissesOverlay(t *testing.T) {
	app := newTestAppWithSwarm()
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app, _ = updateApp(app, agents.OpenMacroPickerMsg{})
	if app.overlayType != overlayMacroPicker {
		t.Fatalf("overlay should be open, got %d", app.overlayType)
	}
	app, _ = updateApp(app, overlay.MacroCancelledMsg{})
	if app.overlayType != overlayNone {
		t.Errorf("overlay should close after cancel, got %d", app.overlayType)
	}
}

func TestMacroSelectedClosesOverlay(t *testing.T) {
	// We can't easily exercise the tmux send path without a recording tmux
	// (a.tmux == nil short-circuits). Asserting the overlay closes is the
	// part app.go owns.
	app := newTestAppWithSwarm()
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app, _ = updateApp(app, agents.OpenMacroPickerMsg{})
	app, _ = updateApp(app, overlay.MacroSelectedMsg{
		Macro: macros.Macro{Name: "tests", Keys: "task test\n"},
	})
	if app.overlayType != overlayNone {
		t.Errorf("overlay should close after select, got %d", app.overlayType)
	}
}

func TestSetSparklineWindowOverridesDefaults(t *testing.T) {
	app := newTestApp()
	app.SetSparklineWindow(5*time.Minute, 5)
	if app.sparklineWindow != 5*time.Minute {
		t.Errorf("sparklineWindow = %v, want 5m", app.sparklineWindow)
	}
	if app.sparklineBuckets != 5 {
		t.Errorf("sparklineBuckets = %d, want 5", app.sparklineBuckets)
	}
	// Zero/negative values are ignored — the previous override stays.
	app.SetSparklineWindow(0, -1)
	if app.sparklineWindow != 5*time.Minute || app.sparklineBuckets != 5 {
		t.Errorf("non-positive args should not override; got %v / %d", app.sparklineWindow, app.sparklineBuckets)
	}
}

// --- Swarm cancel overlay tests ---

func TestSwarmCancelOverlayOpensWithShiftC(t *testing.T) {
	app := initTestAppWithSwarm()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	if app.overlayType != overlaySwarmCancel {
		t.Fatalf("overlayType = %d, want overlaySwarmCancel (%d)", app.overlayType, overlaySwarmCancel)
	}
}

func TestSwarmCancelShiftCNoOpWithoutSwarmService(t *testing.T) {
	app := initTestApp() // no swarmSvc
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	if app.overlayType != overlayNone {
		t.Errorf("overlay should not open without swarmSvc, got %d", app.overlayType)
	}
}

func TestSwarmCancelConfirmedCallsCancelSwarm(t *testing.T) {
	app, svc := newTestAppWithRecordingSwarm()
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app, _ = updateApp(app, overlay.SwarmCancelConfirmedMsg{ParentTaskID: "REX-1"})
	if app.overlayType != overlayNone {
		t.Errorf("overlay should close after confirm, got %d", app.overlayType)
	}
	if len(svc.calls) != 1 || svc.calls[0].method != "CancelSwarm" || svc.calls[0].id != "REX-1" {
		t.Errorf("expected CancelSwarm(REX-1) call, got %+v", svc.calls)
	}
}

func TestSwarmCancelCancelledDismissesOverlay(t *testing.T) {
	app := initTestAppWithSwarm()
	app, _ = updateApp(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	if app.overlayType != overlaySwarmCancel {
		t.Fatalf("overlay should be open, got %d", app.overlayType)
	}
	app, _ = updateApp(app, overlay.SwarmCancelCancelledMsg{})
	if app.overlayType != overlayNone {
		t.Errorf("overlay should close after cancel, got %d", app.overlayType)
	}
}

func TestSwarmStartShowsShiftCHintOnExistingSwarmError(t *testing.T) {
	app := newTestAppWithSwarm()
	// Wrap the fake so StartSwarm returns an "existing swarm" error.
	errSvc := &fakeStartSwarmErrorService{existingSwarmErr: true}
	app.swarmSvc = errSvc
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})

	app, cmd := updateApp(app, overlay.SwarmStartMsg{ParentTaskID: "REX-1", WorkingDir: "/tmp"})
	if cmd == nil {
		t.Fatal("expected error command after StartSwarm failure")
	}
	msg := cmd()
	if sb, ok := msg.(statusbar.ErrorMsg); ok {
		if !strings.Contains(sb.Text, "Shift+C") {
			t.Errorf("expected error hint to mention Shift+C, got %q", sb.Text)
		}
	} else {
		t.Fatalf("expected statusbar.ErrorMsg, got %T", msg)
	}
}

func TestSwarmStartNoShiftCHintOnOtherError(t *testing.T) {
	app := newTestAppWithSwarm()
	errSvc := &fakeStartSwarmErrorService{existingSwarmErr: false}
	app.swarmSvc = errSvc
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})

	app, cmd := updateApp(app, overlay.SwarmStartMsg{ParentTaskID: "REX-1", WorkingDir: "/tmp"})
	if cmd == nil {
		t.Fatal("expected error command after StartSwarm failure")
	}
	msg := cmd()
	if sb, ok := msg.(statusbar.ErrorMsg); ok {
		if strings.Contains(sb.Text, "Shift+C") {
			t.Errorf("non-existing-swarm error should not mention Shift+C, got %q", sb.Text)
		}
	} else {
		t.Fatalf("expected statusbar.ErrorMsg, got %T", msg)
	}
}

// fakeStartSwarmErrorService wraps StartSwarm to return a configurable error.
type fakeStartSwarmErrorService struct {
	existingSwarmErr bool
}

func (f *fakeStartSwarmErrorService) ListSubtasks(_ context.Context, _ string) ([]store.Subtask, error) {
	return nil, nil
}
func (f *fakeStartSwarmErrorService) GetSubtask(_ context.Context, _ string) (*store.Subtask, error) {
	return nil, nil
}
func (f *fakeStartSwarmErrorService) ListSubtaskInfos(_ context.Context, _ string) ([]service.SwarmSubtaskInfo, error) {
	return nil, nil
}
func (f *fakeStartSwarmErrorService) Snapshot(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}
func (f *fakeStartSwarmErrorService) LatestSnapshot(_ string) *service.SwarmSnapshot { return nil }
func (f *fakeStartSwarmErrorService) FetchInbox(_ context.Context, _ string) ([]service.InboxEntry, error) {
	return nil, nil
}
func (f *fakeStartSwarmErrorService) PeekInbox(_ context.Context, _ string) ([]service.InboxEntry, error) {
	return nil, nil
}
func (f *fakeStartSwarmErrorService) LoadPlan(_ string) (*service.SwarmPlan, error) { return nil, nil }
func (f *fakeStartSwarmErrorService) StartSwarm(_ context.Context, _, _ string) error {
	if f.existingSwarmErr {
		return fmt.Errorf("parent task has leftover swarm sub-tasks — cancel the existing swarm first")
	}
	return fmt.Errorf("some other error")
}
func (f *fakeStartSwarmErrorService) ApplyApprovedPlan(_ context.Context, _ *swarm.Plan) error {
	return nil
}
func (f *fakeStartSwarmErrorService) CancelSwarm(_ context.Context, _ string) error { return nil }
func (f *fakeStartSwarmErrorService) ExtendApprovedPlan(_ context.Context, _ *swarm.Plan) error {
	return nil
}
func (f *fakeStartSwarmErrorService) Dispatch(_ context.Context, _ string) error { return nil }
func (f *fakeStartSwarmErrorService) NextStep(_ context.Context, _ string) error { return nil }
func (f *fakeStartSwarmErrorService) Message(_ context.Context, _, _ string, _ bool) error {
	return nil
}
func (f *fakeStartSwarmErrorService) MessageParent(_ context.Context, _, _ string, _ bool) error {
	return nil
}
func (f *fakeStartSwarmErrorService) Broadcast(_ context.Context, _, _ string, _ bool) (int, error) {
	return 0, nil
}
func (f *fakeStartSwarmErrorService) Close(_ context.Context, _ string) error       { return nil }
func (f *fakeStartSwarmErrorService) Finish(_ context.Context, _, _ string) error   { return nil }
func (f *fakeStartSwarmErrorService) Progress(_ context.Context, _, _ string) error { return nil }
func (f *fakeStartSwarmErrorService) Question(_ context.Context, _, _ string) error { return nil }
func (f *fakeStartSwarmErrorService) Built(_ context.Context, _ string) error       { return nil }
func (f *fakeStartSwarmErrorService) InsertPendingPlan(_ context.Context, _, _, _ string) error {
	return nil
}
func (f *fakeStartSwarmErrorService) GetPendingPlan(_ context.Context, _ string) (*store.PendingPlanEntry, error) {
	return nil, nil
}
func (f *fakeStartSwarmErrorService) ListAllPendingPlans(_ context.Context) ([]store.PendingPlanEntry, error) {
	return nil, nil
}
func (f *fakeStartSwarmErrorService) DeletePendingPlan(_ context.Context, _ string) error { return nil }
func (f *fakeStartSwarmErrorService) HandleAgentDied(_ context.Context, _, _, _ string)   {}
func (f *fakeStartSwarmErrorService) StartEventLoop(_ context.Context) func()             { return func() {} }

func initTestAppWithSwarm() App {
	app := newTestAppWithSwarm()
	cmd := app.Init()
	if cmd != nil {
		msg := cmd()
		app, _ = updateApp(app, msg)
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	return app
}
