package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/store"
)

// runAgentBridgeContract hosts the Claude Code bridge. The Legato mod inside a
// Claude Code session spawns it and reads its stdout, so stdout carries
// commands only and every diagnostic goes to stderr.
func runAgentBridgeContract(args []string) int {
	const command = "agent.bridge"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("agent", "bridge"))
	if parseErr != nil {
		return renderCommandError(command, parseErr, false)
	}
	taskID, rest, idErr := resolveTaskID(parsed.Positionals)
	if idErr != nil {
		return renderCommandError(command, idErr, false)
	}
	if len(rest) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "agent bridge accepts one task ID"), false)
	}
	executionID := strings.TrimSpace(parsed.Values["execution-id"])
	if executionID == "" {
		executionID = strings.TrimSpace(os.Getenv("LEGATO_EXECUTION_ID"))
	}
	host, db, models, failed := bridgeHost(command, taskID, executionID, func(cfg *config.Config) []string { return cfg.Adapters["claude-code"].Models })
	if failed != 0 {
		return failed
	}
	if db != nil {
		defer db.Close()
	}

	// SIGPIPE is taken so a write to a mod that has gone returns an error and
	// the bridge unbinds itself, instead of the process dying mid-cleanup.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	signal.Ignore(syscall.SIGPIPE)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go cancelWhenOrphaned(ctx, cancel, os.Getppid(), os.Getppid, time.Second)

	err := cli.RunClaudeBridge(ctx, db, cli.ClaudeBridgeOptions{
		TaskID: taskID, ExecutionID: executionID, Stdout: os.Stdout,
		Models: models, Host: host,
		// Set in a session's environment to see what the mod and the bridge
		// said to each other.
		TracePath: os.Getenv("LEGATO_CLAUDE_BRIDGE_TRACE"),
	})
	if err != nil {
		return renderCommandError(command, commandDomainError(err), false)
	}
	return exitOK
}

// cancelWhenOrphaned ends the bridge when the Claude Code process that
// spawned it is gone. A killed parent leaves no signal behind; the bridge
// notices that it was handed to another parent.
func cancelWhenOrphaned(ctx context.Context, cancel context.CancelFunc, parent int, currentParent func() int, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if currentParent() != parent {
				cancel()
				return
			}
		}
	}
}

// bridgeHost is how a bridge reaches Legato. A session with a broker that
// answers uses it and reads neither configuration nor store; a session in
// the sandbox has nothing else. An unsandboxed session with no broker
// answering opens the store as before. models reads what the bridge needs
// of the configuration in that case. failed is an exit code, or 0.
func bridgeHost(command, taskID, executionID string, models func(*config.Config) []string) (host cli.BridgeHost, db *store.Store, chosen []string, failed int) {
	if socket, token, ok := brokerSession(); ok {
		host = cli.BrokerBridgeHost(socket, token)
		_, err := host.Session(context.Background(), taskID, executionID)
		switch {
		case err == nil:
			return host, nil, nil, 0
		case !errors.Is(err, broker.ErrUnreachable) || sandboxed():
			return nil, nil, nil, renderCommandError(command, commandDomainError(err), false)
		}
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, nil, renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, false)
	}
	db, err = openStore(cfg)
	if err != nil {
		return nil, nil, nil, renderCommandError(command, &commandError{Code: "database_unavailable", Message: err.Error(), Exit: exitEnvironment}, false)
	}
	return cli.StoreBridgeHost(db), db, models(cfg), 0
}
