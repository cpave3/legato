package handofftest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

func TestAnAwayTaskIsReadOnlyOnTheMainFromEverySurface(t *testing.T) {
	ctx := context.Background()
	worker := StartWorker(t, WorkerOptions{Name: "vm-1"})
	main := NewMain(t, MainOptions{Clock: worker.Clock})
	paired := Pair(t, main, worker)
	taskID, _ := localTask(t, main, "Fix it", "Details.", "")
	if _, err := handOff(t, main, paired.InstanceID, taskID); err != nil {
		t.Fatal(err)
	}
	refused := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, service.ErrTaskAway) || !strings.Contains(err.Error(), "vm-1") {
			t.Fatalf("%s: %v", what, err)
		}
	}

	// The board service, which the web UI and the TUI use.
	refused("title", main.Board.UpdateTaskTitle(ctx, taskID, "New title"))
	refused("description", main.Board.UpdateTaskDescription(ctx, taskID, "New details"))
	refused("move", main.Board.MoveCard(ctx, taskID, "Doing"))
	refused("reorder", main.Board.PlaceCard(ctx, taskID, ""))
	refused("workspace", main.Board.UpdateTaskWorkspace(ctx, taskID, nil))
	refused("delete", main.Board.DeleteTask(ctx, taskID))
	refused("archive", main.Board.ArchiveTask(ctx, taskID))
	// The agent service.
	agents := service.NewAgentService(main.Store, NewFakeTmux(), main.Dir)
	refused("agent start", agents.SpawnAgent(ctx, taskID, 0, 0))
	// The CLI (and agents, whose legato task update reaches it).
	column := "Doing"
	refused("legato task update", cli.TaskUpdateFields(main.Store, taskID, cli.TaskUpdateOptions{Status: &column}))
	// Another handoff.
	_, err := handOff(t, main, paired.InstanceID, taskID)
	refused("a second handoff", err)

	// The web API answers 409 with the refusal.
	srv := server.New(main.Board, agents, NewFakeTmux(), "")
	srv.SetAuthToken("main-web-token")
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodPatch, "/api/tasks/"+taskID, strings.NewReader(`{"title":"New title"}`)),
		httptest.NewRequest(http.MethodPatch, "/api/tasks/"+taskID, strings.NewReader(`{"column":"Doing"}`)),
		httptest.NewRequest(http.MethodDelete, "/api/tasks/"+taskID, nil),
		httptest.NewRequest(http.MethodPost, "/api/tasks/"+taskID+"/agent", strings.NewReader(`{}`)),
	} {
		req.Header.Set("Authorization", "Bearer main-web-token")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "vm-1") {
			t.Fatalf("%s %s: %d %s", req.Method, req.URL.Path, rec.Code, rec.Body.String())
		}
	}

	// Nothing changed, and the board says where the task is.
	detail, err := main.Board.GetCard(ctx, taskID)
	if err != nil || detail.Title != "Fix it" || detail.DescriptionMD != "Details." || detail.Status != "Ready" {
		t.Fatalf("the task changed: %+v, %v", detail, err)
	}
	cards, _ := main.Board.ListCards(ctx, "Ready")
	if len(cards) != 1 || cards[0].Away == nil || cards[0].Away.WorkerName != "vm-1" || cards[0].Away.WorkerInstanceID != paired.InstanceID {
		t.Fatalf("cards %+v", cards)
	}
	// Notes may still be added: they stay on the main.
	if _, err := service.AddTaskNote(ctx, main.Store, taskID, "A thought while it is away.", "agent"); err != nil {
		t.Fatal(err)
	}
}
