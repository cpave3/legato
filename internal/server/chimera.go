package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/cpave3/legato/internal/engine/chimera"
)

// chimeraSnapshotHandler proxies GET /v1/sessions/:id from the Chimera
// server to the browser. The endpoint and session ID are resolved from
// the agent session row (populated by lockfile discovery), not exposed
// directly to the client.
func (s *Server) chimeraSnapshotHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		taskID := r.PathValue("task_id")
		if taskID == "" {
			// Fall back to path split for older Go versions.
			taskID = strings.TrimPrefix(r.URL.Path, "/api/agents/")
			taskID = strings.TrimSuffix(taskID, "/chimera/snapshot")
		}
		if taskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}

		endpoint, sessionID, err := s.getChimeraEndpoint(r, taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		client := chimera.NewClient(endpoint)
		snap, err := client.GetSession(r.Context(), sessionID)
		if err != nil {
			http.Error(w, "chimera: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snap)
	}
}

// chimeraSendHandler sends a message to the Chimera session via the HTTP API.
func (s *Server) chimeraSendHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		taskID := r.PathValue("task_id")
		if taskID == "" {
			taskID = strings.TrimPrefix(r.URL.Path, "/api/agents/")
			taskID = strings.TrimSuffix(taskID, "/chimera/send")
		}
		if taskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}

		var req struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Content == "" {
			http.Error(w, "content required", http.StatusBadRequest)
			return
		}

		endpoint, sessionID, err := s.getChimeraEndpoint(r, taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		client := chimera.NewClient(endpoint)
		if err := client.PostMessage(r.Context(), sessionID, req.Content); err != nil {
			if err == chimera.ErrBusy {
				http.Error(w, `{"error":"busy"}`, http.StatusConflict)
				return
			}
			http.Error(w, "chimera: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

// chimeraInterruptHandler sends an interrupt to the Chimera session.
func (s *Server) chimeraInterruptHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		taskID := r.PathValue("task_id")
		if taskID == "" {
			taskID = strings.TrimPrefix(r.URL.Path, "/api/agents/")
			taskID = strings.TrimSuffix(taskID, "/chimera/interrupt")
		}
		if taskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}

		endpoint, sessionID, err := s.getChimeraEndpoint(r, taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		client := chimera.NewClient(endpoint)
		if err := client.Interrupt(r.Context(), sessionID); err != nil {
			http.Error(w, "chimera: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// chimeraPermissionHandler resolves a pending permission request.
func (s *Server) chimeraPermissionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		taskID := r.PathValue("task_id")
		if taskID == "" {
			taskID = strings.TrimPrefix(r.URL.Path, "/api/agents/")
			taskID = strings.TrimSuffix(taskID, "/chimera/permission")
		}
		if taskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}

		var req struct {
			RequestID     string `json:"request_id"`
			Decision      string `json:"decision"`
			RememberScope string `json:"remember_scope"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.RequestID == "" || req.Decision == "" {
			http.Error(w, "request_id and decision required", http.StatusBadRequest)
			return
		}

		endpoint, sessionID, err := s.getChimeraEndpoint(r, taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		client := chimera.NewClient(endpoint)
		if err := client.ResolvePermission(r.Context(), sessionID, req.RequestID, req.Decision, req.RememberScope); err != nil {
			http.Error(w, "chimera: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// chimeraModesHandler returns the available modes for the session.
func (s *Server) chimeraModesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := r.PathValue("task_id")
		if taskID == "" {
			taskID = strings.TrimPrefix(r.URL.Path, "/api/agents/")
			taskID = strings.TrimSuffix(taskID, "/chimera/modes")
		}
		endpoint, sessionID, err := s.getChimeraEndpoint(r, taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		client := chimera.NewClient(endpoint)
		if r.Method == http.MethodGet {
			modes, err := client.ListModes(r.Context(), sessionID)
			if err != nil {
				http.Error(w, "chimera: "+err.Error(), http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(modes)
		} else if r.Method == http.MethodPost {
			var req struct {
				Mode string `json:"mode"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			if err := client.SetMode(r.Context(), sessionID, req.Mode); err != nil {
				http.Error(w, "chimera: "+err.Error(), http.StatusBadGateway)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// getChimeraEndpoint resolves the private Chimera endpoint URL and session ID
// for the given task from the agent service. Returns an error if the agent
// doesn't have a discovered Chimera endpoint.
func (s *Server) getChimeraEndpoint(r *http.Request, taskID string) (endpoint, sessionID string, err error) {
	if s.agents == nil {
		return "", "", &chimeraError{msg: "agent service not available", code: http.StatusServiceUnavailable}
	}
	if !s.agents.HasChimeraEndpoint(r.Context(), taskID) {
		return "", "", &chimeraError{msg: "no chimera endpoint discovered for " + taskID, code: http.StatusNotFound}
	}
	// We need the raw session row. Use the store directly via the service
	// interface — but since AgentService doesn't expose the store, we
	// route through the agent service's internal access. For now, use the
	// interaction methods which already resolve the endpoint internally.
	// The snapshot/send/interrupt/permission handlers all use this path.
	//
	// We need to get the endpoint from somewhere. The AgentService.SendMessage
	// already resolves it internally via the ChimeraInteractionAdapter, but
	// for the snapshot proxy we need the raw endpoint URL. Let's add a method.
	session, err := s.agents.GetAgentSession(r.Context(), taskID)
	if err != nil {
		return "", "", err
	}
	if session.ChimeraEndpoint == nil || session.ChimeraSessionID == nil {
		return "", "", &chimeraError{msg: "no chimera endpoint", code: http.StatusNotFound}
	}
	return *session.ChimeraEndpoint, *session.ChimeraSessionID, nil
}

type chimeraError struct {
	msg  string
	code int
}

func (e *chimeraError) Error() string { return e.msg }
func (e *chimeraError) HTTPCode() int { return e.code }
