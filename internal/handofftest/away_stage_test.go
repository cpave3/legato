package handofftest

import (
	"context"
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The worker sends its agent's stage in its summary rows, and a stage
// change reaches the main at once, as the away card shows it.
func TestTheMainHearsTheWorkersAgentStage(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	taskID, away := awayOn(t, main, worker, paired.InstanceID, "Fix it")
	eventually(t, "the connection", func() bool { return main.Connections.Connected(paired.InstanceID) })

	stages := service.NewWorkStageService(worker.Store, worker.Bus)
	if _, err := stages.Report(ctx, service.TaskPlace(away.WorkerTaskID, ""), "", store.WorkStageReviewing, "PR #7 open"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the stage on the main", func() bool {
		got := mainAway(t, main, taskID)
		return got.Stage == store.WorkStageReviewing && got.StageReason == "PR #7 open"
	})

	// The main's web board carries it to the away card.
	var board struct {
		Columns []struct {
			Cards []struct {
				ID   string `json:"id"`
				Away *struct {
					Stage       string `json:"stage"`
					StageReason string `json:"stage_reason"`
				} `json:"away"`
			} `json:"cards"`
		} `json:"columns"`
	}
	newMainWeb(t, main).do(http.MethodGet, "/api/board", "", &board)
	for _, column := range board.Columns {
		for _, card := range column.Cards {
			if card.ID == taskID {
				if card.Away == nil || card.Away.Stage != store.WorkStageReviewing || card.Away.StageReason != "PR #7 open" {
					t.Fatalf("the board's away card = %+v", card.Away)
				}
				return
			}
		}
	}
	t.Fatalf("no card %s on the board", taskID)
}
