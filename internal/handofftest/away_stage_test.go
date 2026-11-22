package handofftest

import (
	"context"
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
}
