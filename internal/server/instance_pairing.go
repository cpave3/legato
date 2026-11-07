package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// Pairing a main instance with this worker, and the worker as a main sees
// it. A main pairs by phrase like a phone (POST /api/pairing with
// {"purpose":"main"}), and gets a token that reaches only
// /api/integrations/handoff/. See docs/ai/integrations.md § Main instances
// and workers.

// MainInstances are the main instances paired with this worker.
type MainInstances interface {
	ListMains(ctx context.Context) ([]service.PairedMain, error)
	RevokeMain(ctx context.Context, instanceID string) error
}

// Workers are the workers this main instance paired with.
type Workers interface {
	List(ctx context.Context) ([]service.Worker, error)
	Pair(ctx context.Context, req service.WorkerPairRequest) (service.Worker, error)
	Defaults(ctx context.Context) (service.WorkerDefaults, error)
}

type instanceDeps struct {
	mains      MainInstances
	workers    Workers
	instanceID func(context.Context) (string, error)
	sandbox    func() bool
	// reviewPasses is the review passes advertisement, nil when off.
	reviewPasses func() *service.ReviewPassesAdvert
	handoffs     TaskHandoffs
	// mainHandoffs is this Legato as a main: handing its tasks off.
	mainHandoffs MainHandoffs
	// reauthEvery is how often an Events connection checks its main's
	// token (SetHandoffReauthEvery).
	reauthEvery time.Duration
	// streams are the open Events connections, closed when the server
	// shuts down (a hijacked connection outlives Shutdown otherwise).
	streams handoffStreams
}

// SetMainInstances serves Settings › Main instances (/api/settings/mains).
func (s *Server) SetMainInstances(m MainInstances) { s.instances.mains = m }

// SetWorkers serves Settings › Workers (/api/settings/workers).
func (s *Server) SetWorkers(w Workers) { s.instances.workers = w }

// SetInstanceID tells the server this Legato's instance ID, which the
// handoff routes answer with.
func (s *Server) SetInstanceID(id func(context.Context) (string, error)) { s.instances.instanceID = id }

// SetReviewPasses tells /api/meta this worker's review passes; advert
// returns nil while they are off.
func (s *Server) SetReviewPasses(advert func() *service.ReviewPassesAdvert) {
	s.instances.reviewPasses = advert
}

// SetSandboxAvailable tells /api/meta whether agents can run in Legato's
// sandbox here, which a main checks before it hands work to this worker.
func (s *Server) SetSandboxAvailable(available func() bool) { s.instances.sandbox = available }

// HandoffMeta is what /api/meta says of handoff. HandoffPairing is 1 when
// a main instance can pair with this server as a worker; Handoff is the
// handoff protocol version it speaks, and Sandbox whether its agents can
// run in Legato's sandbox. A main checks them before it pairs or hands off
// ("update the worker").
type HandoffMeta struct {
	HandoffPairing int   `json:"handoff_pairing,omitempty"`
	Handoff        int   `json:"handoff,omitempty"`
	Sandbox        *bool `json:"sandbox,omitempty"`
	// ReviewPasses is set when this worker runs review passes on
	// make-a-change handoffs: its defaults, and what a main may choose.
	ReviewPasses *service.ReviewPassesAdvert `json:"review_passes,omitempty"`
}

// handoffMeta adds to /api/meta what a main checks before it pairs or hands
// off: that this server pairs mains, its handoff protocol version and
// whether it has the sandbox.
func (s *Server) handoffMeta(meta *MobileMetaResponse) {
	if s.pairing == nil || !s.pairing.MainsAllowed() {
		return
	}
	if s.instances.reviewPasses != nil {
		meta.ReviewPasses = s.instances.reviewPasses()
	}
	meta.HandoffPairing = 1
	meta.Handoff = service.HandoffProtocolVersion
	available := s.instances.sandbox != nil && s.instances.sandbox()
	meta.Sandbox = &available
}

// handoffMain is the paired main instance a request acts for, or false. Only
// a scoped token of kind main with the handoff scope is one: the full web
// token is no main. Every handoff handler starts here and acts only on that
// main's records (answering not_found for another's).
func handoffMain(r *http.Request) (service.IntegrationIdentity, bool) {
	id, ok := integrationFrom(r.Context())
	if !ok || id.Kind != service.IntegrationKindMain || !id.Has(service.ScopeHandoff) || id.InstanceID == "" {
		return service.IntegrationIdentity{}, false
	}
	return id, true
}

// requireHandoffMain is handoffMain for a handler: it answers 403
// not_a_main itself when the request is not a paired main's.
func requireHandoffMain(w http.ResponseWriter, r *http.Request) (service.IntegrationIdentity, bool) {
	id, ok := handoffMain(r)
	if !ok {
		writeIntegrationError(w, http.StatusForbidden, "not_a_main", "only a paired main instance's handoff token reaches this route")
	}
	return id, ok
}

