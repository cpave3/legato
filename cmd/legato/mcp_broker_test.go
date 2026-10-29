package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPServeRelaysTheHostsToolsThroughTheBroker(t *testing.T) {
	db, _, _, token := brokerFixture(t)
	host := startBrokerHost(context.Background(), db, shortDir(t), brokerHostOptions{Deps: cli.BrokerDeps{
		Ops: cli.MCPBrokerOps(func(cli.BrokerCaller) cli.MCPOptions { return cli.MCPOptions{Store: db} }),
	}})
	t.Cleanup(host.Stop)
	// Broker mode reads no configuration: one that does not load proves it.
	bad := filepath.Join(t.TempDir(), "config.yaml")
	_ = os.WriteFile(bad, []byte("db: [not a map\n"), 0o600)
	t.Setenv("LEGATO_CONFIG", bad)
	t.Setenv("LEGATO_TASK_ID", "task1")
	t.Setenv("LEGATO_EXECUTION_ID", "ex-1")
	t.Setenv("LEGATO_BROKER_SOCKET", host.SocketPath())
	t.Setenv("LEGATO_BROKER_TOKEN", token)

	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	exited := make(chan int, 1)
	go func() { exited <- runMCPServe(nil, serverIn, serverOut) }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "0"}, nil).
		Connect(ctx, &mcp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = session.Close()
		<-exited
	}()
	if !strings.Contains(session.InitializeResult().Instructions, "Legato") {
		t.Fatalf("instructions = %q", session.InitializeResult().Instructions)
	}
	listed, err := session.ListTools(ctx, nil)
	if err != nil || len(listed.Tools) == 0 {
		t.Fatalf("tools = %v, %v", listed, err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "task_context"})
	if err != nil || result.IsError || !strings.Contains(mcpResultText(result), "task1") {
		t.Fatalf("task_context = %+v, %v", result, err)
	}
	note, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "add_task_note", Arguments: map[string]any{"body": "Found it: the cache was stale."}})
	if err != nil || note.IsError {
		t.Fatalf("add_task_note = %+v, %v", note, err)
	}
	notes, _ := db.ListTaskNotes(ctx, "task1")
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "cache was stale") {
		t.Fatalf("notes = %+v", notes)
	}
}
