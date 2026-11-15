package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
)

func runAgentLinkChimeraContract(args []string) int {
	const command = "agent.link.chimera"
	parsed, err := parseCommandArgs(args, commandFlagSpecs("agent", "link", "chimera"))
	if err != nil {
		return renderCommandError(command, err, false)
	}
	taskID, rest, idErr := resolveTaskID(parsed.Positionals)
	if idErr != nil {
		return renderCommandError(command, idErr, false)
	}
	if len(rest) > 0 {
		return renderCommandError(command, usageError("unexpected_argument", "agent link chimera accepts one task ID"), false)
	}
	executionID := parsed.Values["execution-id"]
	if executionID == "" {
		executionID = os.Getenv("LEGATO_EXECUTION_ID")
	}
	host, db, _, failed := bridgeHost(command, taskID, executionID, func(*config.Config) []string { return nil })
	if failed != 0 {
		return failed
	}
	if db != nil {
		defer db.Close()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go cancelWhenOrphaned(ctx, cancel, os.Getppid(), os.Getppid, time.Second)
	go func() { <-ctx.Done(); _ = os.Stdin.Close() }()
	if err := cli.RunChimeraLink(ctx, db, cli.ChimeraLinkOptions{TaskID: taskID, ExecutionID: executionID, Stdin: os.Stdin, Host: host}); err != nil {
		return renderCommandError(command, commandDomainError(err), false)
	}
	return exitOK
}
