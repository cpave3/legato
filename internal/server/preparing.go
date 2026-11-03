package server

import (
	"context"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// What an agent being started is waiting for.
const (
	// StageCloning: its clone is being made.
	StageCloning = "cloning"
	// StageSettingUp: its clone is made and being set up (the repository's
	// .legato.toml or .yggdrasil.toml); the agent tool starts after.
	StageSettingUp = "setting_up"
)

// cloneSetupAwaiter waits for the setup of the clone an agent was just
// started in (service.agentService).
type cloneSetupAwaiter interface {
	AwaitCloneSetup(ctx context.Context, taskID string) error
}

// PreparingAgent is an agent being started in the background: its clone
// is being made, then set up. One that failed stays, with why, until it is
// dismissed.
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
	// Stage is what it waits for: StageCloning, then StageSettingUp.
	Stage string `json:"stage"`
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
	p.Stage = StageCloning
	s.preparing.byTask[p.TaskID] = &p
	s.preparing.mu.Unlock()
	s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})

	go func() {
		err := spawn(context.Background())
		if awaiter, ok := s.agents.(cloneSetupAwaiter); ok && err == nil {
			// Listed until the clone is set up and the agent tool starts.
			s.preparing.mu.Lock()
			if current, ok := s.preparing.byTask[p.TaskID]; ok {
				current.Stage = StageSettingUp
			}
			s.preparing.mu.Unlock()
			s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})
			if setupErr := awaiter.AwaitCloneSetup(context.Background(), p.TaskID); setupErr != nil {
				log.Printf("preparing %s: %v", p.TaskID, setupErr)
			}
		}
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
