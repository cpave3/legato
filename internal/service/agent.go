package service

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// TmuxManager abstracts tmux operations for testability.
type TmuxManager interface {
	Spawn(name, workDir string) error
	Kill(name string) error
	Capture(name string) (string, error)
	Attach(name string) *exec.Cmd
	ListSessions() ([]string, error)
	IsAlive(name string) (bool, error)
}

// AgentSession represents a running or completed agent session.
type AgentSession struct {
	ID          int
	TaskID    string
	TmuxSession string
	Command     string
	Status      string
	StartedAt   time.Time
	EndedAt     *time.Time
}

// AgentService manages agent session lifecycle.
type AgentService interface {
	SpawnAgent(ctx context.Context, taskID string) error
	KillAgent(ctx context.Context, taskID string) error
	ListAgents(ctx context.Context) ([]AgentSession, error)
	ReconcileSessions(ctx context.Context) error
	CaptureOutput(ctx context.Context, taskID string) (string, error)
	AttachCmd(ctx context.Context, taskID string) (*exec.Cmd, error)
}

type agentService struct {
	store   *store.Store
	tmux    TmuxManager
	workDir string
}

// NewAgentService creates an AgentService.
func NewAgentService(s *store.Store, tmux TmuxManager, workDir string) AgentService {
	return &agentService{store: s, tmux: tmux, workDir: workDir}
}

func (a *agentService) SpawnAgent(ctx context.Context, taskID string) error {
	// Check for existing running session
	existing, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err == nil {
		// DB says "running" — verify the tmux session is actually alive
		alive, aliveErr := a.tmux.IsAlive(existing.TmuxSession)
		if aliveErr == nil && alive {
			return fmt.Errorf("agent already running for task %s", taskID)
		}
		// Tmux session is gone — mark it dead so we can re-spawn
		_ = a.store.UpdateAgentSessionStatus(ctx, taskID, "dead")
	}

	// Clean up dead sessions so UNIQUE constraint doesn't block re-spawn
	_ = a.store.DeleteDeadAgentSessions(ctx, taskID)

	sessionName := "legato-" + taskID
	// Kill any orphaned tmux session with the same name
	a.tmux.Kill(sessionName)

	if err := a.tmux.Spawn(sessionName, a.workDir); err != nil {
		return fmt.Errorf("spawning tmux session: %w", err)
	}

	if err := a.store.InsertAgentSession(ctx, store.AgentSession{
		TaskID:    taskID,
		TmuxSession: sessionName,
		Command:     "shell",
		Status:      "running",
	}); err != nil {
		// Roll back tmux session on DB failure
		a.tmux.Kill(sessionName)
		return fmt.Errorf("recording agent session: %w", err)
	}

	return nil
}

func (a *agentService) KillAgent(ctx context.Context, taskID string) error {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		// No running session — delete dead records
		return a.store.DeleteDeadAgentSessions(ctx, taskID)
	}

	a.tmux.Kill(session.TmuxSession)
	if err := a.store.UpdateAgentSessionStatus(ctx, taskID, "dead"); err != nil {
		return err
	}
	return a.store.DeleteDeadAgentSessions(ctx, taskID)
}

func (a *agentService) ListAgents(ctx context.Context) ([]AgentSession, error) {
	sessions, err := a.store.ListAgentSessions(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]AgentSession, len(sessions))
	for i, s := range sessions {
		startedAt, err := time.Parse("2006-01-02 15:04:05", s.StartedAt)
		if err != nil {
			startedAt = time.Now() // fallback to avoid huge elapsed times
		}
		var endedAt *time.Time
		if s.EndedAt != nil {
			t, _ := time.Parse("2006-01-02 15:04:05", *s.EndedAt)
			endedAt = &t
		}
		result[i] = AgentSession{
			ID:          s.ID,
			TaskID:    s.TaskID,
			TmuxSession: s.TmuxSession,
			Command:     s.Command,
			Status:      s.Status,
			StartedAt:   startedAt,
			EndedAt:     endedAt,
		}
	}
	return result, nil
}

func (a *agentService) ReconcileSessions(ctx context.Context) error {
	sessions, err := a.store.ListAgentSessions(ctx)
	if err != nil {
		return err
	}

	liveSessions, err := a.tmux.ListSessions()
	if err != nil {
		return err
	}

	live := make(map[string]bool)
	for _, s := range liveSessions {
		live[s] = true
	}

	for _, s := range sessions {
		if s.Status == "running" && !live[s.TmuxSession] {
			if err := a.store.UpdateAgentSessionStatus(ctx, s.TaskID, "dead"); err != nil {
				return err
			}
		}
	}

	return nil
}

func (a *agentService) CaptureOutput(ctx context.Context, taskID string) (string, error) {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return "", err
	}
	return a.tmux.Capture(session.TmuxSession)
}

func (a *agentService) AttachCmd(ctx context.Context, taskID string) (*exec.Cmd, error) {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return a.tmux.Attach(session.TmuxSession), nil
}
