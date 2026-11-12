package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// SetGroupDefaults sets the groups that always exist (groups.defaults),
// which GET /api/agent-groups lists first.
func (s *Server) SetGroupDefaults(defaults []string) {
	s.groupDefaults = nil
	for _, group := range defaults {
		if group = strings.TrimSpace(group); group != "" {
			s.groupDefaults = append(s.groupDefaults, group)
		}
	}
}

func (s *Server) taskGroups(w http.ResponseWriter) (service.TaskGroups, bool) {
	groups, ok := s.agents.(service.TaskGroups)
	if !ok || s.agents == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "agent groups are not available")
		return nil, false
	}
	return groups, true
}

// groupsChanged tells the web clients and the other Legato processes (a TUI)
// that tasks moved between groups.
func (s *Server) groupsChanged(taskID string) {
	s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})
	if s.notifyTaskChanged != nil {
		s.notifyTaskChanged(taskID)
	}
}

// agentGroupHandler is PUT /api/agents/{task_id}/group: {"group": "name"}
// puts the agent's task in a group, {"group": null} or "" takes it out.
func (s *Server) agentGroupHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		groups, ok := s.taskGroups(w)
		if !ok {
			return
		}
		var req struct {
			Group *string `json:"group"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		id := r.PathValue("task_id")
		if err := groups.SetTaskGroup(r.Context(), id, req.Group); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeJSONError(w, http.StatusNotFound, "no task "+id)
				return
			}
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		taskID, _ := service.SplitAgentID(id)
		s.groupsChanged(taskID)
		group := ""
		if req.Group != nil {
			group = strings.TrimSpace(*req.Group)
		}
		writeJSONValue(w, http.StatusOK, map[string]string{"group": group})
	}
}

// agentGroupsHandler is GET /api/agent-groups: the default groups, and every
// group, the defaults first and then those in use in name order.
func (s *Server) agentGroupsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		groups, ok := s.taskGroups(w)
		if !ok {
			return
		}
		inUse, err := groups.ListTaskGroups(r.Context())
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defaults := append([]string{}, s.groupDefaults...)
		all := append([]string{}, defaults...)
		seen := map[string]bool{}
		for _, group := range defaults {
			seen[group] = true
		}
		for _, group := range inUse {
			if !seen[group] {
				seen[group] = true
				all = append(all, group)
			}
		}
		writeJSONValue(w, http.StatusOK, map[string][]string{"defaults": defaults, "groups": all})
	}
}

// agentGroupNameHandler is PUT /api/agent-groups/{name} ({"name": "new"}),
// which renames a group, moving its tasks, and DELETE, which takes every task
// out of it.
func (s *Server) agentGroupNameHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groups, ok := s.taskGroups(w)
		if !ok {
			return
		}
		name := strings.TrimSpace(r.PathValue("name"))
		if name == "" {
			writeJSONError(w, http.StatusBadRequest, "a group name is required")
			return
		}
		to := ""
		switch r.Method {
		case http.MethodPut:
			var req struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			if to = strings.TrimSpace(req.Name); to == "" {
				writeJSONError(w, http.StatusBadRequest, "the new name is empty")
				return
			}
		case http.MethodDelete:
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		moved, err := groups.RenameTaskGroup(context.WithoutCancel(r.Context()), name, to)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.groupsChanged("")
		writeJSONValue(w, http.StatusOK, map[string]any{"group": to, "moved": moved})
	}
}
