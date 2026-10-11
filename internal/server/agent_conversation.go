package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

const (
	// defaultConversationEntries is how many entries of a conversation are
	// returned when the request does not say, and maxConversationEntries the
	// most it may ask for.
	defaultConversationEntries = 40
	maxConversationEntries     = 100
)

// conversationResponse is the end of a native agent's conversation as the
// web UI's ensemble reads it.
type conversationResponse struct {
	TaskID  string              `json:"task_id"`
	Status  string              `json:"status"`
	Entries []conversationEntry `json:"entries"`
	// Permission and Question are the open request of each kind, or null.
	Permission    *conversationPermission `json:"permission"`
	Question      *conversationQuestion   `json:"question"`
	ContextUsed   int                     `json:"context_used"`
	ContextWindow int                     `json:"context_window"`
}

type conversationEntry struct {
	Kind    string `json:"kind"`
	Text    string `json:"text"`
	Tool    string `json:"tool,omitempty"`
	Command string `json:"command,omitempty"`
	State   string `json:"state,omitempty"`
}

type conversationPermission struct {
	Tool    string `json:"tool"`
	Command string `json:"command,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Plan    string `json:"plan,omitempty"`
}

type conversationQuestion struct {
	Text    string   `json:"text"`
	Options []string `json:"options"`
}

// agentConversationHandler serves GET /api/agents/{task_id}/conversation: the
// newest things a native agent said and did, its open request and how full
// its context is. It is what a snapshot holds, cut to what a glance needs: a
// long session's snapshot is megabytes, and the ensemble asks for every agent
// every few seconds.
//
// An agent driven through its terminal has no conversation to read (409);
// its pane is at /api/agents/{task_id}/preview.
func (s *Server) agentConversationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.agents == nil {
			http.Error(w, "agent service not available", http.StatusServiceUnavailable)
			return
		}
		reader, ok := s.agents.(service.AgentConversationReader)
		if !ok {
			http.Error(w, "agent conversations are not available", http.StatusNotImplemented)
			return
		}
		taskID := r.PathValue("task_id")
		if taskID == "" {
			http.Error(w, "task_id is required", http.StatusBadRequest)
			return
		}
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil || limit < 1 {
			limit = defaultConversationEntries
		}
		limit = min(limit, maxConversationEntries)

		tail, err := reader.ConversationTail(r.Context(), taskID, limit)
		switch {
		case errors.Is(err, service.ErrNoNativeEndpoint):
			http.Error(w, "agent has no native endpoint", http.StatusConflict)
			return
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, "running agent not found", http.StatusNotFound)
			return
		case err != nil:
			http.Error(w, "read agent conversation: "+err.Error(), http.StatusBadGateway)
			return
		}

		response := conversationResponse{
			TaskID:        taskID,
			Status:        tail.Status,
			Entries:       make([]conversationEntry, len(tail.Entries)),
			ContextUsed:   tail.ContextUsed,
			ContextWindow: tail.ContextWindow,
		}
		for i, entry := range tail.Entries {
			response.Entries[i] = conversationEntry{
				Kind: string(entry.Kind), Text: entry.Text, Tool: entry.Tool, Command: entry.Command, State: entry.State,
			}
		}
		if permission := tail.Permission; permission != nil {
			response.Permission = &conversationPermission{
				Tool: permission.Tool, Command: permission.Command, Reason: permission.Reason, Plan: permission.Plan,
			}
		}
		if question := tail.Question; question != nil {
			options := question.Options
			if options == nil {
				options = []string{}
			}
			response.Question = &conversationQuestion{Text: question.Text, Options: options}
		}
		s.writeJSON(w, http.StatusOK, response)
	}
}
