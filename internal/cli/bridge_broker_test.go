package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/service"
)

func TestTheClaudeBridgeBindsThroughTheBroker(t *testing.T) {
	s := newTestStore(t)
	session := seedRunningClaudeAgent(t, s, "task1")
	linkNativeTestExecution(t, s, session)
	token := "lgb_bridge"
	if err := s.SetExecutionBrokerToken(context.Background(), "native-task1", service.HashBrokerToken(token)); err != nil {
		t.Fatal(err)
	}
	dir, _ := os.MkdirTemp("", "brb")
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "b.sock")
	srv, err := broker.Listen(socket, cli.NewBrokerHandler(s, cli.BrokerDeps{}))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	// No store on the bridge's side: it reaches the host only through the broker.
	endpoint, stop := runningBridge(t, nil, cli.ClaudeBridgeOptions{
		TaskID: "task1", ExecutionID: "native-task1", RuntimeDir: t.TempDir(),
		Host: cli.BrokerBridgeHost(socket, token),
	})
	bound, _ := s.GetAgentSessionByTaskID(context.Background(), "task1")
	if bound.ChimeraEndpoint == nil || *bound.ChimeraEndpoint != endpoint {
		t.Fatalf("endpoint = %v, want %s", bound.ChimeraEndpoint, endpoint)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	unbound, _ := s.GetAgentSessionByTaskID(context.Background(), "task1")
	if unbound.ChimeraEndpoint != nil {
		t.Fatalf("still bound: %v", *unbound.ChimeraEndpoint)
	}
}
