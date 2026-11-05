package handofftest

import (
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

func TestTheWebBoardReturnsRecallsAndReturnsAllDone(t *testing.T) {
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := liveMain(t, worker)
	paired := Pair(t, main, worker)
	main.StartConnections()
	web := newMainWeb(t, main)
	done, _, _ := workedOn(t, main, worker, paired.InstanceID, "Done")
	working, _, _ := workedOn(t, main, worker, paired.InstanceID, "Doing")
	alsoDone, _, _ := workedOn(t, main, worker, paired.InstanceID, "Done")
	eventually(t, "done on the main", func() bool {
		return mainAway(t, main, done).View == service.AwayViewDone && mainAway(t, main, alsoDone).View == service.AwayViewDone
	})

	// Return refuses a task not done, in the service's words.
	var refused struct {
		Error string `json:"error"`
	}
	if code := web.do(http.MethodPost, "/api/tasks/"+working+"/return", "", &refused); code != http.StatusConflict || refused.Error == "" {
		t.Fatalf("returning a working task: %d %+v", code, refused)
	}
	var recalled struct {
		Returned service.ReturnedTask `json:"returned"`
	}
	if code := web.do(http.MethodPost, "/api/tasks/"+working+"/recall", "", &recalled); code != http.StatusOK || !recalled.Returned.Recalled || recalled.Returned.Column != "Doing" {
		t.Fatalf("recall: %d %+v", code, recalled)
	}
	var all struct {
		Returned []service.ReturnedTask `json:"returned"`
		Error    string                 `json:"error"`
	}
	if code := web.do(http.MethodPost, "/api/workers/"+paired.InstanceID+"/return-done", "", &all); code != http.StatusOK || len(all.Returned) != 2 || all.Error != "" {
		t.Fatalf("return all done: %d %+v", code, all)
	}
	// Nothing is away now: the board shows every task in a column.
	var board boardJSON
	web.do(http.MethodGet, "/api/board", "", &board)
	for _, id := range []string{done, alsoDone} {
		if a := board.away(t, "Done", id); a != nil {
			t.Fatalf("%s still away: %+v", id, a)
		}
	}
	if code := web.do(http.MethodPost, "/api/tasks/"+done+"/return", "", &refused); code != http.StatusConflict {
		t.Fatalf("returning a task at home: %d %+v", code, refused)
	}
}
