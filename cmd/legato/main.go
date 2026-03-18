package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/tui"
)

func main() {
	bus := events.New()
	svc := &tui.FakeBoardService{}
	app := tui.NewApp(svc, bus)

	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
