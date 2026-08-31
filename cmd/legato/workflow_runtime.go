package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	gitengine "github.com/cpave3/legato/internal/engine/git"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/tmux"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/engine/worktree"
	"github.com/cpave3/legato/internal/service"
)

func checkoutInspector() service.CheckoutInspector {
	return service.CheckoutInspectFunc(func(directory string) (*store.CodeContext, error) {
		checkout, err := gitengine.InspectCheckout(context.Background(), directory)
		if err != nil {
			return nil, err
		}
		return &store.CodeContext{Repo: checkout.Repo, Branch: checkout.Branch, HeadSHA: checkout.HeadSHA, WorkingDir: checkout.WorkingDir}, nil
	})
}

type workflowRuntimeOptions struct {
	Bus     *events.Bus
	Board   service.BoardService
	Sync    service.SyncService
	Agents  service.AgentService
	PR      service.PRTrackingService
	PRErr   error
	WorkDir string
}

func buildWorkflowService(db *store.Store, cfg *config.Config, opts workflowRuntimeOptions) (*service.WorkflowService, error) {
	bus := opts.Bus
	if bus == nil {
		bus = events.New()
	}
	board := opts.Board
	if board == nil {
		board = service.NewBoardService(db, bus)
	}
	prSvc := opts.PR
	prErr := opts.PRErr
	if prSvc == nil && prErr == nil {
		client, _, err := buildGitHubClient(cfg, nil)
		if err != nil {
			prErr = err
		} else {
			prSvc = service.NewPRTrackingServiceWithInspector(db, bus, client, 10*time.Minute, 10*time.Minute, checkoutInspector(), githubRepositoryMappings(cfg)...)
		}
	}
	learningSvc := configuredLearningService(cfg, db, bus, service.NewProjectIdentityResolver(checkoutInspector()))
	agentSvc := opts.Agents
	if agentSvc == nil {
		mgr, err := tmux.New(tmux.Options{})
		if err != nil {
			return nil, fmt.Errorf("agent workflows require tmux: %w", err)
		}
		binary, _ := os.Executable()
		if binary == "" {
			binary = "legato"
		}
		defaultAdapter, adapters := configuredAdapters(cfg, binary)
		var promptProvider service.LearningPromptProvider
		if cfg.Learning.PromptEnabled {
			promptProvider = learningSvc
		}
		agentSvc = service.NewAgentService(db, mgr, opts.WorkDir, service.AgentServiceOptions{Adapter: defaultAdapter, Adapters: adapters, PRService: prSvc, EventBus: service.AgentDiedPublisher{Bus: bus}, TmuxOptions: cfg.Agents.TmuxOptions, LearningPromptProvider: promptProvider, LearningPromptMaxItems: cfg.Learning.PromptMaxItems, LearningPromptMaxBytes: cfg.Learning.PromptMaxBytes, DisablePeerAwareness: !cfg.Agents.PeerAwareness})
	}
	binary, _ := os.Executable()
	runner := service.NewWorkflowAgentRunner(agentSvc, db, binary)
	executors := []service.WorkflowExecutor{
		service.NewAgentRunExecutor(runner, binary),
		service.NewAgentDispatchExecutor(agentSvc, db),
		service.NewAgentStopExecutor(runner),
		service.NewChimeraSessionRemoveExecutor(db, service.NewChimeraCLISessionRemover("chimera")),
	}
	executors = append(executors, learningWorkflowExecutors(db, learningSvc, runner, binary)...)
	var moveSync service.CardMoveSync
	if configured, ok := opts.Sync.(service.CardMoveSync); ok {
		moveSync = configured
	}
	executors = append(executors,
		service.NewTaskContextExecutor(board),
		service.NewTaskWorkingDirResolveExecutor(db),
		service.NewTaskNoteExecutor(db),
		service.NewTaskMetadataExecutor(db),
		service.NewTaskMoveExecutor(service.NewWorkflowTaskMover(board, moveSync)),
		service.NewFileRemoveExecutor(),
	)
	if cfg.Worktrees.Yggdrasil.Enabled {
		worktrees := service.NewWorktreeWorkflow(db, worktree.NewRunner("yg"), checkoutInspector())
		if prSvc != nil {
			executors = append(executors, service.NewGitHubPRPrepareExecutor(prSvc, gitengine.ExactFetcher{}, worktrees, prSvc))
		} else {
			executors = append(executors, service.NewUnavailableGitHubPRPrepareExecutor(prErr))
		}
		executors = append(executors, service.NewWorktreeRemoveExecutor(worktrees))
	}
	workflows := service.NewWorkflowServiceWithOutputs(db, executors, workflowArtifactRoot(cfg))
	workflows.SetRepositoryPresets(workflowRepositoryPresets(cfg))
	return workflows, nil
}

func learningWorkflowExecutors(db *store.Store, learning *service.LearningService, runner *service.WorkflowAgentRunner, binary string) []service.WorkflowExecutor {
	return []service.WorkflowExecutor{
		service.NewAgentInterviewExecutor(learning, runner, db, binary),
		service.NewLearningPacketPrepareExecutor(learning),
		service.NewLearningProposalsSubmitExecutor(learning),
	}
}

func learningPromptProvider(cfg *config.Config, db *store.Store, bus *events.Bus) service.LearningPromptProvider {
	if !cfg.Learning.PromptEnabled {
		return nil
	}
	return configuredLearningService(cfg, db, bus, service.NewProjectIdentityResolver(checkoutInspector()))
}

func githubRepositoryMappings(cfg *config.Config) []service.RepositoryMapping {
	mappings := make([]service.RepositoryMapping, len(cfg.Repositories))
	for i, repository := range cfg.Repositories {
		mappings[i] = service.RepositoryMapping{Repo: repository.Repo, BaseBranch: repository.BaseBranch, JiraProjects: repository.JiraProjects, Workspaces: repository.Workspaces}
	}
	return mappings
}

func workflowRepositoryPresets(cfg *config.Config) []workflowengine.RepositoryPreset {
	presets := make([]workflowengine.RepositoryPreset, len(cfg.Repositories))
	for i, repository := range cfg.Repositories {
		presets[i] = workflowengine.RepositoryPreset{Name: repository.Name, Repo: repository.Repo, PrimaryDir: repository.PrimaryDir, BaseBranch: repository.BaseBranch}
	}
	return presets
}
