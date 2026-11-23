package cli_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// A Chimera agent a workflow started binds the native panel as an ordinary
// agent does, whether its run has a related card or none, in the sandbox
// (through the broker) or not. Its conversation is not its task's to
// resume, so the link saves no identity for it (LEG-539).
func TestChimeraLinkBindsWorkflowAgents(t *testing.T) {
	for _, related := range []bool{false, true} {
		for _, remote := range []bool{false, true} {
			t.Run(fmt.Sprintf("related=%v/broker=%v", related, remote), func(t *testing.T) {
				ctx := context.Background()
				s := newTestStore(t)
				seedTask(t, s, "wf-task", "Doing")
				seedTask(t, s, "card", "Doing")
				ownerID, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "wf-task", TmuxSession: "legato-wf-task", AgentKind: "chimera", Command: "shell", Status: "running"})
				if err != nil {
					t.Fatal(err)
				}
				// A run with no related card attributes its agent to no task.
				attribution := ""
				if related {
					attribution = "card"
				}
				if err := s.CreateExecution(ctx, store.Execution{
					ID: "wf-exec", TaskID: attribution, ExecutionTaskID: "wf-task", AgentKind: "chimera",
					Kind: store.ExecutionWorkflowAgent, Status: store.ExecutionRunning,
					AgentSessionID: &ownerID, StartedAt: time.Now().UTC(),
				}); err != nil {
					t.Fatal(err)
				}
				var host cli.BridgeHost
				if remote {
					token := "lgb_chimera_workflow"
					if err := s.SetExecutionBrokerToken(ctx, "wf-exec", service.HashBrokerToken(token)); err != nil {
						t.Fatal(err)
					}
					dir, err := os.MkdirTemp("", "clw")
					if err != nil {
						t.Fatal(err)
					}
					defer os.RemoveAll(dir)
					srv, err := broker.Listen(filepath.Join(dir, "b.sock"), cli.NewBrokerHandler(s, cli.BrokerDeps{}))
					if err != nil {
						t.Fatal(err)
					}
					defer srv.Close()
					host = cli.BrokerBridgeHost(filepath.Join(dir, "b.sock"), token)
				}
				r, w := io.Pipe()
				done := make(chan error, 1)
				go func() {
					done <- cli.RunChimeraLink(ctx, s, cli.ChimeraLinkOptions{TaskID: "wf-task", ExecutionID: "wf-exec", Stdin: r, Host: host})
				}()
				defer w.Close()
				if _, err := io.WriteString(w, `{"seq":1,"type":"session.start","sessionId":"wf-task","cwd":"/repo","serverUrl":"http://127.0.0.1:4444/secret","apiVersion":1}`+"\n"); err != nil {
					t.Fatal(err)
				}
				waitFor(t, "workflow agent's panel binding", func() bool {
					a, err := s.GetAgentSessionByTaskID(ctx, "wf-task")
					return err == nil && a.ChimeraSessionID != nil && *a.ChimeraSessionID == "wf-task" && a.ChimeraEndpoint != nil
				})
				select {
				case err := <-done:
					t.Fatalf("link ended while its agent runs: %v", err)
				default:
				}
				if _, err := io.WriteString(w, `{"seq":2,"type":"session.end","sessionId":"wf-task","reason":"exit"}`+"\n"); err != nil {
					t.Fatal(err)
				}
				if err := <-done; err != nil {
					t.Fatalf("link ended with error: %v", err)
				}
				for _, task := range []string{"wf-task", "card"} {
					if saved, err := s.GetTaskNativeSession(ctx, task, "chimera"); err != nil || saved != nil {
						t.Fatalf("workflow agent saved %s's identity: %+v, %v", task, saved, err)
					}
				}
			})
		}
	}
}
