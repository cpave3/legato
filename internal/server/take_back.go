package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The ways out of a handoff. On a main: Take back an away task without its
// results (POST /api/tasks/{id}/take-back) and Remove a worker (DELETE
// /api/settings/workers/{instance_id}). On a worker: the main's Drop and
// Leave calls, and Unpair in Settings › Main instances, which closes the
// main's handoffs. See docs/ai/integrations.md § Take back, Drop and Leave.

// MainTakeBacks takes away tasks back without results and removes workers.
// The main's handoff service implements it beside MainHandoffs.
type MainTakeBacks interface {
	TakeBack(ctx context.Context, taskID string) (service.ReturnedTask, error)
	RemoveWorker(ctx context.Context, workerInstanceID string) (service.RemovedWorker, error)
}

// HandoffDrops is a worker's side of Drop, Leave and Unpair. The worker's
// handoff service implements it beside TaskHandoffs.
type HandoffDrops interface {
	Drop(ctx context.Context, mainInstanceID, handoffID string) (service.HandoffDropped, error)
	Leave(ctx context.Context, mainInstanceID string) (service.HandoffLeft, error)
	UnpairMain(ctx context.Context, mainInstanceID string) (int, error)
}

func (s *Server) mainTakeBacks(w http.ResponseWriter) (MainTakeBacks, bool) {
	takeBacks, ok := s.instances.mainHandoffs.(MainTakeBacks)
	if !ok {
		writeJSONError(w, http.StatusServiceUnavailable, "this Legato does not hand tasks off")
	}
	return takeBacks, ok
}

// taskTakeBackHandler takes an away task back without its results: 200
// with {"returned": ...}, else {"error": ...} in the service's words.
func (s *Server) taskTakeBackHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		takeBacks, ok := s.mainTakeBacks(w)
		if !ok {
			return
		}
		id := r.PathValue("id")
		if _, err := s.board.GetCard(r.Context(), id); errors.Is(err, store.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "no task "+id)
			return
		}
		back, err := takeBacks.TakeBack(r.Context(), id)
		if err != nil {
			writeJSONError(w, handOffStatus(err), err.Error())
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"returned": back})
	}
}

// workerRemoveHandler removes a worker: 200 with {"removed": ...}.
func (s *Server) workerRemoveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		takeBacks, ok := s.mainTakeBacks(w)
		if !ok {
			return
		}
		removed, err := takeBacks.RemoveWorker(r.Context(), r.PathValue("instance_id"))
		if errors.Is(err, service.ErrWorkerNotPaired) {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}
		if err != nil {
			writeJSONError(w, handOffStatus(err), err.Error())
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"removed": removed})
	}
}

func (s *Server) handoffDrops(w http.ResponseWriter) (HandoffDrops, bool) {
	drops, ok := s.instances.handoffs.(HandoffDrops)
	if !ok {
		writeIntegrationError(w, http.StatusServiceUnavailable, "unavailable", "this worker does not take handoffs")
	}
	return drops, ok
}

// taskHandoffDropHandler is a main's Drop of one of its handoffs; safe to
// repeat.
func (s *Server) taskHandoffDropHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		main, ok := requireHandoffMain(w, r)
		if !ok {
			return
		}
		drops, ok := s.handoffDrops(w)
		if !ok {
			return
		}
		dropped, err := drops.Drop(r.Context(), main.InstanceID, r.PathValue("handoff_id"))
		if err != nil {
			writeHandoffError(w, err)
			return
		}
		writeJSONValue(w, http.StatusOK, dropped)
	}
}

// handoffLeaveHandler is a main's Leave: it unpairs itself, and its token
// stops working.
func (s *Server) handoffLeaveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		main, ok := requireHandoffMain(w, r)
		if !ok {
			return
		}
		drops, ok := s.handoffDrops(w)
		if !ok {
			return
		}
		left, err := drops.Leave(r.Context(), main.InstanceID)
		if err != nil {
			writeHandoffError(w, err)
			return
		}
		writeJSONValue(w, http.StatusOK, left)
	}
}
