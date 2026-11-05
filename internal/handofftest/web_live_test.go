package handofftest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

func TestTheBoardAPIShowsAnAwayTasksLiveStateAndItsWorkersReach(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, away := awayOn(t, main, worker, paired.InstanceID, "Fix it")
	if err := worker.Agents.SetAgentActivity(ctx, away.WorkerTaskID, "waiting", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "needs you", func() bool { return mainAway(t, main, taskID).View == service.AwayViewNeedsYou })

	web := newMainWeb(t, main)
	type liveAway struct {
		View         string   `json:"view"`
		Activity     string   `json:"activity"`
		WorkerColumn string   `json:"worker_column"`
		WorkerTitle  string   `json:"worker_title"`
		Branches     []string `json:"branches"`
		URL          string   `json:"url"`
		Unreachable  string   `json:"worker_unreachable_since"`
	}
	var board struct {
		Columns []struct {
			Name  string `json:"name"`
			Cards []struct {
				ID   string    `json:"id"`
				Away *liveAway `json:"away"`
			} `json:"cards"`
		} `json:"columns"`
		Workers []struct {
			InstanceID       string  `json:"instance_id"`
			LastPullAt       *string `json:"last_pull_at"`
			UnreachableSince *string `json:"unreachable_since"`
			RefusedAt        *string `json:"refused_at"`
		} `json:"workers"`
	}
	read := func() *liveAway {
		t.Helper()
		if code := web.do(http.MethodGet, "/api/board", "", &board); code != http.StatusOK {
			t.Fatalf("board: %d", code)
		}
		for _, c := range board.Columns {
			for _, card := range c.Cards {
				if card.ID == taskID {
					return card.Away
				}
			}
		}
		t.Fatalf("no card %s", taskID)
		return nil
	}
	got := read()
	if got == nil || got.View != "needs_you" || got.Activity != "waiting" || got.WorkerColumn != "Doing" || got.WorkerTitle != "Fix it" ||
		got.URL != worker.URL+"/board?highlight_task="+away.WorkerTaskID {
		t.Fatalf("away %+v", got)
	}
	if len(board.Workers) != 1 || board.Workers[0].LastPullAt == nil || board.Workers[0].UnreachableSince != nil {
		t.Fatalf("workers %+v", board.Workers)
	}

	worker.Stop()
	eventually(t, "the drop", func() bool { return !main.Connections.Connected(paired.InstanceID) })
	worker.Clock.Advance(31 * time.Second)
	main.Connections.CheckReach(ctx)
	if got := read(); got.View != "unreachable" || got.Unreachable == "" {
		t.Fatalf("away %+v", got)
	}
	if board.Workers[0].UnreachableSince == nil {
		t.Fatalf("workers %+v", board.Workers)
	}
}
