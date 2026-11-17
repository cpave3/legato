// Package mcpstdiotest is a fake MCP server that speaks over stdio, for
// tests of Legato's stdio MCP connections. A test binary runs itself as the
// server: its TestMain calls Serve first, which serves and exits when the
// process was started as one (Command), and returns otherwise.
package mcpstdiotest

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverEnv marks a test binary started as the fake server.
const serverEnv = "LEGATO_MCPSTDIO_FAKE_SERVER"

// PNG is the image the image tool returns: a 1x1 transparent PNG.
var PNG, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")

// Command is how to start the fake server: this test binary, and the
// variable that makes it serve. Pass the variable in the process's
// environment.
func Command() (path string, env string) {
	return os.Args[0], serverEnv + "=1"
}

type textIn struct {
	Text string `json:"text"`
}

type spawnIn struct {
	PIDFile string `json:"pid_file"`
}

// Serve serves the fake server and exits when this process was started as
// one; otherwise it returns at once.
func Serve() {
	if os.Getenv(serverEnv) != "1" {
		return
	}
	fmt.Fprintln(os.Stderr, "fake server starting")
	// FAKE_MCP_STARTS names a folder where each start leaves a file named
	// by its process ID, so a test can count starts and find them.
	if dir := os.Getenv("FAKE_MCP_STARTS"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, strconv.Itoa(os.Getpid())), nil, 0o600)
	}
	if v := os.Getenv("FAKE_MCP_FAIL"); v != "" {
		fmt.Fprintln(os.Stderr, "fake server cannot start: "+v)
		os.Exit(2)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "1"}, nil)
	text := func(s string) *mcp.CallToolResult {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
	}
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "Says the text back."}, func(_ context.Context, _ *mcp.CallToolRequest, in textIn) (*mcp.CallToolResult, any, error) {
		return text(in.Text), nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "pid", Description: "The server's process ID."}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return text(strconv.Itoa(os.Getpid())), nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "env", Description: "One environment variable."}, func(_ context.Context, _ *mcp.CallToolRequest, in textIn) (*mcp.CallToolResult, any, error) {
		v, ok := os.LookupEnv(in.Text)
		if !ok {
			return text("<unset>"), nil, nil
		}
		return text(v), nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "cwd", Description: "The working directory."}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		dir, _ := os.Getwd()
		return text(dir), nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "image", Description: "A picture."}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "a picture"}, &mcp.ImageContent{Data: PNG, MIMEType: "image/png"}}}, nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "crash", Description: "Exits without answering."}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		fmt.Fprintln(os.Stderr, "fake server crashing")
		os.Exit(3)
		return nil, nil, nil
	})
	// spawn starts a grandchild in a session of its own, as Playwright
	// starts Chromium, so stopping only the server's process group would
	// leave it running. Its PID is written to the file.
	mcp.AddTool(server, &mcp.Tool{Name: "spawn", Description: "Starts a detached helper."}, func(_ context.Context, _ *mcp.CallToolRequest, in spawnIn) (*mcp.CallToolResult, any, error) {
		cmd := exec.Command("sleep", "300")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return nil, nil, err
		}
		pid := strconv.Itoa(cmd.Process.Pid)
		if err := os.WriteFile(in.PIDFile, []byte(pid), 0o600); err != nil {
			return nil, nil, err
		}
		return text(pid), nil, nil
	})
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "fake server:", err)
	}
	os.Exit(0)
}
