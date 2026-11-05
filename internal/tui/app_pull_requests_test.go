package tui

import (
	"context"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/tui/board"
	"github.com/cpave3/legato/internal/tui/theme"
)

// stackedBoardService is a board whose card REX-1 has a stack of PRs.
type stackedBoardService struct {
	fakeBoardService
}

func (s *stackedBoardService) ListPullRequests(_ context.Context, taskIDs []string) (map[string][]service.PRMetaView, error) {
	out := map[string][]service.PRMetaView{}
	for _, id := range taskIDs {
		if id == "REX-1" {
			out[id] = []service.PRMetaView{
				{PRNumber: 11, CheckStatus: "fail"},
				{PRNumber: 12, IsDraft: true, ReviewDecision: "APPROVED", CommentCount: 3},
			}
		}
	}
	return out, nil
}

func TestBoardCardsCarryEveryPullRequestWithoutGitHub(t *testing.T) {
	app := NewApp(&stackedBoardService{}, nil, nil, nil, nil, theme.NewIcons("unicode"), nil, "", nil, nil, "", nil, nil)
	if cmd := app.Init(); cmd != nil {
		app, _ = updateApp(app, cmd())
	}
	app, _ = updateApp(app, tea.WindowSizeMsg{Width: 100, Height: 30})

	card := app.board.SelectedCard()
	if card == nil {
		t.Fatal("expected a selected card")
	}
	want := []board.PRStateData{
		{PRNumber: 11, CheckStatus: "fail"},
		{PRNumber: 12, IsDraft: true, ReviewDecision: "APPROVED", CommentCount: 3},
	}
	if !reflect.DeepEqual(card.PullRequests, want) {
		t.Fatalf("pull requests = %+v, want %+v", card.PullRequests, want)
	}
}
