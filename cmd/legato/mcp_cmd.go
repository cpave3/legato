package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/mcpserver"
	"github.com/cpave3/legato/internal/service"
)

// runMCPServeContract is `legato mcp serve`: Legato's tools for the agent of
// one session, as a stdio MCP server. An agent tool starts it as a child and
// speaks the protocol over its stdin and stdout, so stdout carries protocol
// messages only and every diagnostic goes to stderr.
func runMCPServeContract(args []string) int {
	return runMCPServe(args, os.Stdin, os.Stdout)
}

func runMCPServe(args []string, in io.ReadCloser, out io.WriteCloser) int {
	const command = "mcp.serve"
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("mcp", "serve"))
	if parseErr != nil {
		return renderCommandError(command, parseErr, false)
	}
	if len(parsed.Positionals) != 0 {
		return renderCommandError(command, usageError("unexpected_argument", "mcp serve accepts no positional arguments"), false)
	}
	// The session is the one whose pane this process was started in: Legato
	// puts both IDs in the pane's environment, and the agent tool hands that
	// environment (or these flags) to the servers it starts.
	taskID := strings.TrimSpace(parsed.Values["task"])
	if taskID == "" {
		taskID = strings.TrimSpace(os.Getenv("LEGATO_TASK_ID"))
	}
	executionID := strings.TrimSpace(parsed.Values["execution-id"])
	if executionID == "" {
		executionID = strings.TrimSpace(os.Getenv("LEGATO_EXECUTION_ID"))
	}

	cfg, err := config.Load()
	if err != nil {
		return renderCommandError(command, &commandError{Code: "config_invalid", Message: err.Error(), Exit: exitEnvironment}, false)
	}
	db, err := openStore(cfg)
	if err != nil {
		return renderCommandError(command, &commandError{Code: "database_unavailable", Message: err.Error(), Exit: exitEnvironment}, false)
	}
	defer db.Close()

	opts := cli.MCPOptions{
		Store: db, TaskID: taskID, ExecutionID: executionID,
		Version: mcpServerVersion(), ResourceDomains: cfg.Agents.MCP.ResourceDomains,
		ImageDir: agentUIImageRoot(cfg), VideoDir: agentUIVideoRoot(cfg),
	}
	// The peer tools reach other agents through tmux. Without it the rest of
	// the tools still work, so its absence costs those three and no more.
	if manager, tmuxErr := newAgentContractTmux(); tmuxErr == nil {
		workDir, _ := os.Getwd()
		opts.Agents = service.NewAgentService(db, manager, workDir, service.AgentServiceOptions{DisablePeerAwareness: true})
	} else {
		fmt.Fprintf(os.Stderr, "legato mcp serve: peer tools are unavailable: %v\n", tmuxErr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	// A client that goes away closes stdin, which ends Serve without an
	// error worth reporting; ctx ends it when the process is told to stop.
	if err := mcpserver.Serve(ctx, cli.MCPServerOptions(opts), in, out); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "legato mcp serve: %v\n", err)
	}
	return exitOK
}

// mcpServerVersion is the version this server reports to its client.
func mcpServerVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
