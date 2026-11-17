package main

import (
	"os"
	"testing"

	"github.com/cpave3/legato/internal/engine/mcpstdio/mcpstdiotest"
	"github.com/cpave3/legato/internal/testenv"
)

func TestMain(m *testing.M) {
	// The fake local MCP server is this test binary, started as one.
	mcpstdiotest.Serve()
	os.Exit(testenv.Run(m.Run))
}
