package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/engine/tmux"
	"github.com/cpave3/legato/internal/service"
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

	bus := events.New()
	boardSvc := &tui.FakeBoardService{}

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

	app := tui.NewApp(boardSvc, agentSvc, bus)

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
