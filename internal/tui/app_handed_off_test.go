package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/overlay"
	"github.com/cpave3/legato/internal/tui/theme"
)

func TestLinkingAPRToAnAwayCardIsRefusedInTheStatusBar(t *testing.T) {
	svc := &awayBoard{}
	app := NewApp(svc, nil, nil, &fakeTargetedPRService{}, &fakeReportService{}, theme.NewIcons("unicode"), nil, "vi", nil, nil, "", &fakeSwarmService{}, nil)
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 160, Height: 40})
	app, _ = updateApp(app, app.board.Init()())
	app = pressKey(app, 'p') // link PR
	if app.overlayType != overlayNone {
		t.Fatalf("the link PR overlay opened for an away card: %v", app.overlayType)
	}
	if bar := app.statusBar.View(); !strings.Contains(bar, awayRefusal) {
		t.Fatalf("status bar = %q, want %q", bar, awayRefusal)
	}
}

// handedOffBoard is a worker's board whose Done card HO-1 a main handed
// to it: deleting or archiving it is refused, as the real service does.
type handedOffBoard struct {
	fakeBoardService
}

func (f *handedOffBoard) DeleteTask(_ context.Context, id string) error {
	return &service.TaskHandedOffError{TaskID: id, Main: "workstation"}
}

// The Done column holds HO-1, which bulk archiving keeps.
func (f *handedOffBoard) CountDoneCards(context.Context) (int, error)   { return 1, nil }
func (f *handedOffBoard) ArchiveDoneCards(context.Context) (int, error) { return 0, nil }

const handedOffRefusal = "this task belongs to workstation; return or recall it from there"

func TestDeletingAHandedOffCardOnTheWorkerSaysWhoItBelongsTo(t *testing.T) {
	app := NewApp(&handedOffBoard{}, nil, nil, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "vi", nil, nil, "", &fakeSwarmService{}, nil)
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 160, Height: 40})
	app, _ = updateApp(app, overlay.DeleteConfirmedMsg{TaskID: "HO-1"})
	if bar := app.statusBar.View(); !strings.Contains(bar, handedOffRefusal) {
		t.Fatalf("status bar = %q, want %q", bar, handedOffRefusal)
	}
}

func TestArchivingDoneCardsSaysWhenHandedOffCardsWereKept(t *testing.T) {
	app := NewApp(&handedOffBoard{}, nil, nil, nil, &fakeReportService{}, theme.NewIcons("unicode"), nil, "vi", nil, nil, "", &fakeSwarmService{}, nil)
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 160, Height: 40})
	app, _ = updateApp(app, overlay.ArchiveConfirmedMsg{})
	if bar := app.statusBar.View(); !strings.Contains(bar, "1 done card kept") {
		t.Fatalf("status bar = %q", bar)
	}
}
