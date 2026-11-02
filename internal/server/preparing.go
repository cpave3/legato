package server

import (
	"context"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// PreparingAgent is an agent being started in the background: its clone
// is being made. One that failed stays, with why, until it is dismissed.
type PreparingAgent struct {
	TaskID    string `json:"task_id"`
	Title     string `json:"title"`
	AgentKind string `json:"agent_kind,omitempty"`
	// Repo is the repository cloned for it; CloneID the job clone it
	// joins. Its progress is the repository's copy (repository_cache).
	Repo      string    `json:"repo,omitempty"`
	CloneID   int64     `json:"clone_id,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Error     string    `json:"error,omitempty"`
}

// preparingAgents are the agents being started in the background.
type preparingAgents struct {
	mu     sync.Mutex
	byTask map[string]*PreparingAgent
}

// ephemeralTaskCreator makes an agent's own task before it is started, so
// a spawn in the background can answer with it at once.
type ephemeralTaskCreator interface {
	CreateEphemeralTask(ctx context.Context, title string) (string, error)
}

// startsInBackground says whether a spawn waits for a clone, which can take
// minutes the first time a repository is copied.
func startsInBackground(opts service.AgentSpawnOptions) bool {
	return opts.Repo != "" || opts.Clone != 0
}

// startInBackground starts an agent outside the request, so closing the page
// does not cancel its clone, and lists it as preparing until it runs. It is
// false when the task's agent is being prepared already.
func (s *Server) startInBackground(p PreparingAgent, spawn func(ctx context.Context) error) bool {
	s.preparing.mu.Lock()
	if s.preparing.byTask == nil {
		s.preparing.byTask = map[string]*PreparingAgent{}
	}
	if current, ok := s.preparing.byTask[p.TaskID]; ok && current.Error == "" {
		s.preparing.mu.Unlock()
		return false
	}
	p.StartedAt = time.Now()
	s.preparing.byTask[p.TaskID] = &p
	s.preparing.mu.Unlock()
	s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})

	go func() {
		err := spawn(context.Background())
		s.preparing.mu.Lock()
		if err == nil {
			delete(s.preparing.byTask, p.TaskID)
		} else if current, ok := s.preparing.byTask[p.TaskID]; ok {
			current.Error = err.Error()
		}
		s.preparing.mu.Unlock()
		s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})
	}()
	return true
}

// BroadcastRepositoryCache tells the web UIs how a repository's copy
// stands, for the progress of agents being prepared and Settings.
func (s *Server) BroadcastRepositoryCache(status service.RepositoryCacheStatus) {
	s.hub.Broadcast(WSMessage{Type: MsgRepositoryCache, Cache: &status})
}

// preparingAgentsHandler serves GET /api/preparing-agents: the agents being
// started in the background, the oldest first.
func (s *Server) preparingAgentsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.preparing.mu.Lock()
		list := make([]PreparingAgent, 0, len(s.preparing.byTask))
		for _, p := range s.preparing.byTask {
			list = append(list, *p)
		}
		s.preparing.mu.Unlock()
		sort.Slice(list, func(i, j int) bool { return list[i].StartedAt.Before(list[j].StartedAt) })
		s.writeJSON(w, http.StatusOK, list)
	}
}

// preparingAgentHandler serves DELETE /api/preparing-agents/{task_id}: it
// dismisses one that failed (409 while it is still being prepared).
func (s *Server) preparingAgentHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		taskID := r.PathValue("task_id")
		s.preparing.mu.Lock()
		p, ok := s.preparing.byTask[taskID]
		switch {
		case !ok:
			s.preparing.mu.Unlock()
			s.writeError(w, http.StatusNotFound, "no agent is being prepared for that task")
			return
		case p.Error == "":
			s.preparing.mu.Unlock()
			s.writeError(w, http.StatusConflict, "the agent is still being prepared")
			return
		}
		delete(s.preparing.byTask, taskID)
		s.preparing.mu.Unlock()
		s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})
		w.WriteHeader(http.StatusNoContent)
	}
}
