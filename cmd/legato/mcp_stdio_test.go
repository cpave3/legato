package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/mcpstdio/mcpstdiotest"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newStdioServeFixture runs `legato mcp serve` for ex-1 (task1, in
// acme/app) against a host broker that offers the fake local server
// "design", approved by the operator, which acme/app agents may use.
func newStdioServeFixture(t *testing.T, repo string) *mcp.ClientSession {
	t.Helper()
	db, _, _, token := brokerFixture(t)
	ctx := context.Background()
	if err := db.SetCodeContext(ctx, "task1", &store.CodeContext{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	approvals := service.NewApprovalService(db, service.ApprovalServiceOptions{})
	conns := service.NewMCPConnectionService(db, service.MCPConnectionOptions{})
	conns.SetApprovals(approvals)
	credBroker, err := service.NewCredentialBroker(db, approvals, service.CredentialBrokerOptions{WaitTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	grants := service.NewMCPGrants(credBroker, conns, db)
	conns.SetGrants(grants)
	path, marker := mcpstdiotest.Command()
	k, v, _ := strings.Cut(marker, "=")
	_, err = conns.Add(ctx, service.MCPConnectionInput{Name: "design", Kind: "stdio", Command: path, Dir: t.TempDir(), Env: map[string]string{k: v},
		Rules: []service.MCPRule{{Tools: "*", Repository: "acme/app", Approve: "auto"}}})
	var pending *service.MCPApprovalRequired
	if !errors.As(err, &pending) {
		t.Fatalf("Add = %v", err)
	}
	if _, err := approvals.Decide(ctx, pending.ApprovalID, service.ApprovalDecision{Decision: service.ApprovalAllow, By: "operator"}); err != nil {
		t.Fatal(err)
	}
	upstream := service.NewMCPBroker(conns, grants, nil)
	t.Cleanup(upstream.Close)
	host := startBrokerHost(ctx, db, shortDir(t), brokerHostOptions{Deps: cli.BrokerDeps{
		Ops: cli.MCPBrokerOps(func(cli.BrokerCaller) cli.MCPOptions { return cli.MCPOptions{Store: db, Upstream: upstream} }),
	}})
	t.Cleanup(host.Stop)
	t.Setenv("LEGATO_TASK_ID", "task1")
	t.Setenv("LEGATO_EXECUTION_ID", "ex-1")
	t.Setenv("LEGATO_BROKER_SOCKET", host.SocketPath())
	t.Setenv("LEGATO_BROKER_TOKEN", token)
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	exited := make(chan int, 1)
	go func() { exited <- runMCPServe(nil, serverIn, serverOut) }()
	cctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "0"}, nil).
		Connect(cctx, &mcp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		<-exited
	})
	return session
}

func TestALocalServersPictureReachesTheAgentAsAnImage(t *testing.T) {
	session := newStdioServeFixture(t, "acme/app")
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "design__image", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || !strings.Contains(mcpResultText(result), "a picture") {
		t.Fatalf("result %+v", result)
	}
	var image *mcp.ImageContent
	for _, c := range result.Content {
		if i, ok := c.(*mcp.ImageContent); ok {
			image = i
		}
	}
	if image == nil || image.MIMEType != "image/png" || !bytes.Equal(image.Data, mcpstdiotest.PNG) {
		t.Fatalf("no image reached the agent: %+v", result.Content)
	}
}

func TestAnAgentWithoutAGrantDoesNotSeeALocalServer(t *testing.T) {
	session := newStdioServeFixture(t, "acme/other")
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools.Tools {
		if strings.HasPrefix(tool.Name, "design__") {
			t.Fatalf("an agent without a grant was offered %s", tool.Name)
		}
	}
}
