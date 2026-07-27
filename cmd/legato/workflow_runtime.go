package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	gitengine "github.com/cpave3/legato/internal/engine/git"
	gh "github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/tmux"
	"github.com/cpave3/legato/internal/engine/worktree"
	"github.com/cpave3/legato/internal/service"
)

func buildWorkflowService(db *store.Store, cfg *config.Config, bus *events.Bus, existingAgents service.AgentService, existingPR service.PRTrackingService, workDir string) (*service.WorkflowService, error) {
	if bus == nil {
		bus = events.New()
	}
	prSvc := existingPR
	if prSvc == nil {
		client, err := gh.New(gh.Options{})
		if err != nil {
			return nil, fmt.Errorf("GitHub workflows require gh: %w", err)
		}
		prSvc = service.NewPRTrackingService(db, bus, client, 10*time.Minute, 10*time.Minute)
	}
	agentSvc := existingAgents
	if agentSvc == nil {
		mgr, err := tmux.New(tmux.Options{})
		if err != nil {
			return nil, fmt.Errorf("agent workflows require tmux: %w", err)
		}
		binary, _ := os.Executable()
		if binary == "" {
			binary = "legato"
		}
		claude := hooks.NewClaudeCodeAdapter(binary)
		chimera := hooks.NewChimeraAdapter(binary)
		codex := hooks.NewCodexAdapter(binary)
		adapters := map[string]service.AIToolAdapter{claude.Name(): claude, chimera.Name(): chimera, codex.Name(): codex}
		agentSvc = service.NewAgentService(db, mgr, workDir, service.AgentServiceOptions{Adapter: chimera, Adapters: adapters, PRService: prSvc, EventBus: service.AgentDiedPublisher{Bus: bus}})
	}
	runner := service.NewWorkflowAgentRunner(agentSvc, db)
	executors := []service.WorkflowExecutor{
		service.NewGitHubPRPrepareExecutor(prSvc, gitengine.ExactFetcher{}, service.NewWorktreeWorkflow(db, worktree.NewRunner("yg")), prSvc),
		service.NewAgentRunExecutor(runner),
	}
	dataDir := filepath.Join(filepath.Dir(config.ResolveDBPath(cfg)), "artifacts")
	return service.NewWorkflowServiceWithOutputs(db, executors, dataDir), nil
}
