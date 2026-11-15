package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	enginetmux "github.com/cpave3/legato/internal/engine/tmux"
)

// verifyPaneDelivery is shared by service messages and the engine's raw-key
// boundary (macros, voice, swarm notices, and the operator's prompt bar).
func (a *agentService) verifyPaneDelivery(name string) (err error) {
	commands, err := a.tmux.PaneCommands()
	if err != nil {
		return fmt.Errorf("message not delivered: verifying foreground agent: %w", err)
	}
	return a.verifyPaneCommand(name, commands[name])
}

func (a *agentService) verifyPaneCommand(name, command string) (err error) {
	defer func() {
		if err != nil {
			log.Printf("agent input refused session=%s: %v", name, err)
		}
	}()
	ctx := context.Background()
	sessions, err := a.store.ListAgentSessions(ctx)
	if err != nil {
		return fmt.Errorf("message not delivered: reading agent session: %w", err)
	}
	for _, session := range sessions {
		if session.TmuxSession != name || session.Status != "running" {
			continue
		}

		if !agentForeground(session.AgentKind, command) {
			// Native tools may have a bridge/runtime in the foreground, but
			// they must never receive raw keys, regardless of that process.
			if !enginetmux.IsShellCommand(command) && (session.ChimeraEndpoint != nil || a.expectsNative(session)) {
				return fmt.Errorf("message not delivered: %s requires its native route; terminal input is disabled", name)
			}
			if command != "" && a.agentStartupOver(session) {
				status := store.AgentSessionInterrupted
				if enginetmux.IsShellCommand(command) {
					status = "dead"
				}
				if err := a.endGoneSession(ctx, session, status); err != nil {
					return fmt.Errorf("message not delivered; ending agent: %w", err)
				}
			}
			return fmt.Errorf("message not delivered: %s foreground is %q, not agent %q", name, command, session.AgentKind)
		}
		a.foregroundAgents.Store(session.ID, struct{}{})
		if session.ChimeraEndpoint != nil || a.expectsNative(session) {
			return fmt.Errorf("message not delivered: %s requires its native route; terminal input is disabled", name)
		}
		return nil
	}
	return fmt.Errorf("message not delivered: %s has no running agent", name)
}

func agentForeground(kind, command string) bool {
	command = filepath.Base(command)
	switch kind {
	case "claude-code":
		return command == "claude"
	case "chimera":
		return command == "chimera"
	case "codex":
		return command == "codex"
	case "shell", "":
		// A manually started tool may live in a shell session. The shell itself
		// is never a message receiver.
		return command == "claude" || command == "chimera" || command == "codex"
	default:
		return command == kind && !enginetmux.IsShellCommand(command)
	}
}

func (a *agentService) expectsNative(session store.AgentSession) bool {
	if session.NativeOff {
		return false
	}
	binder, ok := a.resolveAdapter(session.AgentKind).(NativeBindingAdapter)
	return ok && binder.BindsNativeEndpoint()
}

func (a *agentService) agentStartupOver(session store.AgentSession) bool {
	if session.AgentKind == "shell" || session.AgentKind == "" {
		return true
	}
	if _, seen := a.foregroundAgents.Load(session.ID); seen {
		return true
	}
	started, err := time.Parse(time.RFC3339, session.StartedAt)
	if err != nil {
		started, err = time.Parse("2006-01-02 15:04:05", session.StartedAt)
	}
	if err != nil || time.Since(started) < 30*time.Second {
		return false
	}
	// Clone setup has its own completion gate; don't mistake it for exit.
	a.setupsMu.Lock()
	status := a.setups[session.TaskID]
	a.setupsMu.Unlock()
	if status != "" {
		if _, err := os.Stat(status); err != nil && time.Since(started) < cloneSetupTimeout {
			return false
		}
	}
	return true
}
