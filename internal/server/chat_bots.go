package server

import (
	"errors"
	"net/http"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// ChatBots is the chat bots' supervisor (service.ChatBots): their status,
// which never holds a token, and a reconnect.
type ChatBots interface {
	Statuses() []service.ChatBotStatus
	Reconnect(name string) error
	Known(name string) (configured, enabled bool)
}

// SetChatBots serves the chat bots' status, and checks the bots workflows
// name against them.
func (s *Server) SetChatBots(b ChatBots) { s.chatBots = b }

func (s *Server) registerChatBotRoutes(mux *routeMux) {
	mux.HandleFunc("GET /api/settings/integrations/chat-bots", s.chatBotsHandler())
	mux.HandleFunc("POST /api/settings/integrations/chat-bots/{name}/reconnect", s.chatBotReconnectHandler())
}

func (s *Server) chatBotsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.chatBots == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "chat bots are not available on this server")
			return
		}
		writeJSONValue(w, http.StatusOK, map[string]any{"bots": s.chatBots.Statuses()})
	}
}

func (s *Server) chatBotReconnectHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.chatBots == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "chat bots are not available on this server")
			return
		}
		err := s.chatBots.Reconnect(r.PathValue("name"))
		switch {
		case errors.Is(err, service.ErrChatBotUnknown):
			writeJSONError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, service.ErrChatBotNotConnected):
			writeJSONError(w, http.StatusConflict, err.Error())
		case err != nil:
			writeJSONError(w, http.StatusInternalServerError, err.Error())
		default:
			writeJSONValue(w, http.StatusOK, map[string]any{"bots": s.chatBots.Statuses()})
		}
	}
}

// chatBotDiagnostics are the problems with the bots a workflow names, on
// this Legato; none without chat bots.
func (s *Server) chatBotDiagnostics(data []byte, definition *workflowengine.Definition) []workflowengine.Diagnostic {
	if s.chatBots == nil || definition == nil {
		return nil
	}
	names := map[string]bool{}
	for _, status := range s.chatBots.Statuses() {
		names[status.Name] = true
	}
	return workflowengine.ChatBotDiagnostics(data, definition, func(name string) (workflowengine.ChatBotState, bool) {
		if !names[name] {
			return workflowengine.ChatBotState{}, false
		}
		configured, enabled := s.chatBots.Known(name)
		return workflowengine.ChatBotState{Configured: configured, Enabled: enabled}, true
	})
}
