package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// Handing tasks off from this main instance's web board: the Hand off form
// (POST /api/tasks/{id}/handoff) and the workers the board's Away lane
// groups away tasks under. See docs/ai/web-ui.md § Handing off a task.

// MainHandoffs hands this main's tasks to the workers it paired with.
type MainHandoffs interface {
	HandOff(ctx context.Context, req service.TaskHandoffRequest) (service.AwayTask, error)
}

// SetMainHandoffs lets the web board hand tasks off to workers. Without it
// the route answers 503.
func (s *Server) SetMainHandoffs(h MainHandoffs) { s.instances.mainHandoffs = h }

// handOffRequest is the Hand off form: the worker's instance ID, the
// purpose (make-change or find-out), where the work starts (new-branch when
// empty, or pushed-branch with the branch's name), the repository
// (owner/name) and the agent tool (empty for the worker's default).
type handOffRequest struct {
	Worker    string `json:"worker"`
	Purpose   string `json:"purpose"`
	Kind      string `json:"kind"`
	Branch    string `json:"branch"`
	Repo      string `json:"repo"`
	AgentKind string `json:"agent_kind"`
}

// taskHandOffHandler hands a task to a worker: 201 with {"away": ...} as the
// board shows it, else {"error": ...} in the handoff service's words.
func (s *Server) taskHandOffHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.instances.mainHandoffs == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "this Legato does not hand tasks off")
			return
		}
		var body handOffRequest
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "send the worker, the purpose, the repository and the agent tool")
			return
		}
		id := r.PathValue("id")
		if _, err := s.board.GetCard(r.Context(), id); errors.Is(err, store.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "no task "+id)
			return
		}
		away, err := s.instances.mainHandoffs.HandOff(r.Context(), service.TaskHandoffRequest{
			TaskID: id, Worker: body.Worker, Purpose: body.Purpose, Kind: body.Kind, Branch: body.Branch, Repo: body.Repo, AgentKind: body.AgentKind,
		})
		if err != nil {
			writeJSONError(w, handOffStatus(err), err.Error())
			return
		}
		writeJSONValue(w, http.StatusCreated, map[string]any{"away": awayResponse(&away)})
	}
}

// handOffStatus is the HTTP status of a refused handoff.
func handOffStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrHandoffInput), errors.Is(err, service.ErrWorkerNotPaired), errors.Is(err, service.ErrWorkerInput):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrTaskAway), errors.Is(err, service.ErrHandoffAgentRunning), errors.Is(err, service.ErrWorkerTooOld),
		errors.Is(err, service.ErrReturnRefused):
		return http.StatusConflict
	case errors.Is(err, service.ErrWorkerRefused):
		return http.StatusUnprocessableEntity
	case errors.Is(err, service.ErrWorkerUnreachable):
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}

// awayResponse is an away task as the board and the Hand off route show it.
func awayResponse(a *service.AwayTask) *AwayResponse {
	if a == nil {
		return nil
	}
	return &AwayResponse{WorkerInstanceID: a.WorkerInstanceID, Worker: a.WorkerName, HandoffID: a.HandoffID, WorkerTaskID: a.WorkerTaskID,
		Purpose: a.Purpose, Kind: a.Kind, Repo: a.Repo, Branch: a.Branch, AgentKind: a.AgentKind, LeftColumn: a.LeftColumn,
		HandedOffAt: a.HandedOffAt, State: a.State, Failure: a.Failure, StateAt: a.StateAt, URL: a.WorkerURL,
		Activity: a.Activity, WorkerColumn: a.WorkerColumn, WorkerTitle: a.WorkerTitle, Branches: a.Branches, Lost: a.Lost, LostAt: a.LostAt,
		WorkerUnreachableSince: a.WorkerUnreachableSince, WorkerRefusedAt: a.WorkerRefusedAt, View: a.View, Review: a.Review}
}

// boardWorkers are the workers this main is paired with, for the board's
// Away lane; empty (never null) when none or when this is no main.
func (s *Server) boardWorkers(ctx context.Context) []BoardWorkerResponse {
	out := []BoardWorkerResponse{}
	if s.instances.workers == nil {
		return out
	}
	workers, err := s.instances.workers.List(ctx)
	if err != nil {
		return out
	}
	for _, w := range workers {
		out = append(out, BoardWorkerResponse{InstanceID: w.InstanceID, Name: w.Name, Address: w.Address, LastPullAt: w.LastPullAt,
			UnreachableSince: w.UnreachableSince, RefusedAt: w.RefusedAt})
	}
	return out
}

// MainReturns brings this main's away tasks home: Return a task done on its
// worker, Recall one in any state, Return all done on one worker. The
// main's handoff service implements it beside MainHandoffs.
type MainReturns interface {
	Return(ctx context.Context, taskID string) (service.ReturnedTask, error)
	Recall(ctx context.Context, taskID string) (service.ReturnedTask, error)
	ReturnAllDone(ctx context.Context, workerInstanceID string) ([]service.ReturnedTask, error)
}

func (s *Server) mainReturns(w http.ResponseWriter) (MainReturns, bool) {
	returns, ok := s.instances.mainHandoffs.(MainReturns)
	if !ok {
		writeJSONError(w, http.StatusServiceUnavailable, "this Legato does not hand tasks off")
	}
	return returns, ok
}

// taskReturnHandler is Return (recall false) or Recall of an away task:
// 200 with {"returned": ...}, else {"error": ...} in the service's words.
func (s *Server) taskReturnHandler(recall bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		returns, ok := s.mainReturns(w)
		if !ok {
			return
		}
		id := r.PathValue("id")
		if _, err := s.board.GetCard(r.Context(), id); errors.Is(err, store.ErrNotFound) {
			writeJSONError(w, http.StatusNotFound, "no task "+id)
			return
		}
		bring := returns.Return
		if recall {
			bring = returns.Recall
		}
		back, err := bring(r.Context(), id)
		if err != nil {
			writeJSONError(w, handOffStatus(err), err.Error())
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"returned": back})
	}
}

// workerReturnDoneHandler is a worker group's Return all done: 200 with
// {"returned": [...]} and, when some could not come back, {"error": ...};
// an error status when none came back and one failed.
func (s *Server) workerReturnDoneHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		returns, ok := s.mainReturns(w)
		if !ok {
			return
		}
		back, err := returns.ReturnAllDone(r.Context(), r.PathValue("instance_id"))
		if err != nil && len(back) == 0 {
			writeJSONError(w, handOffStatus(err), err.Error())
			return
		}
		body := map[string]any{"returned": back}
		if err != nil {
			body["error"] = err.Error()
		}
		writeJSONValue(w, http.StatusOK, body)
	}
}