func (s *Server) registerInstanceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/integrations/handoff/pairing", s.handoffPairingHandler())
	mux.HandleFunc("POST "+service.HandoffCallPath, s.taskHandoffHandler())
	mux.HandleFunc("GET "+service.HandoffCallPath+"/{handoff_id}", s.taskHandoffGetHandler())
	mux.HandleFunc("POST "+service.HandoffCallPath+"/{handoff_id}/return", s.taskHandoffReturnHandler(false))
	mux.HandleFunc("POST "+service.HandoffCallPath+"/{handoff_id}/archive", s.taskHandoffReturnHandler(true))
	mux.HandleFunc("GET "+service.HandoffPullPath, s.handoffPullHandler())
	mux.HandleFunc("GET "+service.HandoffEventsPath, s.handoffEventsHandler())
	mux.HandleFunc("GET /api/settings/mains", s.mainsListHandler())
	mux.HandleFunc("DELETE /api/settings/mains/{instance_id}", s.mainsUnpairHandler())
	mux.HandleFunc("GET /api/settings/workers", s.workersListHandler())
	mux.HandleFunc("POST /api/tasks/{id}/handoff", s.taskHandOffHandler())
	mux.HandleFunc("POST /api/tasks/{id}/return", s.taskReturnHandler(false))
	mux.HandleFunc("POST /api/tasks/{id}/recall", s.taskReturnHandler(true))
	mux.HandleFunc("POST /api/workers/{instance_id}/return-done", s.workerReturnDoneHandler())
	mux.HandleFunc("POST /api/settings/workers", s.workersPairHandler())
	mux.HandleFunc("POST "+service.HandoffCallPath+"/{handoff_id}/drop", s.taskHandoffDropHandler())
	mux.HandleFunc("POST "+service.HandoffLeavePath, s.handoffLeaveHandler())
	mux.HandleFunc("POST /api/tasks/{id}/take-back", s.taskTakeBackHandler())
	mux.HandleFunc("DELETE /api/settings/workers/{instance_id}", s.workerRemoveHandler())
}

// handoffPairingHandler answers a main with the pairing as this worker sees
// it: the worker's instance ID and the main's, so each side can check the
// other's.
func (s *Server) handoffPairingHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		main, ok := requireHandoffMain(w, r)
		if !ok {
			return
		}
		worker := ""
		if s.instances.instanceID != nil {
			id, err := s.instances.instanceID(r.Context())
			if err != nil {
				writeIntegrationError(w, http.StatusInternalServerError, "internal", err.Error())
				return
			}
			worker = id
		}
		writeJSONValue(w, http.StatusOK, map[string]any{
			"worker_instance_id": worker,
			"main_instance_id":   main.InstanceID,
			"main_name":          main.Name,
			"handoff":            service.HandoffProtocolVersion,
		})
	}
}

func (s *Server) mainsListHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.instances.mains == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "this server does not pair main instances")
			return
		}
		mains, err := s.instances.mains.ListMains(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if mains == nil {
			mains = []service.PairedMain{}
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"mains": mains})
	}
}

func (s *Server) mainsUnpairHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.instances.mains == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "this server does not pair main instances")
			return
		}
		// A worker that takes handoffs also closes the main's handoffs,
		// leaving their cards here.
		var err error
		if drops, ok := s.instances.handoffs.(HandoffDrops); ok {
			_, err = drops.UnpairMain(r.Context(), r.PathValue("instance_id"))
		} else {
			err = s.instances.mains.RevokeMain(r.Context(), r.PathValue("instance_id"))
		}
		if errors.Is(err, service.ErrMainNotPaired) {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func (s *Server) workersListHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.instances.workers == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "this server does not hand off to workers")
			return
		}
		workers, err := s.instances.workers.List(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if workers == nil {
			workers = []service.Worker{}
		}
		defaults, err := s.instances.workers.Defaults(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"workers": workers, "instance_id": defaults.InstanceID, "main_name": defaults.MainName})
	}
}

func (s *Server) workersPairHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.instances.workers == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "this server does not hand off to workers")
			return
		}
		var req service.WorkerPairRequest
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "send the worker's address, the phrase and this main's name")
			return
		}
		worker, err := s.instances.workers.Pair(r.Context(), req)
		if err != nil {
			writeJSONError(w, workerPairStatus(err), err.Error())
			return
		}
		writeJSONValue(w, http.StatusOK, worker)
	}
}

// workerPairStatus is the HTTP status of a failed Add a worker.
func workerPairStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrWorkerInput):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrWorkerTooOld), errors.Is(err, service.ErrWorkerRefused):
		return http.StatusConflict
	case errors.Is(err, service.ErrWorkerUnreachable):
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}
