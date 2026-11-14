package cli_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/service"
)

// Faithfully copied CHIM-69 ordering, not a recording. See testdata/chimera-mod/README.md.
func TestChimeraLinkAutomaticApprovalAndHumanPrompt(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprint("broker=", remote), func(t *testing.T) {
			s := newTestStore(t)
			seedChimeraLinkAgent(t, s)
			var mu sync.Mutex
			pending := []map[string]string{}
			snapshots := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/sessions/chimera" {
					t.Errorf("unexpected path %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				mu.Lock()
				defer mu.Unlock()
				snapshots++
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "chimera", "pendingPermissions": pending})
			}))
			defer server.Close()
			var host cli.BridgeHost
			if remote {
				const token = "lgb_permission_regression"
				if err := s.SetExecutionBrokerToken(context.Background(), "native-link-task", service.HashBrokerToken(token)); err != nil {
					t.Fatal(err)
				}
				dir, err := os.MkdirTemp("", "cpr")
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
				host = cli.BrokerBridgeHost(socket, token)
			}
			reader, writer := io.Pipe()
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				done <- cli.RunChimeraLink(ctx, s, cli.ChimeraLinkOptions{TaskID: "link-task", ExecutionID: "native-link-task", Stdin: reader, Host: host})
			}()
			defer func() {
				cancel()
				writer.Close()
				reader.Close()
				select {
				case err := <-done:
					if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.ErrClosedPipe) {
						t.Error(err)
					}
				case <-time.After(4 * time.Second):
					t.Error("link did not exit")
				}
			}()
			seq := 0
			emit := func(kind string, fields map[string]any) {
				t.Helper()
				seq++
				if fields == nil {
					fields = map[string]any{}
				}
				fields["type"] = kind
				fields["seq"] = seq
				fields["sessionId"] = "chimera"
				raw, _ := json.Marshal(fields)
				if _, err := fmt.Fprintln(writer, string(raw)); err != nil {
					t.Fatal(err)
				}
			}
			activity := func(want string) {
				t.Helper()
				waitFor(t, want, func() bool {
					a, err := s.GetAgentSessionByTaskID(context.Background(), "link-task")
					return err == nil && a.Activity == want
				})
			}
			emit("session.start", map[string]any{"serverUrl": server.URL, "apiVersion": 1, "cwd": "/repo"})
			emit("turn.start", map[string]any{"source": "user", "turnId": "turn-1"})
			activity("working")
			// System One is still deciding; there is no operator prompt.
			emit("permission.request", map[string]any{"requestId": "auto", "toolName": "bash", "command": "sleep 600"})
			waitFor(t, "snapshot during automatic arbitration", func() bool { mu.Lock(); defer mu.Unlock(); return snapshots > 0 })
			activity("working")
			emit("permission.resolved", map[string]any{"requestId": "auto", "decision": "allow"})
			// A long foreground bash call and task-list updates emit no mod state events.
			time.Sleep(350 * time.Millisecond)
			activity("working")
			durations, err := s.GetExecutionDurations(context.Background(), "native-link-task")
			if err != nil {
				t.Fatal(err)
			}
			if _, waited := durations["waiting"]; waited {
				t.Fatalf("automatic approval recorded operator waiting: %v", durations)
			}
			// A failed automatic retry probe has no resolved event. It must not hold
			// a later, real human request open after that person answers.
			emit("permission.request", map[string]any{"requestId": "orphan", "toolName": "bash", "command": "go test ./..."})
			mu.Lock()
			pending = []map[string]string{{"requestId": "human", "tool": "bash", "command": "go test ./..."}}
			mu.Unlock()
			emit("permission.request", map[string]any{"requestId": "human", "toolName": "bash", "command": "go test ./..."})
			activity("waiting")
			emit("question.request", map[string]any{"requestId": "question"})
			mu.Lock()
			pending = nil
			mu.Unlock()
			emit("permission.resolved", map[string]any{"requestId": "human", "decision": "allow"})
			time.Sleep(350 * time.Millisecond)
			activity("waiting")
			emit("question.resolved", map[string]any{"requestId": "question"})
			activity("working")
			emit("turn.complete", map[string]any{"reason": "stop", "turnId": "turn-1"})
			activity("")
			writer.Close()
		})
	}
}
