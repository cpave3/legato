package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/service"
)

func TestChimeraLinkSurvivesABrokerOutageAtStartup(t *testing.T) {
	s := newTestStore(t)
	seedChimeraLinkAgent(t, s)
	const token = "lgb_chimera_retry"
	if err := s.SetExecutionBrokerToken(context.Background(), "native-link-task", service.HashBrokerToken(token)); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "clr")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "b.sock")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	input := `{"seq":1,"type":"session.start","sessionId":"retry-session","cwd":"/repo","serverUrl":"http://127.0.0.1:4444/secret","apiVersion":1}` + "\n"
	go func() {
		done <- cli.RunChimeraLink(ctx, s, cli.ChimeraLinkOptions{TaskID: "link-task", ExecutionID: "native-link-task", Stdin: strings.NewReader(input), Host: cli.BrokerBridgeHost(socket, token)})
	}()
	select {
	case err := <-done:
		t.Fatalf("link abandoned its stream during outage: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	srv, err := broker.Listen(socket, cli.NewBrokerHandler(s, cli.BrokerDeps{}))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	saved, err := s.GetTaskNativeSession(ctx, "link-task", "chimera")
	if err != nil || saved == nil || saved.ProviderSessionID != "retry-session" {
		t.Fatalf("saved=%+v error=%v", saved, err)
	}
}

func TestChimeraBridgeSessionDoesNotExposeServerSecret(t *testing.T) {
	s := newTestStore(t)
	seedChimeraLinkAgent(t, s)
	ctx := context.Background()
	if err := s.UpdateChimeraDiscovery(ctx, "link-task", "native", "http://127.0.0.1:4444/not-real-secret"); err != nil {
		t.Fatal(err)
	}
	const token = "lgb_chimera_private"
	if err := s.SetExecutionBrokerToken(ctx, "native-link-task", service.HashBrokerToken(token)); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "clp")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "b.sock")
	srv, err := broker.Listen(socket, cli.NewBrokerHandler(s, cli.BrokerDeps{}))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	session, err := cli.BrokerBridgeHost(socket, token).Session(ctx, "link-task", "native-link-task")
	if err != nil {
		t.Fatal(err)
	}
	if session.Endpoint != "" {
		t.Fatalf("agent-side endpoint.session exposed server authentication: %q", session.Endpoint)
	}
}
