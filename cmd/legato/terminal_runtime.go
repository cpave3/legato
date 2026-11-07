package main

import (
	"github.com/cpave3/legato/internal/engine/tmux"
	"github.com/cpave3/legato/internal/server"
)

type ptyAttacher interface {
	AttachPTY(name string, cols, rows int) (*tmux.PTY, error)
}

// terminalAttacher lets the web server attach a tmux client to an agent's
// session on a pty. A manager that cannot attach gives no terminal.
func terminalAttacher(m any) server.TerminalAttacher {
	a, ok := m.(ptyAttacher)
	if !ok || a == nil {
		return nil
	}
	return func(name string, cols, rows int) (server.TerminalSession, error) {
		p, err := a.AttachPTY(name, cols, rows)
		if err != nil {
			return nil, err
		}
		return p, nil
	}
}
