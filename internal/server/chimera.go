package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func (s *Server) chimeraDiscoverHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.agents == nil {
			http.Error(w, "agent service not available", http.StatusServiceUnavailable)
			return
		}
		taskID := r.PathValue("task_id")
		session, err := s.agents.GetAgentSession(r.Context(), taskID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.Error(w, "active agent not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if session.AgentKind != "chimera" || session.Status != "running" {
			http.Error(w, "native upgrade requires an active Chimera agent", http.StatusConflict)
			return
		}
		if session.ChimeraEndpoint != nil || session.ChimeraSessionID != nil {
			http.Error(w, "Chimera agent is already native", http.StatusConflict)
			return
		}
		if err := s.agents.DiscoverChimera(r.Context(), taskID); err != nil {
			switch {
			case errors.Is(err, service.ErrChimeraDiscoveryNotFound):
				http.Error(w, "no live Chimera instance for this task was found", http.StatusNotFound)
			case errors.Is(err, service.ErrChimeraDiscoveryAmbiguous):
				http.Error(w, "multiple live Chimera instances match this task", http.StatusConflict)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"task_id": taskID, "upgraded": true})
	}
}

func (s *Server) chimeraDowngradeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.agents == nil {
			http.Error(w, "agent service not available", http.StatusServiceUnavailable)
			return
		}
		taskID := r.PathValue("task_id")
		agentSession, err := s.agents.GetAgentSession(r.Context(), taskID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.Error(w, "active agent not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if agentSession.ChimeraEndpoint == nil || agentSession.ChimeraSessionID == nil {
			http.Error(w, "agent is already using tmux", http.StatusConflict)
			return
		}
		invalidator, ok := s.agents.(service.ChimeraBindingInvalidator)
		if !ok {
			http.Error(w, "agent service does not support Chimera downgrade", http.StatusNotImplemented)
			return
		}
		cleared, err := invalidator.InvalidateChimeraBinding(r.Context(), agentSession.ID, *agentSession.ChimeraSessionID, *agentSession.ChimeraEndpoint)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !cleared {
			http.Error(w, "Chimera binding changed before downgrade", http.StatusConflict)
			return
		}
		s.NotifyAgentsChanged()
		s.writeJSON(w, http.StatusOK, map[string]any{"task_id": taskID, "downgraded": true})
	}
}

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

		if s.agents == nil {
			http.Error(w, "agent service not available", http.StatusServiceUnavailable)
			return
		}
		agentSession, err := s.agents.GetAgentSession(r.Context(), taskID)
		if err != nil || agentSession.ChimeraEndpoint == nil || agentSession.ChimeraSessionID == nil {
			if err == nil {
				err = errors.New("no chimera endpoint discovered for " + taskID)
			}
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		endpoint := *agentSession.ChimeraEndpoint
		sessionID := *agentSession.ChimeraSessionID

		client := chimera.NewClient(endpoint)
		snap, err := client.GetSession(r.Context(), sessionID)
		if err != nil {
			if r.URL.Query().Get("initial") == "true" {
				invalidator, ok := s.agents.(service.ChimeraBindingInvalidator)
				if !ok {
					http.Error(w, "chimera: "+err.Error()+"; invalidating binding: unsupported agent service", http.StatusBadGateway)
					return
				}
				cleared, clearErr := invalidator.InvalidateChimeraBinding(r.Context(), agentSession.ID, sessionID, endpoint)
				if clearErr != nil {
					http.Error(w, "chimera: "+err.Error()+"; invalidating binding: "+clearErr.Error(), http.StatusBadGateway)
					return
				}
				if cleared {
					s.NotifyAgentsChanged()
				}
			}
			http.Error(w, "chimera: "+err.Error(), http.StatusBadGateway)
			return
		}
		sessionContext, err := client.GetSessionContext(r.Context(), sessionID)
		if err != nil {
			http.Error(w, "chimera context: "+err.Error(), http.StatusBadGateway)
			return
		}
		snap.ContextWindow = sessionContext.ContextWindow
		if sessionContext.LastPromptTokens != nil {
			snap.UsedContextTokens = *sessionContext.LastPromptTokens
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
			RequestID string  `json:"request_id"`
			Decision  string  `json:"decision"`
			Feedback  *string `json:"feedback,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.RequestID == "" || (req.Decision != "allow" && req.Decision != "deny") {
			http.Error(w, "request_id and a valid decision required", http.StatusBadRequest)
			return
		}
		feedback := ""
		if req.Feedback != nil {
			feedback = strings.TrimSpace(*req.Feedback)
			if feedback == "" || req.Decision != "deny" {
				http.Error(w, "non-empty feedback is only valid for deny", http.StatusBadRequest)
				return
			}
		}

		endpoint, sessionID, err := s.getChimeraEndpoint(r, taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		client := chimera.NewClient(endpoint)
		action := chimera.PermissionAction{Decision: req.Decision, Feedback: feedback}
		if err := client.ResolvePermission(r.Context(), sessionID, req.RequestID, action); err != nil {
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

// chimeraModelHandler sets the model for the session.
func (s *Server) chimeraModelHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		taskID := r.PathValue("task_id")
		endpoint, sessionID, err := s.getChimeraEndpoint(r, taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		var body json.RawMessage
		if r.Method == http.MethodPost {
			var req map[string]json.RawMessage
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil || req == nil {
				http.Error(w, "invalid model settings JSON", http.StatusBadRequest)
				return
			}
			if raw, ok := req["model"]; ok {
				var ref string
				if string(raw) != "null" {
					if err := json.Unmarshal(raw, &ref); err != nil {
						http.Error(w, "model must be a string or null", http.StatusBadRequest)
						return
					}
					if ref == "" || ref == "default" || ref == "reset" {
						req["model"] = json.RawMessage(`null`)
					}
				}
			}
			body, _ = json.Marshal(req)
		}
		client := chimera.NewClient(endpoint)
		payload, status, err := client.ModelSettings(r.Context(), sessionID, r.Method, body)
		if err != nil {
			http.Error(w, "chimera: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(payload)
	}
}

// chimeraCommandsHandler returns the slash commands available for the session.
func (s *Server) chimeraCommandsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := r.PathValue("task_id")
		endpoint, sessionID, err := s.getChimeraEndpoint(r, taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		client := chimera.NewClient(endpoint)
		commands, err := client.ListCommands(r.Context(), sessionID)
		if err != nil {
			http.Error(w, "chimera: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(commands)
	}
}

// chimeraSkillsHandler returns the skills available for the session.
func (s *Server) chimeraSkillsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		taskID := r.PathValue("task_id")
		endpoint, sessionID, err := s.getChimeraEndpoint(r, taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		client := chimera.NewClient(endpoint)
		skills, err := client.ListSkills(r.Context(), sessionID)
		if err != nil {
			http.Error(w, "chimera: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(skills)
	}
}

// chimeraModelsHandler returns the models available for the session.
func (s *Server) chimeraModelsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		taskID := r.PathValue("task_id")
		endpoint, sessionID, err := s.getChimeraEndpoint(r, taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		client := chimera.NewClient(endpoint)
		models, err := client.ListModels(r.Context(), sessionID)
		if err != nil {
			http.Error(w, "chimera: "+err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models)
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
