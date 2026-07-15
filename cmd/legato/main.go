package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/attachments"
	"github.com/cpave3/legato/internal/engine/auth"
	"github.com/cpave3/legato/internal/engine/certs"
	"github.com/cpave3/legato/internal/engine/events"
	gh "github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/jira"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/swarm"
	"github.com/cpave3/legato/internal/engine/tmux"
	"github.com/cpave3/legato/internal/engine/worktree"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/setup"
	"github.com/cpave3/legato/internal/tui"
	"github.com/cpave3/legato/internal/tui/theme"
	qrterminal "github.com/mdp/qrterminal/v3"
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
	case "serve":
		return runServeCmd(args[1:])
	case "auth":
		return runAuthCmd(args[1:])
	case "pair":
		return runPairCmd(args[1:])
	case "swarm":
		return runSwarmCmd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		fmt.Fprintf(os.Stderr, "usage: legato [task|agent|hooks|serve|auth|pair|swarm]\n")
		return 1
	}
}

func runTaskCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: legato task [show|update|note|link|unlink] ...\n")
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
	case "show":
		return runTaskShow(db, args[1:])
	case "update":
		return runTaskUpdate(db, args[1:])
	case "note":
		return runTaskNote(db, args[1:])
	case "link":
		return runTaskLink(db, args[1:])
	case "unlink":
		return runTaskUnlink(db, args[1:])
	case "worktree":
		return runTaskWorktree(db, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown task command: %s\n", args[0])
		fmt.Fprintf(os.Stderr, "usage: legato task [show|update|note|link|unlink|worktree] ...\n")
		return 1
	}
}

func runTaskWorktree(db *store.Store, args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: legato task worktree [set|clear] <task-id> ...")
		return 1
	}
	switch args[0] {
	case "set":
		return runTaskWorktreeSet(db, args[1:])
	case "clear":
		return runTaskWorktreeClear(db, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown worktree command: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "usage: legato task worktree [set|clear] <task-id> ...")
		return 1
	}
}

func runTaskWorktreeSet(db *store.Store, args []string) int {
	values := map[string]string{
		"--path": os.Getenv("YG_WORKTREE"), "--primary-dir": os.Getenv("YG_PRIMARY"),
		"--branch": os.Getenv("YG_BRANCH"), "--base-branch": os.Getenv("YG_BASE"),
	}
	taskID := os.Getenv("LEGATO_TASK_ID")
	start := 1
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		taskID = args[0]
		start = 2
	}
	for i := start - 1; i < len(args); i += 2 {
		if i+1 >= len(args) {
			fmt.Fprintf(os.Stderr, "missing value for %s\n", args[i])
			return 1
		}
		if _, ok := values[args[i]]; !ok {
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", args[i])
			return 1
		}
		values[args[i]] = args[i+1]
	}
	meta := store.TaskWorktree{Path: values["--path"], PrimaryDir: values["--primary-dir"], Branch: values["--branch"], BaseBranch: values["--base-branch"]}
	if err := cli.TaskWorktreeSet(db, taskID, meta); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runTaskWorktreeClear(db *store.Store, args []string) int {
	taskID := os.Getenv("LEGATO_TASK_ID")
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") {
		taskID = args[0]
	}
	if err := db.SetTaskWorktree(context.Background(), taskID, nil); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("Cleared worktree metadata for task %s\n", taskID)
	return 0
}

func runTaskShow(db *store.Store, args []string) int {
	// Parse: legato task show <task-id> [--format description|full|json]
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: legato task show <task-id> [--format description|full|json]\n")
		return 1
	}

	taskID := args[0]
	format := "description"
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "usage: legato task show <task-id> [--format description|full|json]\n")
				return 1
			}
			format = args[i+1]
			i++
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", args[i])
			fmt.Fprintf(os.Stderr, "usage: legato task show <task-id> [--format description|full|json]\n")
			return 1
		}
	}

	out, err := cli.TaskShow(db, taskID, format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	writeCLIOutput(out)
	return 0
}

