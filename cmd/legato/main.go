package main

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/events"
	gh "github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/jira"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/tmux"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/setup"
	"github.com/cpave3/legato/internal/tui"
	"github.com/cpave3/legato/internal/tui/theme"
)

func main() {
	// Subcommand dispatch: if args present, handle CLI mode.
	if len(os.Args) > 1 {
		os.Exit(runCLI(os.Args[1:]))
	}

	// Default: launch TUI.
	os.Exit(runTUI())
}

func runCLI(args []string) int {
	switch args[0] {
	case "task":
		return runTaskCmd(args[1:])
	case "agent":
		return runAgentCmd(args[1:])
	case "hooks":
		return runHooksCmd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		fmt.Fprintf(os.Stderr, "usage: legato [task|agent|hooks]\n")
		return 1
	}
}

func runTaskCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: legato task [update|note|link|unlink] ...\n")
		return 1
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	dbPath := config.ResolveDBPath(cfg)
	db, err := store.New(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database: %v\n", err)
		return 1
	}
	defer db.Close()

	switch args[0] {
	case "update":
		return runTaskUpdate(db, args[1:])
	case "note":
		return runTaskNote(db, args[1:])
	case "link":
		return runTaskLink(db, args[1:])
	case "unlink":
		return runTaskUnlink(db, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown task command: %s\n", args[0])
		fmt.Fprintf(os.Stderr, "usage: legato task [update|note|link|unlink] ...\n")
		return 1
	}
}

func runTaskUpdate(db *store.Store, args []string) int {
	// Parse: legato task update <task-id> --status <status>
	if len(args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: legato task update <task-id> --status <status>\n")
		return 1
	}

	taskID := args[0]
	var status string
	for i := 1; i < len(args)-1; i++ {
		if args[i] == "--status" {
			status = args[i+1]
			break
		}
	}
	if status == "" {
		fmt.Fprintf(os.Stderr, "usage: legato task update <task-id> --status <status>\n")
		return 1
	}

	if err := cli.TaskUpdate(db, taskID, status); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runTaskNote(db *store.Store, args []string) int {
	// Parse: legato task note <task-id> <message>
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: legato task note <task-id> <message>\n")
		return 1
	}

	taskID := args[0]
	message := args[1]

	if err := cli.TaskNote(db, taskID, message); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runTaskLink(db *store.Store, args []string) int {
	// Parse: legato task link <task-id> [--branch <branch>] [--repo <owner/repo>]
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: legato task link <task-id> [--branch <branch>] [--repo <owner/repo>]\n")
		return 1
	}

	taskID := args[0]
	var branch, repo string
	for i := 1; i < len(args)-1; i++ {
		switch args[i] {
		case "--branch":
			branch = args[i+1]
		case "--repo":
			repo = args[i+1]
		}
	}

	if err := cli.TaskLink(db, taskID, branch, repo); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	if branch == "" {
		fmt.Println("Linked current branch to task", taskID)
	} else {
		fmt.Printf("Linked branch %q to task %s\n", branch, taskID)
	}
	return 0
}

func runTaskUnlink(db *store.Store, args []string) int {
	// Parse: legato task unlink <task-id>
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: legato task unlink <task-id>\n")
		return 1
	}

	taskID := args[0]
	if err := cli.TaskUnlink(db, taskID); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Println("Unlinked branch from task", taskID)
	return 0
}

func runAgentCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: legato agent [state] ...\n")
		return 1
	}

	switch args[0] {
	case "state":
		return runAgentState(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown agent command: %s\n", args[0])
		return 1
	}
}

func runAgentState(args []string) int {
	// Parse: legato agent state <task-id> --activity <working|waiting|"">
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: legato agent state <task-id> --activity <working|waiting|\"\">\n")
		return 1
	}

	taskID := args[0]
	activity := ""
	for i := 1; i < len(args)-1; i++ {
		if args[i] == "--activity" {
			activity = args[i+1]
			break
		}
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	dbPath := config.ResolveDBPath(cfg)
	db, err := store.New(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database: %v\n", err)
		return 1
	}
	defer db.Close()

	if err := cli.AgentState(db, taskID, activity); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runHooksCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: legato hooks [install|uninstall] [--tool <name>]\n")
		return 1
	}

	// Parse --tool flag, default to "claude-code".
	tool := "claude-code"
	for i := 1; i < len(args)-1; i++ {
		if args[i] == "--tool" {
			tool = args[i+1]
			break
		}
	}

	registry := service.NewAdapterRegistry()

	// Register known adapters.
	legatoBin, err := os.Executable()
	if err != nil {
		legatoBin = "legato" // fallback
	}
	registry.Register(hooks.NewClaudeCodeAdapter(legatoBin))
	registry.Register(hooks.NewStaccatoAdapter(legatoBin))

	adapter, err := registry.Get(tool)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		fmt.Fprintf(os.Stderr, "available tools: %v\n", registry.List())
		return 1
	}

	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error getting working directory: %v\n", err)
		return 1
	}

	switch args[0] {
	case "install":
		if err := adapter.InstallHooks(wd); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Printf("Installed %s hooks in %s\n", tool, wd)
		return 0
	case "uninstall":
		if err := adapter.UninstallHooks(wd); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Printf("Uninstalled %s hooks from %s\n", tool, wd)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown hooks command: %s\n", args[0])
		fmt.Fprintf(os.Stderr, "usage: legato hooks [install|uninstall] [--tool <name>]\n")
		return 1
	}
}

