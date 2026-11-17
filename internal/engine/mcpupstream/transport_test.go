package mcpupstream_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/mcpstdio"
	"github.com/cpave3/legato/internal/engine/mcpstdio/mcpstdiotest"
	"github.com/cpave3/legato/internal/engine/mcpupstream"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMain(m *testing.M) {
	mcpstdiotest.Serve()
	os.Exit(m.Run())
}

// stdioClient is a client whose every session is a new fake server process.
func stdioClient(t *testing.T) (*mcpupstream.Client, *atomic.Int32) {
	t.Helper()
	path, marker := mcpstdiotest.Command()
	var started atomic.Int32
	c := mcpupstream.New(mcpupstream.Options{Connect: func(context.Context) (mcp.Transport, error) {
		p, err := mcpstdio.Start(mcpstdio.Spec{Command: path, Env: []string{marker}, Dir: t.TempDir(), Grace: 100 * time.Millisecond})
		if err != nil {
			return nil, err
		}
		started.Add(1)
		return p.Transport(), nil
	}})
	t.Cleanup(c.Close)
	return c, &started
}

func callText(t *testing.T, r *mcpupstream.Result) string {
	t.Helper()
	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(r.Raw, &out); err != nil || len(out.Content) == 0 {
		t.Fatalf("result %s", r.Raw)
	}
	return out.Content[0].Text
}

func TestAClientCanSpeakToAServerOverAnyTransport(t *testing.T) {
	c, _ := stdioClient(t)
	ctx := context.Background()
	cat, err := c.Tools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r, err := c.Call(ctx, cat.Find("echo"), json.RawMessage(`{"text":"over stdio"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := callText(t, r); got != "over stdio" {
		t.Fatalf("echo = %q", got)
	}
	img, err := c.Call(ctx, cat.Find("image"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(img.Raw), `"type":"image"`) {
		t.Fatalf("the image did not come back: %s", img.Raw)
	}
}

func TestACallToAServerThatDiedFailsAndTheNextStartsAnother(t *testing.T) {
	c, started := stdioClient(t)
	ctx := context.Background()
	cat, err := c.Tools(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := c.Call(ctx, cat.Find("pid"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(ctx, cat.Find("crash"), nil); !errors.Is(err, mcpupstream.ErrSessionEnded) {
		t.Fatalf("crash: err = %v, want ErrSessionEnded", err)
	}
	second, err := c.Call(ctx, cat.Find("pid"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if callText(t, first) == callText(t, second) {
		t.Fatal("the call after the crash reached the same process")
	}
	if n := started.Load(); n != 2 {
		t.Fatalf("started %d processes, want 2", n)
	}
}

// A server built on the TypeScript SDK declares its schemas as draft-07.
func draft07Server(t *testing.T) func(context.Context) (mcp.Transport, error) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "ts", Version: "1"}, nil)
	server.AddTool(&mcp.Tool{Name: "get_block", InputSchema: json.RawMessage(`{"$schema":"http://json-schema.org/draft-07/schema#","type":"object","properties":{"block":{"type":"string"}},"required":["block"],"additionalProperties":false}`)},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "a block"}}}, nil
		})
	return func(ctx context.Context) (mcp.Transport, error) {
		client, srv := mcp.NewInMemoryTransports()
		go func() { _ = server.Run(context.Background(), srv) }()
		return client, nil
	}
}

func TestADraft07SchemaIsAcceptedOnlyWhenTheClientSaysSo(t *testing.T) {
	ctx := context.Background()
	strict := mcpupstream.New(mcpupstream.Options{Connect: draft07Server(t)})
	defer strict.Close()
	cat, err := strict.Tools(ctx)
	if err != nil || len(cat.Tools) != 0 || len(cat.Rejected) != 1 {
		t.Fatalf("strict: %+v %v", cat, err)
	}
	c := mcpupstream.New(mcpupstream.Options{Connect: draft07Server(t), AcceptDraft07: true})
	defer c.Close()
	cat, err = c.Tools(ctx)
	if err != nil || cat.Find("get_block") == nil {
		t.Fatalf("draft-07: %+v %v", cat, err)
	}
	if _, err := c.Call(ctx, cat.Find("get_block"), json.RawMessage(`{}`)); !errors.Is(err, mcpupstream.ErrInvalidArguments) {
		t.Fatalf("a call without the required block = %v", err)
	}
	r, err := c.Call(ctx, cat.Find("get_block"), json.RawMessage(`{"block":"cover"}`))
	if err != nil || callText(t, r) != "a block" {
		t.Fatalf("call = %v", err)
	}
}
