package cli_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/store"
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
	srv, err := broker.Listen(socket, cli.NewBrokerHandler(s, cli.BrokerDeps{ClaudeRuntimeDir: t.TempDir()}))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	// The sandbox side only relays commands; the broker owns the bridge.
	reader, writer := io.Pipe()
	defer reader.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { err := cli.RunClaudeRelay(ctx, socket, token, writer); writer.CloseWithError(err); done <- err }()
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(reader)
		if scanner.Scan() {
			ready <- scanner.Text()
		}
		_, _ = io.Copy(io.Discard, reader)
	}()
	var announcement struct{ Type, URL string }
	select {
	case line := <-ready:
		if err := json.Unmarshal([]byte(line), &announcement); err != nil || announcement.Type != "ready" {
			t.Fatalf("ready = %q: %v", line, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("relay did not announce its host bridge")
	}
	endpoint := announcement.URL
	stop := func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Second):
			t.Fatal("relay did not stop")
			return nil
		}
	}
	bound, _ := s.GetAgentSessionByTaskID(context.Background(), "task1")
	registration, err := s.NativeBridgeRegistration(context.Background(), session.ID)
	if err != nil || bound.ChimeraEndpoint == nil || *bound.ChimeraEndpoint != registration.ControlEndpoint || registration.PublicEndpoint != endpoint {
		t.Fatalf("endpoint = %v, want %s", bound.ChimeraEndpoint, endpoint)
	}

	// An interrupted turn, which no hook reports, reaches the host too.
	if err := s.UpdateAgentActivityForSession(context.Background(), session.ID, store.ActivityWorking); err != nil {
		t.Fatal(err)
	}
	if code, body := postClaudeIdentity(t, endpoint, `[{"type":"hello","sessionId":"claude-1","cwd":"/repo"},{"type":"turn_start","turnId":"t1"},{"type":"turn_complete","turnId":"t1","reason":"aborted"}]`); code != http.StatusNoContent {
		t.Fatalf("events = %d %s", code, body)
	}
	waitFor(t, "the interrupt reported through the broker", func() bool {
		after, err := s.GetAgentSessionByTaskID(context.Background(), "task1")
		return err == nil && after.Activity == "" && after.HookSource == ""
	})
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	unbound, _ := s.GetAgentSessionByTaskID(context.Background(), "task1")
	if unbound.ChimeraEndpoint != nil {
		t.Fatalf("still bound: %v", *unbound.ChimeraEndpoint)
	}
}