func writeCLIOutput(out string) {
	fmt.Print(out)
	if !strings.HasSuffix(out, "\n") {
		fmt.Println()
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
	// Parse: legato task link <task-id> [--branch <branch>] [--repo <owner/repo>] [--sha <commit-sha>]
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: legato task link <task-id> [--branch <branch>] [--repo <owner/repo>] [--sha <commit-sha>]\n")
		return 1
	}

	taskID := args[0]
	var branch, repo, sha string
	for i := 1; i < len(args)-1; i++ {
		switch args[i] {
		case "--branch":
			branch = args[i+1]
		case "--repo":
			repo = args[i+1]
		case "--sha":
			sha = args[i+1]
		}
	}

	if err := cli.TaskLink(db, taskID, branch, repo, sha); err != nil {
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
		fmt.Fprintf(os.Stderr, "usage: legato agent [state|summary] ...\n")
		return 1
	}

	switch args[0] {
	case "state":
		return runAgentState(args[1:])
	case "summary":
		return runAgentSummary(args[1:])
	case "status":
		return runAgentStatus(args[1:])
	case "session-created":
		return runAgentSessionCreated(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown agent command: %s\n", args[0])
		return 1
	}
}

func runAgentSessionCreated(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: legato agent session-created <task-id> <session-id>")
		return 1
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	db, err := store.New(config.ResolveDBPath(cfg))
	if err != nil {
		fmt.Fprintf(os.Stderr, "database: %v\n", err)
		return 1
	}
	defer db.Close()
	if err := cli.AgentSessionCreated(db, args[0], args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runAgentState(args []string) int {
	// Parse: legato agent state <task-id> --activity <working|waiting|""> [--working-dir <dir>]
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: legato agent state <task-id> --activity <working|waiting|\"\"> [--working-dir <dir>]\n")
		return 1
	}

	taskID := args[0]
	activity := ""
	workingDir := ""
	for i := 1; i < len(args)-1; i++ {
		if args[i] == "--activity" {
			activity = args[i+1]
		} else if args[i] == "--working-dir" {
			workingDir = args[i+1]
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

	var pushNotifier service.Notifier
	if cfg.Notifications.Ntfy.Topic != "" {
		pushNotifier = service.NewNtfyNotifier(cfg.Notifications.Ntfy.URL, cfg.Notifications.Ntfy.Topic, cfg.Notifications.Ntfy.Token)
	}
	var osNotifier service.Notifier
	if cfg.Notifications.OS.Enabled {
		osNotifier = service.NewOSNotifier()
	}
	if err := cli.AgentState(db, taskID, activity, workingDir, pushNotifier, osNotifier); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runAgentSummary(args []string) int {
	// Parse: legato agent summary [--exclude <task-id>]
	var excludeTaskID string
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--exclude" {
			excludeTaskID = args[i+1]
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

	out, err := cli.AgentSummary(db, excludeTaskID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Print(out)
	return 0
}

func runAgentStatus(args []string) int {
	// Parse: legato agent status <task-id> --format tmux
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: legato agent status <task-id> --format tmux\n")
		return 1
	}

	taskID := args[0]
	format := ""
	for i := 1; i < len(args)-1; i++ {
		if args[i] == "--format" {
			format = args[i+1]
			break
		}
	}
	if format == "" {
		fmt.Fprintf(os.Stderr, "usage: legato agent status <task-id> --format tmux\n")
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

	out, err := cli.AgentStatus(db, taskID, format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Print(out)
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
	registry.Register(hooks.NewChimeraAdapter(legatoBin))
	registry.Register(hooks.NewCodexAdapter(legatoBin))
	registry.Register(hooks.NewYggdrasilAdapter(legatoBin))

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

func runServeCmd(args []string) int {
	port := "3080"
	for i, a := range args {
		if a == "--port" && i+1 < len(args) {
			port = args[i+1]
		}
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	if err := config.ValidateConductorTier(cfg); err != nil {
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

	bus := events.New()
	attachmentCache := attachments.NewCache(attachments.DefaultRoot(), cfg.Jira.AttachmentMaxSizeBytes)
	boardSvc := service.NewBoardService(db, bus, attachmentCache)
	wd, _ := os.Getwd()

	// Set up agent service (tmux may not be installed).
	var agentSvc service.AgentService
	var tmuxMgr service.TmuxManager
	if mgr, tmuxErr := tmux.New(tmux.Options{}); tmuxErr == nil {
		tmuxMgr = mgr
		var notifier service.Notifier
		if cfg.Notifications.Ntfy.Topic != "" {
			notifier = service.NewNtfyNotifier(cfg.Notifications.Ntfy.URL, cfg.Notifications.Ntfy.Topic, cfg.Notifications.Ntfy.Token)
		}
		var osNotifier service.Notifier
		if cfg.Notifications.OS.Enabled {
			osNotifier = service.NewOSNotifier()
		}
		agentSvc = service.NewAgentService(db, mgr, wd, service.AgentServiceOptions{
			Notifier:   notifier,
			OSNotifier: osNotifier,
		})
	}

	// Swarm service for HTTP endpoints.
	var swarmSvc service.SwarmService
	if agentSvc != nil {
		swarmCfg := service.SwarmConfig{
			MaxConcurrentAgents: cfg.Swarm.MaxConcurrentAgents,
			MaxSubtasksPerPlan:  cfg.Swarm.MaxSubtasksPerPlan,
			MaxStepsPerPlan:     cfg.Swarm.MaxStepsPerPlan,
			StrictScope:         cfg.Swarm.StrictScope,
			RequireUserClose:    cfg.Swarm.RequireUserClose,
			DefaultAgent:        cfg.Swarm.DefaultAgent,
			ConductorAgent:      cfg.Swarm.ConductorAgent,
			ConductorTier:       cfg.Swarm.ConductorTier,
			TierCatalog:         tierCatalog(cfg.Adapters),
			ValidateOptions:     buildValidateOptions(cfg),
		}
		swarmSvc = service.NewSwarmService(db, agentSvc, bus, swarmCfg, wd, attachmentCache)
	}

	addr := ":" + port
	srv := server.NewWithSwarm(boardSvc, agentSvc, tmuxMgr, addr, swarmSvc, bus, wd)
	srv.SetMacros(cfg.Macros)
	srv.SetNtfyConfigured(cfg.Notifications.Ntfy.Topic != "")
	srvWindow, srvBuckets := resolveSparklineWindow(cfg)
	srv.SetSparklineWindow(srvWindow, srvBuckets)

	// Configure TLS.
	certFile, keyFile, caCertFile := resolveTLS(cfg)
	if certFile != "" && keyFile != "" {
		srv.SetTLS(certFile, keyFile)
	}
	if caCertFile != "" {
		srv.SetCACertPath(caCertFile)
	}

	// Auth token — auto-generated on first run.
	dataDir := filepath.Dir(config.ResolveDBPath(cfg))
	if token, err := auth.EnsureToken(dataDir); err != nil {
		log.Printf("auth: %v (web UI will run without auth)", err)
	} else {
		srv.SetAuthToken(token)
	}

	// IPC server for receiving CLI→web updates.
	socketPath := ipc.SocketPath()
	ipcSrv, ipcErr := ipc.NewServer(socketPath, func(msg ipc.Message) {
		switch msg.Type {
		case "task_update", "task_note", "agent_state", "pr_linked":
			srv.NotifyAgentsChanged()
		}
	})
	if ipcErr == nil {
		defer ipcSrv.Close()
	}

	// Handle SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		srv.Stop(context.Background())
	}()

	if swarmSvc != nil {
		swarmStop := swarmSvc.StartEventLoop(context.Background())
		defer swarmStop()
	}
	srv.StartSwarmEvents()
	srv.StartBoardEvents()

	scheme := "http"
	if certFile != "" {
		scheme = "https"
	}
	fmt.Printf("Legato web UI: %s://localhost:%s\n", scheme, port)
	if err := srv.Start(); err != nil && err.Error() != "http: Server closed" {
		fmt.Fprintf(os.Stderr, "server: %v\n", err)
		return 1
	}
	return 0
}

func runTUI() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	if err := config.ValidateConductorTier(cfg); err != nil {
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
		if err := config.ValidateConductorTier(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "config: %v\n", err)
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
	attachmentCache := attachments.NewCache(attachments.DefaultRoot(), cfg.Jira.AttachmentMaxSizeBytes)
	boardSvc := service.NewBoardService(db, bus, attachmentCache)

	// Start IPC server for CLI→TUI communication.
	// webSrv is set later if web.enabled — the closure captures the pointer.
	var webSrv *server.Server
	socketPath := ipc.SocketPath()
	ipcServer, ipcErr := ipc.NewServer(socketPath, func(msg ipc.Message) {
		var wn webNotifier
		if webSrv != nil {
			wn = webSrv
		}
		handleIPCMessage(msg, bus, wn)
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
		syncSvc = service.NewSyncService(db, bus, provider, cfg.Jira.JQLFilter, cfg.Jira.ProjectKeys, interval, attachmentCache)

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
	var tmuxMgr service.TmuxManager
	wd, _ := os.Getwd()
	escapeKey := cfg.Agents.EscapeKey
	if escapeKey == "" {
		escapeKey = "ctrl+]"
	}
	if mgr, tmuxErr := tmux.New(tmux.Options{EscapeKey: tmuxEscapeKey(escapeKey)}); tmuxErr == nil {
		tmuxMgr = mgr

		// Configure AI tool adapter for env var injection.
		legatoBin, binErr := os.Executable()
		if binErr != nil {
			legatoBin = "legato"
		}
		ccAdapter := hooks.NewClaudeCodeAdapter(legatoBin)
		if overrides := buildAdapterRoleOverrides(cfg, ccAdapter.Name()); overrides != nil {
			ccAdapter.SetRoleOverrides(overrides)
		}
		if a, ok := cfg.Adapters[ccAdapter.Name()]; ok {
			if len(a.LaunchArgs) > 0 {
				ccAdapter.SetLaunchArgs(a.LaunchArgs)
			}
			if tierArgs := adapterTierLaunchArgs(a); tierArgs != nil {
				ccAdapter.SetTiers(tierArgs)
			}
		}
		chimeraAdapter := hooks.NewChimeraAdapter(legatoBin)
		if overrides := buildAdapterRoleOverrides(cfg, chimeraAdapter.Name()); overrides != nil {
			chimeraAdapter.SetRoleOverrides(overrides)
		}
		if a, ok := cfg.Adapters[chimeraAdapter.Name()]; ok {
			if len(a.LaunchArgs) > 0 {
				chimeraAdapter.SetLaunchArgs(a.LaunchArgs)
			}
			// SetModes only when the user explicitly configured the field;
			// nil-Modes means "fall back to defaults" inside the adapter.
			if a.Modes != nil {
				chimeraAdapter.SetModes(a.Modes)
			}
			if tierArgs := adapterTierLaunchArgs(a); tierArgs != nil {
				chimeraAdapter.SetTiers(tierArgs)
			}
		}
		codexAdapter := hooks.NewCodexAdapter(legatoBin)
		if overrides := buildAdapterRoleOverrides(cfg, codexAdapter.Name()); overrides != nil {
			codexAdapter.SetRoleOverrides(overrides)
		}
		if a, ok := cfg.Adapters[codexAdapter.Name()]; ok {
			if len(a.LaunchArgs) > 0 {
				codexAdapter.SetLaunchArgs(a.LaunchArgs)
			}
			if tierArgs := adapterTierLaunchArgs(a); tierArgs != nil {
				codexAdapter.SetTiers(tierArgs)
			}
		}

		// Pick the default adapter from cfg.Swarm.DefaultAgent. The full
		// registry is passed via Adapters so per-sub-task `agent:` overrides
		// in the conductor's plan can resolve any registered name.
		var defaultAdapter service.AIToolAdapter = ccAdapter
		switch cfg.Swarm.DefaultAgent {
		case chimeraAdapter.Name():
			defaultAdapter = chimeraAdapter
		case codexAdapter.Name():
			defaultAdapter = codexAdapter
		case ccAdapter.Name(), "":
			defaultAdapter = ccAdapter
		}
		adapters := map[string]service.AIToolAdapter{
			ccAdapter.Name():      ccAdapter,
			chimeraAdapter.Name(): chimeraAdapter,
			codexAdapter.Name():   codexAdapter,
		}
		var notifier service.Notifier
		if cfg.Notifications.Ntfy.Topic != "" {
			notifier = service.NewNtfyNotifier(cfg.Notifications.Ntfy.URL, cfg.Notifications.Ntfy.Topic, cfg.Notifications.Ntfy.Token)
		}
		var osNotifier service.Notifier
		if cfg.Notifications.OS.Enabled {
			osNotifier = service.NewOSNotifier()
		}
		agentSvc = service.NewAgentService(db, tmuxMgr, wd, service.AgentServiceOptions{
			Adapter:           defaultAdapter,
			Adapters:          adapters,
			SocketPath:        socketPath,
			TmuxOptions:       cfg.Agents.TmuxOptions,
			PRService:         prSvc,
			BinaryPath:        legatoBin,
			EventBus:          service.AgentDiedPublisher{Bus: bus},
			BriefKickoffDelay: time.Duration(cfg.Swarm.BriefKickoffDelayMs) * time.Millisecond,
			Notifier:          notifier,
			OSNotifier:        osNotifier,
		})
	}

	icons := theme.NewIcons(cfg.Icons)
	editor := config.ResolveEditor(cfg)

	// Construct SwarmService before the optional web server.
	swarmCfg := service.SwarmConfig{
		MaxConcurrentAgents: cfg.Swarm.MaxConcurrentAgents,
		MaxSubtasksPerPlan:  cfg.Swarm.MaxSubtasksPerPlan,
		MaxStepsPerPlan:     cfg.Swarm.MaxStepsPerPlan,
		StrictScope:         cfg.Swarm.StrictScope,
		RequireUserClose:    cfg.Swarm.RequireUserClose,
		DefaultAgent:        cfg.Swarm.DefaultAgent,
		ConductorAgent:      cfg.Swarm.ConductorAgent,
		ConductorTier:       cfg.Swarm.ConductorTier,
		TierCatalog:         tierCatalog(cfg.Adapters),
		ValidateOptions:     buildValidateOptions(cfg),
	}
	swarmSvc := service.NewSwarmService(db, agentSvc, bus, swarmCfg, wd, attachmentCache)
	swarmStop := swarmSvc.StartEventLoop(context.Background())
	defer swarmStop()

	// Auto-start web server if configured and port is free.
	if cfg.Web.Enabled {
		addr := ":" + cfg.Web.Port
		ln, listenErr := net.Listen("tcp", addr)
		if listenErr != nil {
			log.Printf("web: port %s unavailable: %v", cfg.Web.Port, listenErr)
		} else {
			webSrv = server.NewWithSwarm(boardSvc, agentSvc, tmuxMgr, ln.Addr().String(), swarmSvc, bus, wd)
			webSrv.SetMacros(cfg.Macros)
			webSrv.SetSyncService(syncSvc)
			webSrv.SetPRTrackingService(prSvc)
			webSrv.SetNtfyConfigured(cfg.Notifications.Ntfy.Topic != "")
			webWindow, webBuckets := resolveSparklineWindow(cfg)
			webSrv.SetSparklineWindow(webWindow, webBuckets)
			certFile, keyFile, caCertFile := resolveTLS(cfg)
			if certFile != "" && keyFile != "" {
				webSrv.SetTLS(certFile, keyFile)
			}
			if caCertFile != "" {
				webSrv.SetCACertPath(caCertFile)
			}
			if token, err := auth.EnsureToken(filepath.Dir(dbPath)); err == nil {
				webSrv.SetAuthToken(token)
			}
			webSrv.StartSwarmEvents()
			webSrv.StartBoardEvents()
			go func() {
				if err := webSrv.Serve(ln); err != nil && err.Error() != "http: Server closed" {
					log.Printf("web server: %v", err)
				}
			}()
		}
	}

	// Load workspaces for TUI
	workspaces, _ := boardSvc.ListWorkspaces(context.Background())

	reportSvc := service.NewReportService(db)

	app := tui.NewApp(boardSvc, syncSvc, agentSvc, prSvc, reportSvc, icons, bus, editor, workspaces, tmuxMgr, wd, swarmSvc, cfg.Macros)
	app.SetGroupDefaults(cfg.Groups.Defaults)
	if cfg.Worktrees.Yggdrasil.Enabled {
		app.SetWorktreeService(service.NewWorktreeWorkflow(db, worktree.NewRunner("yg")))
	}
	appWindow, appBuckets := resolveSparklineWindow(cfg)
	app.SetSparklineWindow(appWindow, appBuckets)
	app.SetNtfyConfigured(cfg.Notifications.Ntfy.Topic != "")

	// Wire voice dictation when enabled in config. The same service instance
	// is shared between the TUI (recorder-based) and web (PCM-based) paths.
	var voiceSvc *service.VoiceService
	if cfg.Voice.Enabled && cfg.Voice.WhisperURL != "" {
		voiceSvc = service.NewVoiceService(cfg.Voice.WhisperURL, tmuxMgr, agentSvc, service.VoiceServiceOptions{
			AutoSend:  cfg.Voice.AutoSend,
			MicDevice: cfg.Voice.MicDevice,
		})
		app.SetVoiceService(voiceSvc, cfg.Voice.AutoSend, cfg.Voice.MicDevice)
	}

	// If the web server was auto-started, tell the TUI to show the indicator.
	if webSrv != nil {
		app.SetWebServerRunning(cfg.Web.Port)
		// Wire voice service into the web server for the web UI voice button.
		if voiceSvc != nil {
			webSrv.SetVoiceService(voiceSvc, cfg.Voice.AutoSend)
		}
	}

	// Silence log output — bubbletea owns the terminal in alt-screen mode
	// and stray log writes corrupt the UI.
	log.SetOutput(io.Discard)

	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	// Shut down the web server so the process can exit cleanly.
	if webSrv != nil {
		webSrv.Stop(context.Background())
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

// resolveSparklineWindow parses cfg.Agents.SparklineWindow. Invalid or empty
// values fall back to 10 minutes / 10 buckets.
func resolveSparklineWindow(cfg *config.Config) (time.Duration, int) {
	window := 10 * time.Minute
	if cfg.Agents.SparklineWindow != "" {
		if d, err := time.ParseDuration(cfg.Agents.SparklineWindow); err == nil && d > 0 {
			window = d
		}
	}
	return window, 10
}

// resolveTLS returns cert/key/CA paths. Explicit config takes priority;
// otherwise auto-generates self-signed certs in the data directory.
func resolveTLS(cfg *config.Config) (certFile, keyFile, caCertFile string) {
	if cfg.Web.TLS.Cert != "" && cfg.Web.TLS.Key != "" {
		return cfg.Web.TLS.Cert, cfg.Web.TLS.Key, ""
	}

	// Auto-generate self-signed certs.
	dataDir := resolveDataDir(cfg)
	var extraDNS []string
	if cfg.Web.TLS.Hostname != "" {
		extraDNS = append(extraDNS, cfg.Web.TLS.Hostname)
	}
	paths, err := certs.EnsureCerts(dataDir, extraDNS...)
	if err != nil {
		log.Printf("tls: auto-cert generation failed: %v", err)
		return "", "", ""
	}
	log.Printf("tls: using auto-generated certs (install CA on devices: %s)", paths.CACert)
	return paths.ServerCert, paths.ServerKey, paths.CACert
}

func resolveDataDir(cfg *config.Config) string {
	// Reuse the same base directory as the database.
	dbPath := config.ResolveDBPath(cfg)
	return filepath.Dir(dbPath)
}

func runAuthCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "usage: legato auth [token|regenerate]\n")
		return 1
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	dataDir := resolveDataDir(cfg)

	switch args[0] {
	case "token":
		token, err := auth.ReadToken(dataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			fmt.Fprintf(os.Stderr, "hint: start legato once to auto-generate a token\n")
			return 1
		}
		fmt.Println(token)
		return 0

	case "regenerate":
		token, err := auth.RegenerateToken(dataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		fmt.Println(token)
		fmt.Fprintln(os.Stderr, "Token regenerated. All paired devices must re-authenticate.")
		return 0

	default:
		fmt.Fprintf(os.Stderr, "unknown auth command: %s\n", args[0])
		fmt.Fprintf(os.Stderr, "usage: legato auth [token|regenerate]\n")
		return 1
	}
}

func runPairCmd(args []string) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	dataDir := resolveDataDir(cfg)

	token, err := auth.ReadToken(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		fmt.Fprintf(os.Stderr, "hint: start legato once to auto-generate a token\n")
		return 1
	}

	// Determine port.
	port := cfg.Web.Port
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--port" {
			port = args[i+1]
			break
		}
	}
	if port == "" {
		port = "3080"
	}

	// Determine scheme.
	scheme := "https"
	if cfg.Web.TLS.Cert == "" && cfg.Web.TLS.Key == "" {
		// Auto-generated certs — still HTTPS.
		scheme = "https"
	}

	// Build hostname — try configured hostname, fall back to system hostname.
	host := cfg.Web.TLS.Hostname
	if host == "" {
		host, _ = os.Hostname()
	}
	if host == "" {
		host = "localhost"
	}

	serverURL := fmt.Sprintf("%s://%s:%s", scheme, host, port)
	pairURI := fmt.Sprintf("legato://pair?url=%s&token=%s", serverURL, token)

	// Render QR code to terminal.
	qrterminal.GenerateWithConfig(pairURI, qrterminal.Config{
		Level:     qrterminal.L,
		Writer:    os.Stdout,
		BlackChar: qrterminal.BLACK,
		WhiteChar: qrterminal.WHITE,
		QuietZone: 2,
	})

	fmt.Println()
	fmt.Printf("Server: %s\n", serverURL)
	fmt.Printf("Token:  %s\n", token)
	fmt.Println()
	fmt.Println("Scan the QR code with the Legato PWA to pair, or copy the token above.")
	return 0
}

// buildAdapterRoleOverrides extracts the role overrides for a specific adapter
// from cfg.Swarm.Prompts. Returns nil when no overrides apply.
//
// Config layout: swarm.prompts.<role>.<adapter> = "<prompt text>".
func buildAdapterRoleOverrides(cfg *config.Config, adapterName string) hooks.RolePromptOverrides {
	if cfg == nil || len(cfg.Swarm.Prompts) == 0 {
		return nil
	}
	out := hooks.RolePromptOverrides{}
	for role, byAdapter := range cfg.Swarm.Prompts {
		if prompt, ok := byAdapter[adapterName]; ok && prompt != "" {
			out[role] = prompt
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// adapterTiersRegistry projects the user's adapter tier config into the
// name-set form ValidatePlan expects. Returns nil when no adapter has any
// tiers configured (signals "tier check disabled" to the validator).
func adapterTiersRegistry(adapters map[string]config.AdapterConfig) map[string]map[string]struct{} {
	if len(adapters) == 0 {
		return nil
	}
	out := make(map[string]map[string]struct{}, len(adapters))
	for name, ac := range adapters {
		if len(ac.Tiers) == 0 {
			continue
		}
		set := make(map[string]struct{}, len(ac.Tiers))
		for tier := range ac.Tiers {
			set[tier] = struct{}{}
		}
		out[name] = set
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// adapterTierLaunchArgs extracts the per-tier launch_args slices for one
// adapter, formatted for hooks.{ClaudeCode,Chimera}Adapter.SetTiers.
func adapterTierLaunchArgs(ac config.AdapterConfig) map[string][]string {
	if len(ac.Tiers) == 0 {
		return nil
	}
	out := make(map[string][]string, len(ac.Tiers))
	for name, tier := range ac.Tiers {
		out[name] = append([]string(nil), tier.LaunchArgs...)
	}
	return out
}

// buildValidateOptions assembles the swarm.ValidateOptions used by both
// `legato swarm propose-plan` (CLI guard) and `SwarmService.ApplyApprovedPlan`
// (defensive re-validation for the TUI/web edit-and-approve path). Reuses
// the same registered-adapter list legato exposes elsewhere so the two paths
// can never disagree about which agent names are valid.
func buildValidateOptions(cfg *config.Config) swarm.ValidateOptions {
	opts := swarm.ValidateOptions{
		RegisteredAdapters: []string{"claude-code", "chimera", "codex"},
	}
	if cfg == nil {
		return opts
	}
	opts.AdapterTiers = adapterTiersRegistry(cfg.Adapters)
	opts.DefaultAgent = cfg.Swarm.DefaultAgent
	if cfg.Swarm.MaxSubtasksPerPlan > 0 {
		opts.MaxSubtasks = cfg.Swarm.MaxSubtasksPerPlan
	} else {
		opts.MaxSubtasks = 10
	}
	if cfg.Swarm.MaxStepsPerPlan > 0 {
		opts.MaxSteps = cfg.Swarm.MaxStepsPerPlan
	} else {
		opts.MaxSteps = 10
	}
	return opts
}

// tierCatalog projects the user's tier config into the adapter→tier→description
// shape SwarmService uses to render the conductor brief.
func tierCatalog(adapters map[string]config.AdapterConfig) map[string]map[string]string {
	if len(adapters) == 0 {
		return nil
	}
	out := make(map[string]map[string]string, len(adapters))
	for name, ac := range adapters {
		if len(ac.Tiers) == 0 {
			continue
		}
		descs := make(map[string]string, len(ac.Tiers))
		for tier, tc := range ac.Tiers {
			descs[tier] = tc.Description
		}
		out[name] = descs
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// runSwarmCmd dispatches to the conductor and worker swarm subcommands.
func runSwarmCmd(args []string) int {
	if len(args) == 0 {
		swarmUsage()
		return 1
	}
	switch args[0] {
	// Conductor verbs (callable from any context, but conventionally the
	// conductor is the only delegator).
	case "validate-plan":
		return runSwarmValidatePlan(args[1:])
	case "create":
		return runSwarmCreate(args[1:])
	case "propose-plan":
		return runSwarmProposePlan(args[1:])
	case "cancel":
		return runSwarmCancel(args[1:])
	case "extend-plan":
		return runSwarmExtendPlan(args[1:])
	case "dispatch":
		return runSwarmDispatch(args[1:])
	case "message":
		return runSwarmMessage(args[1:])
	case "broadcast":
		return runSwarmBroadcast(args[1:])
	case "close":
		return runSwarmClose(args[1:])
	case "finish":
		return runSwarmFinish(args[1:])
	// Worker verbs (callable from any context — workers self-identify by
	case "next-step":
		return runSwarmNextStep(args[1:])
	// LEGATO_AGENT_ROLE; the verb itself doesn't enforce caller identity).
	case "progress":
		return runSwarmProgress(args[1:])
	case "question":
		return runSwarmQuestion(args[1:])
	case "built":
		return runSwarmBuilt(args[1:])
	// Read-only.
	case "status":
		return runSwarmStatus(args[1:])
	case "inbox":
		return runSwarmInbox(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown swarm subcommand: %s\n", args[0])
		swarmUsage()
		return 1
	}
}

func swarmUsage() {
	fmt.Fprintln(os.Stderr, `usage: legato swarm <verb> [args]

Conductor verbs:
  create "<goal>" [--working-dir DIR]
  validate-plan <plan-file>
  propose-plan <plan-file> [--auto-approve] [--timeout 5m]
  extend-plan <plan-file> [--auto-approve] [--timeout 5m]
  cancel <parent-id>
  dispatch <subtask-id>
  message <subtask-id> "<text>" [--urgent]
  broadcast <parent-id> "<text>" [--urgent]
  close <subtask-id>
  finish <parent-id> "<summary>"
  next-step <parent-id>

Worker verbs:
  progress <subtask-id> "<text>"
  question <subtask-id> "<text>"
  built <subtask-id>

Read-only:
  status <parent-id>
  inbox <parent-id>`)
}

// loadSwarmServiceForCLI builds a SwarmService backed by a real tmux Manager.
// CLI swarm verbs need a real tmux because they may dispatch agents (which
// requires creating sessions) or send-keys to live workers/conductor.
func loadSwarmServiceForCLI() (service.SwarmService, *store.Store, int) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return nil, nil, 1
	}
	if err := config.ValidateConductorTier(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return nil, nil, 1
	}
	dbPath := config.ResolveDBPath(cfg)
	db, err := store.New(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database: %v\n", err)
		return nil, nil, 1
	}
	wd, _ := os.Getwd()

	// Real tmux required for swarm CLI ops. If unavailable, error fast.
	tmuxMgr, tmuxErr := tmux.New(tmux.Options{})
	if tmuxErr != nil {
		fmt.Fprintf(os.Stderr, "tmux: %v\n", tmuxErr)
		db.Close()
		return nil, nil, 1
	}

	bus := events.New()
	legatoBin, _ := os.Executable()
	ccAdapter := hooks.NewClaudeCodeAdapter(legatoBin)
	if overrides := buildAdapterRoleOverrides(cfg, ccAdapter.Name()); overrides != nil {
		ccAdapter.SetRoleOverrides(overrides)
	}
	if a, ok := cfg.Adapters[ccAdapter.Name()]; ok {
		if len(a.LaunchArgs) > 0 {
			ccAdapter.SetLaunchArgs(a.LaunchArgs)
		}
		if tierArgs := adapterTierLaunchArgs(a); tierArgs != nil {
			ccAdapter.SetTiers(tierArgs)
		}
	}
	chimeraAdapter := hooks.NewChimeraAdapter(legatoBin)
	if overrides := buildAdapterRoleOverrides(cfg, chimeraAdapter.Name()); overrides != nil {
		chimeraAdapter.SetRoleOverrides(overrides)
	}
	if a, ok := cfg.Adapters[chimeraAdapter.Name()]; ok {
		if len(a.LaunchArgs) > 0 {
			chimeraAdapter.SetLaunchArgs(a.LaunchArgs)
		}
		if a.Modes != nil {
			chimeraAdapter.SetModes(a.Modes)
		}
		if tierArgs := adapterTierLaunchArgs(a); tierArgs != nil {
			chimeraAdapter.SetTiers(tierArgs)
		}
	}
	codexAdapter := hooks.NewCodexAdapter(legatoBin)
	if overrides := buildAdapterRoleOverrides(cfg, codexAdapter.Name()); overrides != nil {
		codexAdapter.SetRoleOverrides(overrides)
	}
	if a, ok := cfg.Adapters[codexAdapter.Name()]; ok {
		if len(a.LaunchArgs) > 0 {
			codexAdapter.SetLaunchArgs(a.LaunchArgs)
		}
		if tierArgs := adapterTierLaunchArgs(a); tierArgs != nil {
			codexAdapter.SetTiers(tierArgs)
		}
	}
	defaultAdapter := service.AIToolAdapter(ccAdapter)
	if cfg.Swarm.DefaultAgent == chimeraAdapter.Name() {
		defaultAdapter = chimeraAdapter
	} else if cfg.Swarm.DefaultAgent == codexAdapter.Name() {
		defaultAdapter = codexAdapter
	}
	adapters := map[string]service.AIToolAdapter{
		ccAdapter.Name():      ccAdapter,
		chimeraAdapter.Name(): chimeraAdapter,
		codexAdapter.Name():   codexAdapter,
	}
	agents := service.NewAgentService(db, tmuxMgr, wd, service.AgentServiceOptions{
		Adapter:           defaultAdapter,
		Adapters:          adapters,
		EventBus:          service.AgentDiedPublisher{Bus: bus},
		BriefKickoffDelay: time.Duration(cfg.Swarm.BriefKickoffDelayMs) * time.Millisecond,
	})

	swCfg := service.SwarmConfig{
		MaxConcurrentAgents: cfg.Swarm.MaxConcurrentAgents,
		MaxSubtasksPerPlan:  cfg.Swarm.MaxSubtasksPerPlan,
		MaxStepsPerPlan:     cfg.Swarm.MaxStepsPerPlan,
		StrictScope:         cfg.Swarm.StrictScope,
		RequireUserClose:    cfg.Swarm.RequireUserClose,
		DefaultAgent:        cfg.Swarm.DefaultAgent,
		ConductorAgent:      cfg.Swarm.ConductorAgent,
		ConductorTier:       cfg.Swarm.ConductorTier,
		TierCatalog:         tierCatalog(cfg.Adapters),
		ValidateOptions:     buildValidateOptions(cfg),
	}
	sw := service.NewSwarmService(db, agents, bus, swCfg, wd)
	return sw, db, 0
}

func runSwarmValidatePlan(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: legato swarm validate-plan <plan-file>")
		return 1
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	result, err := cli.SwarmValidatePlan(args[0], buildValidateOptions(cfg))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	data, merr := json.Marshal(result)
	if merr != nil {
		fmt.Fprintf(os.Stderr, "marshal result: %v\n", merr)
		return 1
	}
	fmt.Println(string(data))
	if !result.Valid {
		return 2
	}
	return 0
}

func runSwarmCreate(args []string) int {
	goal, workingDir, err := parseSwarmCreateArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "usage: legato swarm create \"<goal>\" [--working-dir DIR]")
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	taskID := os.Getenv("LEGATO_TASK_ID")
	if taskID == "" {
		fmt.Fprintln(os.Stderr, "error: legato swarm create must be run from an existing Legato agent session (LEGATO_TASK_ID is not set)")
		return 1
	}
	if workingDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "working directory: %v\n", err)
			return 1
		}
		workingDir = wd
	}

	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()
	if err := cli.SwarmCreate(sw, taskID, goal, workingDir); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("created adhoc swarm %s\n", taskID)
	return 0
}

func runSwarmProposePlan(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: legato swarm propose-plan <plan-file> [--auto-approve] [--timeout 5m]")
		return 1
	}
	planPath := args[0]
	autoApprove := false
	var timeout time.Duration
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--auto-approve":
			autoApprove = true
		case "--timeout":
			if i+1 < len(args) {
				if d, err := time.ParseDuration(args[i+1]); err == nil {
					timeout = d
					i++
				}
			}
		}
	}
	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()

	cfg, _ := config.Load()
	if err := cli.SwarmProposePlan(sw, planPath, autoApprove, timeout, buildValidateOptions(cfg)); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runSwarmCancel(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: legato swarm cancel <parent-id>")
		return 1
	}
	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()
	if err := cli.SwarmCancel(sw, args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runSwarmExtendPlan(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: legato swarm extend-plan <plan-file> [--auto-approve] [--timeout 5m]")
		return 1
	}
	planPath := args[0]
	autoApprove := false
	var timeout time.Duration
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--auto-approve":
			autoApprove = true
		case "--timeout":
			if i+1 < len(args) {
				if d, err := time.ParseDuration(args[i+1]); err == nil {
					timeout = d
					i++
				}
			}
		}
	}
	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()

	cfg, _ := config.Load()

	// Auto-detect the existing swarm's working directory from the plan's parent
	// so extension plans can omit working_dir.
	plan, perr := swarm.LoadPlan(planPath)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "load plan: %v\n", perr)
		return 1
	}
	fallbackWD := ""
	if parent, err := db.GetTask(context.Background(), plan.Swarm.ParentTaskID); err == nil && parent.SwarmWorkingDir != nil {
		fallbackWD = *parent.SwarmWorkingDir
	}

	if err := cli.SwarmExtendPlan(sw, planPath, fallbackWD, autoApprove, timeout, buildValidateOptions(cfg)); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runSwarmDispatch(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: legato swarm dispatch <subtask-id>")
		return 1
	}
	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()
	if err := cli.SwarmDispatch(sw, args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("dispatched %s\n", args[0])
	return 0
}

func runSwarmMessage(args []string) int {
	subtaskID, text, urgent, err := parseMessageArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, `usage: legato swarm message <subtask-id> "<text>" [--urgent]`)
		return 1
	}
	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()
	if err := cli.SwarmMessage(sw, subtaskID, text, urgent); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runSwarmBroadcast(args []string) int {
	parentID, text, urgent, err := parseMessageArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, `usage: legato swarm broadcast <parent-id> "<text>" [--urgent]`)
		return 1
	}
	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()
	if err := cli.SwarmBroadcast(sw, parentID, text, urgent); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runSwarmClose(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: legato swarm close <subtask-id>")
		return 1
	}
	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()
	if err := cli.SwarmClose(sw, args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("closed %s\n", args[0])
	return 0
}

func runSwarmFinish(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, `usage: legato swarm finish <parent-id> "<summary>"`)
		return 1
	}
	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()
	if err := cli.SwarmFinish(sw, args[0], args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("swarm %s finished\n", args[0])
	return 0
}

func runSwarmNextStep(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: legato swarm next-step <parent-id>")
		return 1
	}
	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()
	if err := cli.SwarmNextStep(sw, args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Println("advanced to next step")
	return 0
}
func runSwarmStatus(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: legato swarm status <parent-id>")
		return 1
	}
	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()
	if err := cli.SwarmStatus(sw, args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runSwarmInbox(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: legato swarm inbox <parent-id>")
		return 1
	}
	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()
	if err := cli.SwarmInbox(sw, args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runSwarmProgress(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, `usage: legato swarm progress <subtask-id> "<text>"`)
		return 1
	}
	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()
	if err := cli.SwarmProgress(sw, args[0], args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runSwarmQuestion(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, `usage: legato swarm question <subtask-id> "<text>"`)
		return 1
	}
	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()
	if err := cli.SwarmQuestion(sw, args[0], args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

func runSwarmBuilt(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: legato swarm built <subtask-id>")
		return 1
	}
	sw, db, code := loadSwarmServiceForCLI()
	if code != 0 {
		return code
	}
	defer db.Close()
	if err := cli.SwarmBuilt(sw, args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	fmt.Printf("sub-task %s marked built\n", args[0])
	return 0
}

// cliNoopTmux is a minimal TmuxManager used in CLI mode. CLI commands never
// spawn agents — sub-task assignment that requires spawning is delegated to
// running TUI/server instances via IPC broadcast. If AssignNext attempts to
// spawn here, it fails fast with a clear error.
type cliNoopTmux struct{}

func (c *cliNoopTmux) Spawn(name, workDir string, width, height int, envVars ...string) error {
	return fmt.Errorf("cli mode cannot spawn agents — start legato TUI to materialize this sub-task")
}
func (c *cliNoopTmux) Kill(name string) error                            { return nil }
func (c *cliNoopTmux) Capture(name string) (string, error)               { return "", nil }
func (c *cliNoopTmux) CaptureWithEscapes(name string) (string, error)    { return "", nil }
func (c *cliNoopTmux) Attach(name string) *exec.Cmd                      { return exec.Command("true") }
func (c *cliNoopTmux) ListSessions() ([]string, error)                   { return nil, nil }
func (c *cliNoopTmux) IsAlive(name string) (bool, error)                 { return false, nil }
func (c *cliNoopTmux) SendKeys(name, keys string) error                  { return nil }
func (c *cliNoopTmux) SendKey(name, key string) error                    { return nil }
func (c *cliNoopTmux) SendKeysLine(name, line string) error              { return nil }
func (c *cliNoopTmux) SendKeysMultiline(name, payload string) error      { return nil }
func (c *cliNoopTmux) SendKeysShellCommand(name, command string) error   { return nil }
func (c *cliNoopTmux) PipeOutput(name string) (io.Reader, func(), error) { return nil, func() {}, nil }
func (c *cliNoopTmux) SetEnv(sessionName, key, value string) error       { return nil }
func (c *cliNoopTmux) SetOption(sessionName, key, value string) error    { return nil }
func (c *cliNoopTmux) PaneCommands() (map[string]string, error)          { return nil, nil }