func runTUI() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	dbPath := config.ResolveDBPath(cfg)
	db, err := store.New(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database: %v\n", err)
		return 1
	}
	defer db.Close()

	// First-run setup: seed columns if none exist
	mappings, err := db.ListColumnMappings(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "checking columns: %v\n", err)
		return 1
	}
	if len(mappings) == 0 {
		adapter := &setup.StoreAdapter{S: db}
		jiraSetup := &setup.RealJiraSetup{}
		if err := setup.RunWizard(context.Background(), os.Stdout, os.Stdin, adapter, cfg.Board.Columns, jiraSetup); err != nil {
			fmt.Fprintf(os.Stderr, "setup: %v\n", err)
			return 1
		}
		// Reload config in case Jira was configured
		cfg, err = config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "config reload: %v\n", err)
			return 1
		}
	}

	// Seed workspaces from config
	if len(cfg.Workspaces) > 0 {
		if err := service.SeedWorkspaces(context.Background(), db, cfg.Workspaces); err != nil {
			fmt.Fprintf(os.Stderr, "seeding workspaces: %v\n", err)
			return 1
		}
	}

	bus := events.New()
	boardSvc := service.NewBoardService(db, bus)

	// Start IPC server for CLI→TUI communication.
	socketPath := ipc.SocketPath()
	ipcServer, ipcErr := ipc.NewServer(socketPath, func(msg ipc.Message) {
		switch msg.Type {
		case "task_update", "task_note", "agent_state":
			bus.Publish(events.Event{
				Type: events.EventCardUpdated,
				At:   time.Now(),
			})
		case "pr_linked":
			bus.Publish(events.Event{
				Type: events.EventPRStatusUpdated,
				At:   time.Now(),
			})
		}
	})
	if ipcErr == nil {
		defer ipcServer.Close()
	}

	// Set up Jira sync if configured
	var syncSvc service.SyncService
	if cfg.Jira.BaseURL != "" && cfg.Jira.Email != "" && cfg.Jira.APIToken != "" {
		jiraProvider := jira.NewProvider(cfg.Jira.BaseURL, cfg.Jira.Email, cfg.Jira.APIToken, 30*time.Second)
		provider := service.NewJiraProvider(jiraProvider)
		interval := time.Duration(cfg.Jira.SyncIntervalSeconds) * time.Second
		syncSvc = service.NewSyncService(db, bus, provider, cfg.Jira.JQLFilter, cfg.Jira.ProjectKeys, interval)

		// Run initial sync, then start periodic scheduler
		go syncSvc.Sync(context.Background())
		stopSync := syncSvc.StartScheduler(context.Background())
		defer stopSync()
	}

	// Set up GitHub PR tracking (gh CLI may not be installed — that's OK)
	var prSvc service.PRTrackingService
	ghClient, ghErr := gh.New(gh.Options{})
	if ghErr == nil {
		interval := time.Duration(cfg.GitHub.PollIntervalSeconds) * time.Second
		resolvedInterval := time.Duration(cfg.GitHub.ResolvedPollIntervalSeconds) * time.Second
		prSvc = service.NewPRTrackingService(db, bus, ghClient, interval, resolvedInterval)
		// Initial poll fetches all PRs (resolved + unresolved), then periodic uses split cadence
		go prSvc.PollAll(context.Background())
		stopPR := prSvc.StartPolling(context.Background())
		defer stopPR()
	}

	// Set up agent service (tmux may not be installed — that's OK, agent features just won't work)
	var agentSvc service.AgentService
	escapeKey := cfg.Agents.EscapeKey
	if escapeKey == "" {
		escapeKey = "ctrl+]"
	}
	tmuxMgr, tmuxErr := tmux.New(tmux.Options{EscapeKey: tmuxEscapeKey(escapeKey)})
	if tmuxErr == nil {
		wd, _ := os.Getwd()

		// Configure AI tool adapter for env var injection.
		legatoBin, binErr := os.Executable()
		if binErr != nil {
			legatoBin = "legato"
		}
		ccAdapter := hooks.NewClaudeCodeAdapter(legatoBin)
		agentSvc = service.NewAgentService(db, tmuxMgr, wd, service.AgentServiceOptions{
			Adapter:     ccAdapter,
			SocketPath:  socketPath,
			TmuxOptions: cfg.Agents.TmuxOptions,
			PRService:   prSvc,
		})
	}

	icons := theme.NewIcons(cfg.Icons)
	editor := config.ResolveEditor(cfg)

	// Load workspaces for TUI
	workspaces, _ := boardSvc.ListWorkspaces(context.Background())

	app := tui.NewApp(boardSvc, syncSvc, agentSvc, prSvc, icons, bus, editor, workspaces)

	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// tmuxEscapeKey converts config format "ctrl+]" to tmux format "C-]".
func tmuxEscapeKey(key string) string {
	if len(key) > 5 && key[:5] == "ctrl+" {
		return "C-" + key[5:]
	}
	return key
}
