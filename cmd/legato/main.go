package main

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/jira"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/tmux"
	"github.com/cpave3/legato/internal/service"
	"github.com/cpave3/legato/internal/setup"
	"github.com/cpave3/legato/internal/tui"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	dbPath := config.ResolveDBPath(cfg)
	db, err := store.New(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// First-run setup: seed columns if none exist
	mappings, err := db.ListColumnMappings(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "checking columns: %v\n", err)
		os.Exit(1)
	}
	if len(mappings) == 0 {
		adapter := &setup.StoreAdapter{S: db}
		jiraSetup := &setup.RealJiraSetup{}
		if err := setup.RunWizard(context.Background(), os.Stdout, os.Stdin, adapter, cfg.Board.Columns, jiraSetup); err != nil {
			fmt.Fprintf(os.Stderr, "setup: %v\n", err)
			os.Exit(1)
		}
		// Reload config in case Jira was configured
		cfg, err = config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "config reload: %v\n", err)
			os.Exit(1)
		}
	}

	bus := events.New()
	boardSvc := service.NewBoardService(db, bus)

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

	// Set up agent service (tmux may not be installed — that's OK, agent features just won't work)
	var agentSvc service.AgentService
	escapeKey := cfg.Agents.EscapeKey
	if escapeKey == "" {
		escapeKey = "ctrl+]"
	}
	tmuxMgr, tmuxErr := tmux.New(tmux.Options{EscapeKey: tmuxEscapeKey(escapeKey)})
	if tmuxErr == nil {
		wd, _ := os.Getwd()
		agentSvc = service.NewAgentService(db, tmuxMgr, wd)
	}

	app := tui.NewApp(boardSvc, syncSvc, agentSvc, bus)

	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// tmuxEscapeKey converts config format "ctrl+]" to tmux format "C-]".
func tmuxEscapeKey(key string) string {
	if len(key) > 5 && key[:5] == "ctrl+" {
		return "C-" + key[5:]
	}
	return key
}
