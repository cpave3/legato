package service

import (
	"encoding/json"
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

// A summary row carries the worker agent's stage; one from a worker that
// sends none leaves the away card without one, and the fields are
// optional so an older main reading a new row is not upset.
func TestASummaryRowsStageReachesTheAwayCard(t *testing.T) {
	newRow, _ := json.Marshal(HandoffSummary{HandoffID: "h1", State: store.HandoffInProgress, Stage: "reviewing", StageReason: "PR #7 open"})
	away := awayTaskFromRow(store.AwayTask{TaskID: "T", HandoffID: "h1", LastSummary: ptr(string(newRow))})
	if away.Stage != "reviewing" || away.StageReason != "PR #7 open" {
		t.Fatalf("away = %+v", away)
	}

	old := `{"handoff_id":"h1","worker_task_id":"W","state":"in_progress","activity":"working"}`
	away = awayTaskFromRow(store.AwayTask{TaskID: "T", HandoffID: "h1", LastSummary: &old})
	if away.Stage != "" || away.Activity != "working" {
		t.Fatalf("away from an old worker = %+v", away)
	}

	var olderMain struct {
		HandoffID string `json:"handoff_id"`
		State     string `json:"state"`
	}
	if err := json.Unmarshal(newRow, &olderMain); err != nil || olderMain.State != store.HandoffInProgress {
		t.Fatalf("an older main reads %+v, %v", olderMain, err)
	}
	var fields map[string]any
	_ = json.Unmarshal([]byte(`{"handoff_id":"h1"}`), &fields)
	noStage, _ := json.Marshal(HandoffSummary{HandoffID: "h1"})
	if string(noStage) == "" || json.Unmarshal(noStage, &fields) != nil || fields["stage"] != nil {
		t.Fatalf("a row with no stage carries %s", noStage)
	}
}
