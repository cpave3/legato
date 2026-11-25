package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/chimera"
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
	// Repos is the repository cache jobs clone from; built here when nil.
	Repos *service.RepositoryService
	// Presets are the repositories runs pick from, read for each run;
	// the configuration's alone when nil.
	Presets *service.RepositoryPresetService
}

// prWaitInterval is how often a host reads the pull request of each
// github.pr.wait step that waits, besides after each refresh of a card's.
const prWaitInterval = 2 * time.Minute

// lazyPRStates reads pull requests with a GitHub client made on first use.
type lazyPRStates struct {
	cfg    *config.Config
	once   sync.Once
	states service.GitHubPRStates
	err    error
}

func (l *lazyPRStates) PullRequestState(ctx context.Context, repo string, number int) (service.PRState, error) {
	l.once.Do(func() {
		l.states.Client, _, l.err = buildGitHubClient(l.cfg, nil)
	})
	if l.err != nil {
		return service.PRState{}, l.err
	}
	return l.states.PullRequestState(ctx, repo, number)
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
	// The GitHub client is made when a github.pr.wait step first reads a
	// pull request, so a command that runs none never asks gh for a token.
	prWait := service.NewPRWaitExecutor(&lazyPRStates{cfg: cfg})
	learningSvc := configuredLearningService(cfg, db, bus, service.NewProjectIdentityResolver(checkoutInspector()))
	agentSvc := opts.Agents
	if agentSvc == nil {
		var promptProvider service.LearningPromptProvider
		if cfg.Learning.PromptEnabled {
			promptProvider = learningSvc
		}
		var err error
		if agentSvc, opts.Repos, err = commandAgentService(cfg, db, opts.WorkDir, prSvc, bus, promptProvider, opts.Repos, opts.Presets); err != nil {
			return nil, fmt.Errorf("agent workflows require tmux: %w", err)
		}
	}
	binary, _ := os.Executable()
	runner := service.NewWorkflowAgentRunner(agentSvc, db, binary)
	runner.SetBus(bus)
	macros := service.NewMacroService(settingsFor(db))
	agentRun := service.NewAgentRunExecutor(runner, binary)
	agentRun.SetMacros(macros)
	agentRun.SetScratchRoot(filepath.Join(config.ResolveDataDir(cfg), service.WorkflowScratchFolder), db.WorkflowAttemptInUse)
	agentMessage := service.NewAgentMessageExecutor(runner, db)
	agentMessage.SetMacros(macros)
	agentMessage.SetBinaryPath(binary)
	chatBots := chatBotsFor(cfg, db)
	executors := []service.WorkflowExecutor{
		agentRun,
		service.NewAgentDispatchExecutor(agentRun),
		agentMessage,
		service.NewAgentStopExecutor(runner),
		agentCompactExecutor(agentSvc),
		service.NewChimeraSessionRemoveExecutor(db, chimera.NewSessionRemover("chimera")),
	}
	var repoCache service.RepoCache
	if opts.Repos != nil {
		repoCache = opts.Repos
	}
	executors = append(executors, service.NewRepoEnsureExecutor(repoCache))
	executors = append(executors, learningWorkflowExecutors(db, learningSvc, runner, binary)...)
	var moveSync service.CardMoveSync
	if configured, ok := opts.Sync.(service.CardMoveSync); ok {
		moveSync = configured
	}
	// The ticket steps go through the sync service's sources; without one
	// they fail saying so.
	var tickets service.TicketImporter
	var ticketLists service.TicketLister
	if configured, ok := opts.Sync.(service.TicketImporter); ok {
		tickets = configured
	}
	if configured, ok := opts.Sync.(service.TicketLister); ok {
		ticketLists = configured
	}
	executors = append(executors,
		service.NewTaskContextExecutor(board),
		service.NewTaskWorkingDirResolveExecutor(db),
		service.NewTaskNoteExecutor(db),
		service.NewTaskMetadataExecutor(db),
		service.NewTaskMoveExecutor(service.NewWorkflowTaskMover(board, moveSync)),
		service.NewTicketImportExecutor(tickets, db),
		service.NewTicketsListExecutor(ticketLists),
		service.NewTasksListExecutor(db),
		service.NewFileRemoveExecutor(),
		prWait,
		service.NewBurnliteCloseExecutor(db, service.ConnectedBurnlite{Connection: burnliteConnections(cfg, db)}),
		service.NewTicketCreateExecutor(db, ticketWriters(cfg, db)...),
		service.NewTicketCommentExecutor(db, ticketWriters(cfg, db)...),
		service.NewChatSendExecutor(chatBots),
		service.NewChatReactionAddExecutor(chatBots),
		service.NewChatReactionRemoveExecutor(chatBots),
		service.NewChatThreadReadExecutor(chatBots),
		// Starting it only writes the step; the review engine's tick in
		// the broker holder opens it (attachReviewRuns, spec LEG-559 D9).
		reviewPassesStep(cfg),
	)
	// The review loop steps, opened and settled by the same tick.
	executors = append(executors, reviewLoopSteps(cfg).Executors()...)
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
	// Every change to a run's state, for the web UI (Server.StartWorkflowEvents).
	workflows.SetEventBus(bus)
	workflows.SetRepositoryPresets(workflowRepositoryPresets(cfg))
	if opts.Presets != nil {
		workflows.SetRepositorySource(opts.Presets.WorkflowPresets)
	}
	workflows.SetAgentKinds(agentSvc.RegisteredAdapters())
	workflows.SetAgentOutputs(agentOutputSources(db, agentSvc, prSvc))
	// workflow.run steps call the workflows found now, by name (LEG-489).
	workflows.SetDefinitions(workflowDiscoveryProvider(cfg, db))
	// Resume agent: a run's card agent started with a macro, as the card
	// menu starts one, and stopped when the run's merge is over.
	workflows.SetAgentResumer(macrosFor(db, agentSvc), agentSvc)
	// Resume on a suspended step: its interrupted agent, resumed on the
	// same attempt (LEG-625).
	if interrupted, ok := agentSvc.(service.InterruptedAgents); ok {
		workflows.SetInterruptedAgents(interrupted)
	}
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

// repositoryGuidance is the guidance agents get for the repository they work
// in: config.yaml's agents.guidance, then what was saved in the web UI.
func repositoryGuidance(cfg *config.Config, db *store.Store) *service.RepositoryGuidanceService {
	return service.NewRepositoryGuidanceService(db, cfg.Agents.Guidance)
}

// pullRequestModes is pull_requests.rules as Settings has them now: the
// effective configuration, read at each ask, else cfg's.
func pullRequestModes(cfg *config.Config, db *store.Store) service.PullRequestModes {
	return func(ctx context.Context) (config.PullRequestsConfig, error) {
		if db != nil {
			if effective, err := settingsFor(db).Effective(ctx); err == nil && effective != nil {
				return effective.PullRequests, nil
			}
		}
		return cfg.PullRequests, nil
	}
}

// repositoryResources is agents.repository_resources, resolved against the
// configuration's resources.
func repositoryResources(cfg *config.Config) *service.RepositoryResources {
	return service.NewRepositoryResources(cfg.Agents.RepositoryResources, cfg.Resources)
}

// commandAgentService is the agent service a command (outside the TUI and
// serve) starts agents through, configured as theirs is: the adapters,
// sandbox, credentials, repositories and engrams. repos is built when nil
// and returned.
func commandAgentService(cfg *config.Config, db *store.Store, workDir string, prSvc service.PRTrackingService, bus *events.Bus, promptProvider service.LearningPromptProvider, repos *service.RepositoryService, presets *service.RepositoryPresetService) (service.AgentService, *service.RepositoryService, error) {
	mgr, err := tmux.New(tmux.Options{})
	if err != nil {
		return nil, repos, err
	}
	binary, _ := os.Executable()
	if binary == "" {
		binary = "legato"
	}
	defaultAdapter, adapters := configuredAdapters(cfg, binary)
	agentSvc := service.NewAgentService(db, mgr, workDir, service.AgentServiceOptions{ToolCheck: processDoctor().SpawnCheck, Adapter: defaultAdapter, Adapters: adapters, PRService: prSvc, EventBus: service.AgentDiedPublisher{Bus: bus}, TmuxOptions: cfg.Agents.TmuxOptions, LearningPromptProvider: promptProvider, LearningPromptMaxItems: cfg.Learning.PromptMaxItems, LearningPromptMaxBytes: cfg.Learning.PromptMaxBytes, DisablePeerAwareness: !cfg.Agents.PeerAwareness, PeerMessageSent: peerMessageSent, ReeveWakes: cfg.Reeve.Watch.Wake, ReviewsOff: !cfg.Agents.Reviews, HasSkill: agentHasSkill(cfg, db), RepositoryGuidance: repositoryGuidance(cfg, db), PullRequestModes: pullRequestModes(cfg, db), RepositoryResources: repositoryResources(cfg), Sandbox: sandboxSettings(cfg), SandboxScratch: sandboxScratch(cfg), Capabilities: skillService(cfg, db), CapabilityFolders: capabilityFolders(cfg)})
	channels := attachCredentialChannels(cfg, db, agentSvc)
	if repos == nil {
		repos = newRepositoryService(cfg, db)
		if channels != nil {
			repos.SetTokens(channels)
		}
	}
	giveAgentsRepositories(agentSvc, repos)
	giveAgentsPrimaryDirs(agentSvc, presets)
	giveAgentsEngrams(agentSvc, db)
	giveAgentsWorkStages(agentSvc, db, bus)
	return agentSvc, repos, nil
}

// agentCompactExecutor is agent.compact, compacting through the agent
// service; without one that compacts, its steps fail with the reason.
func agentCompactExecutor(agents service.AgentService) *service.AgentCompactExecutor {
	compactor, _ := agents.(service.AgentCompactor)
	return service.NewAgentCompactExecutor(compactor)
}

// compactWaits has the agent.compact steps of workflows told of their
// agents' compactions by the approval watcher, and checked every
// service.CompactCheckEvery. It returns a function that stops it.
func (rt *approvalRuntime) compactWaits(workflows *service.WorkflowService) func() {
	if workflows == nil {
		return func() {}
	}
	var events service.CompactionEvents
	if rt != nil && rt.watcher != nil {
		events = rt.watcher
	}
	return workflows.StartCompactWaits(context.Background(), events, service.CompactCheckEvery)
}

// reviewLoopSteps are the review loop step executors (github.pr.review.open
// and the rest), refused while review_passes is off, with this Legato's
// review_passes.max_reentries.
func reviewLoopSteps(cfg *config.Config) *service.ReviewLoopSteps {
	steps := service.NewReviewLoopSteps()
	steps.SetEnabled(func() bool { return cfg.ReviewPasses.Enabled })
	steps.SetMaxReentries(func() int { return cfg.ReviewPasses.MaxReentries })
	return steps
}

// reviewPassesStep is the github.pr.review_passes executor, refused while
// review_passes is off: no Legato on the data folder runs the engine then.
func reviewPassesStep(cfg *config.Config) *service.ReviewPassesStep {
	step := service.NewReviewPassesStep()
	step.SetEnabled(func() bool { return cfg.ReviewPasses.Enabled })
	return step
}

// ticketWriters are the providers the ticket steps write to (LEG-640):
// the Jira of this configuration, whose settings are read now as the sync
// service reads them, and the Burnlite Legato is connected to. A writer
// whose settings are missing says so when a step uses it.
func ticketWriters(cfg *config.Config, db *store.Store) []service.TicketWriter {
	return []service.TicketWriter{
		service.JiraTicketWriter{BaseURL: cfg.Jira.BaseURL, Email: cfg.Jira.Email, APIToken: cfg.Jira.APIToken},
		service.BurnliteTicketWriter{Connection: burnliteConnections(cfg, db)},
	}
}

// agentOutputSources fill an agent step's outputs at its signal (LEG-485):
// its clone's stack, the pull requests of its branches, and its last
// message.
func agentOutputSources(db *store.Store, agents service.AgentService, prs service.PRTrackingService) service.AgentOutputSources {
	sources := service.AgentOutputSources{Stacks: service.NewTaskStackService(db, nil)}
	if branches, ok := prs.(service.PRBranchActions); ok {
		sources.PRs = branches
	}
	if reader, ok := agents.(service.AgentConversationReader); ok {
		sources.LastMessage = func(ctx context.Context, agentID string) string {
			tail, err := reader.ConversationTail(ctx, agentID, 20)
			if err != nil || tail == nil {
				return ""
			}
			for i := len(tail.Entries) - 1; i >= 0; i-- {
				if entry := tail.Entries[i]; entry.Kind == service.EntryAssistant && strings.TrimSpace(entry.Text) != "" {
					return strings.TrimSpace(entry.Text)
				}
			}
			return ""
		}
	}
	return sources
}

// grantWorkflowBranches lets version 2 agent steps grant their agents the
// branches they name (LEG-485), through the credential broker, which is
// started after the workflow service.
func grantWorkflowBranches(workflows *service.WorkflowService, rt *approvalRuntime) {
	if workflows == nil || rt == nil || rt.credentials == nil {
		return
	}
	if run, ok := workflows.Executor("agent.run").(*service.AgentRunExecutor); ok {
		run.SetBranchGrants(rt.credentials)
	}
}
