package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// TaskHandoffs is a worker's side of handoff: the Handoff call a paired main
// sends, and a handoff as that main sees it.
type TaskHandoffs interface {
	Accept(ctx context.Context, main service.HandoffMain, call service.HandoffCall) (service.HandoffAccepted, bool, error)
	Get(ctx context.Context, mainInstanceID, handoffID string) (service.HandoffAccepted, error)
}

// SetTaskHandoffs lets paired mains hand tasks to this worker
// (POST /api/integrations/handoff/tasks). Without it the route answers 503.
func (s *Server) SetTaskHandoffs(h TaskHandoffs) { s.instances.handoffs = h }

// taskHandoffHandler is the Handoff call: a paired main hands this worker a
// task. 201 with the new card, 200 with the card already made for the same
// handoff ID.
func (s *Server) taskHandoffHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		main, ok := requireHandoffMain(w, r)
		if !ok {
			return
		}
		if s.instances.handoffs == nil {
			writeIntegrationError(w, http.StatusServiceUnavailable, "unavailable", "this worker does not take handoffs")
			return
		}
		var call service.HandoffCall
		r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			writeIntegrationError(w, http.StatusBadRequest, "bad_request", "the body is not a handoff")
			return
		}
		accepted, created, err := s.instances.handoffs.Accept(r.Context(), service.HandoffMain{InstanceID: main.InstanceID, Name: main.Name}, call)
		if err != nil {
			writeHandoffError(w, err)
			return
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSONValue(w, status, accepted)
	}
}

// taskHandoffGetHandler is one of the calling main's handoffs; another
// main's is not_found.
func (s *Server) taskHandoffGetHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		main, ok := requireHandoffMain(w, r)
		if !ok {
			return
		}
		if s.instances.handoffs == nil {
			writeIntegrationError(w, http.StatusServiceUnavailable, "unavailable", "this worker does not take handoffs")
			return
		}
		accepted, err := s.instances.handoffs.Get(r.Context(), main.InstanceID, r.PathValue("handoff_id"))
		if err != nil {
			writeHandoffError(w, err)
			return
		}
		writeJSONValue(w, http.StatusOK, accepted)
	}
}

func writeHandoffError(w http.ResponseWriter, err error) {
	var he *service.HandoffError
	if !errors.As(err, &he) {
		writeIntegrationError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	status := http.StatusUnprocessableEntity
	switch he.Code {
	case "bad_request":
		status = http.StatusBadRequest
	case "not_found":
		status = http.StatusNotFound
	case "idempotency_conflict":
		status = http.StatusConflict
	}
	writeIntegrationError(w, status, he.Code, he.Message)
}
