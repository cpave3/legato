package handofftest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

// handedOffCard hands a local task from a main named workstation to a
// worker and returns the worker with the card it made.
func handedOffCard(t *testing.T) (*Worker, string) {
	t.Helper()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Name: "workstation", Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix the flaky login test", "", "")
	away, err := handOff(t, main, paired.InstanceID, taskID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Settle()
	return worker, away.WorkerTaskID
}

const belongsToMain = "this task belongs to workstation; return or recall it from there"

func TestTheWorkerRefusesToArchiveOrDeleteAHandedOffCard(t *testing.T) {
	ctx := context.Background()
	worker, card := handedOffCard(t)

	// Moving, editing and notes work as usual.
	if err := worker.Board.MoveCard(ctx, card, "Done"); err != nil {
		t.Fatalf("move: %v", err)
	}
	if err := worker.Board.UpdateTaskTitle(ctx, card, "Fix the login test"); err != nil {
		t.Fatalf("title: %v", err)
	}
	if err := worker.Board.UpdateTaskDescription(ctx, card, "It times out."); err != nil {
		t.Fatalf("description: %v", err)
	}
	if _, err := service.AddTaskNote(ctx, worker.Store, card, "Found it: the cookie expires.", "agent"); err != nil {
		t.Fatalf("note: %v", err)
	}

	// Archiving and deleting are refused, naming the main.
	for name, refused := range map[string]func() error{
		"delete":  func() error { return worker.Board.DeleteTask(ctx, card) },
		"archive": func() error { return worker.Board.ArchiveTask(ctx, card) },
	} {
		err := refused()
		if !errors.Is(err, service.ErrTaskHandedOff) || err.Error() != belongsToMain {
			t.Fatalf("%s: %v, want %q", name, err, belongsToMain)
		}
	}
	// Archive all done leaves it, and archives the rest.
	other, err := worker.Board.CreateTask(ctx, "Done here", "", "Done", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := worker.Board.ArchiveDoneCards(ctx); err != nil || n != 1 {
		t.Fatalf("archive done: %d, %v; want only the worker's own card", n, err)
	}
	if _, err := worker.Board.GetCard(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	done, err := worker.Board.ListCards(ctx, "Done")
	if err != nil || len(done) != 1 || done[0].ID != card {
		t.Fatalf("Done after archiving: %+v, %v", done, err)
	}
}

func TestTheWorkersWebAPIRefusesToArchiveOrDeleteAHandedOffCard(t *testing.T) {
	ctx := context.Background()
	worker, card := handedOffCard(t)
	if err := worker.Board.MoveCard(ctx, card, "Done"); err != nil {
		t.Fatal(err)
	}

	for _, call := range []struct{ method, path string }{
		{http.MethodDelete, "/api/tasks/" + card},
		{http.MethodPost, "/api/tasks/" + card + "/archive"},
	} {
		resp := worker.Do(call.method, call.path, worker.WebToken(), "")
		var body struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusConflict || body.Error != belongsToMain {
			t.Fatalf("%s %s: %d %+v", call.method, call.path, resp.StatusCode, body)
		}
	}
	if _, err := worker.Board.GetCard(ctx, card); err != nil {
		t.Fatalf("the card is gone: %v", err)
	}
	// Moving through the web API works as usual.
	resp := worker.Do(http.MethodPatch, "/api/tasks/"+card, worker.WebToken(), `{"column":"Review"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("move: %d", resp.StatusCode)
	}
}
