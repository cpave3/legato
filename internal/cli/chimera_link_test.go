package cli_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func seedChimeraLinkAgent(t *testing.T, s *store.Store) {
	t.Helper()
	seedTask(t, s, "link-task", "Doing")
	if err := s.InsertAgentSession(context.Background(), store.AgentSession{TaskID: "link-task", TmuxSession: "legato-link-task", AgentKind: "chimera", Command: "shell", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	a, _ := s.GetAgentSessionByTaskID(context.Background(), "link-task")
	linkNativeTestExecution(t, s, a)
}

func TestChimeraLinkBindsSwitchesAndReleases(t *testing.T) {
	s := newTestStore(t)
	seedChimeraLinkAgent(t, s)
	r, w := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- cli.RunChimeraLink(context.Background(), s, cli.ChimeraLinkOptions{TaskID: "link-task", Stdin: r})
	}()
	defer w.Close()
	write := func(line string) {
		t.Helper()
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"seq":1,"type":"session.start","sessionId":"link-task","cwd":"/repo","serverUrl":"http://127.0.0.1:4444/secret","apiVersion":1}`)
	waitFor(t, "initial mod binding", func() bool {
		a, e := s.GetAgentSessionByTaskID(context.Background(), "link-task")
		return e == nil && a.ChimeraSessionID != nil && *a.ChimeraSessionID == "link-task"
	})
	write(`{"seq":2,"type":"turn.start","sessionId":"link-task"}`)
	write(`{"seq":3,"type":"session.end","sessionId":"link-task","reason":"switch"}`)
	write(`{"seq":4,"type":"session.start","sessionId":"chimera-two","cwd":"/repo","serverUrl":"http://127.0.0.1:4444/secret","apiVersion":1}`)
	waitFor(t, "switched mod binding", func() bool {
		a, e := s.GetAgentSessionByTaskID(context.Background(), "link-task")
		return e == nil && a.ChimeraSessionID != nil && *a.ChimeraSessionID == "chimera-two" && a.Activity == ""
	})
	if err := cli.AgentSessionCreatedFor(s, "link-task", "native-link-task", "stale-hook"); err != nil {
		t.Fatal(err)
	}
	write(`{"seq":5,"type":"session.end","sessionId":"chimera-two","reason":"exit"}`)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	a, _ := s.GetAgentSessionByTaskID(context.Background(), "link-task")
	if a.ChimeraEndpoint != nil {
		t.Fatal("ended mod still bound")
	}
	saved, err := s.GetTaskNativeSession(context.Background(), "link-task", "chimera")
	if err != nil || saved == nil || saved.ProviderSessionID != "chimera-two" {
		t.Fatalf("saved identity = %+v, %v", saved, err)
	}
}

func TestChimeraLinkRefusesNonLoopback(t *testing.T) {
	s := newTestStore(t)
	seedChimeraLinkAgent(t, s)
	err := cli.RunChimeraLink(context.Background(), s, cli.ChimeraLinkOptions{TaskID: "link-task", Stdin: strings.NewReader(`{"seq":1,"type":"session.start","sessionId":"s","serverUrl":"http://example.com:4444","apiVersion":1}` + "\n")})
	if err == nil {
		t.Fatal("accepted remote endpoint")
	}
}

// Synthetic v1 shapes, pending a mod-capable Chimera recording.
func TestChimeraLinkKeepsStateOrderedThroughStoreAndBroker(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprint("broker=", remote), func(t *testing.T) {
			s := newTestStore(t)
			seedChimeraLinkAgent(t, s)
			owner, _ := s.GetAgentSessionByTaskID(context.Background(), "link-task")
			secretPath := filepath.Join(t.TempDir(), "secret")
			if err := os.WriteFile(secretPath, []byte("secret"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := s.SetChimeraLaunchSecret(context.Background(), owner.ID, "secret", secretPath); err != nil {
				t.Fatal(err)
			}
			var host cli.BridgeHost
			if remote {
				token := "lgb_chimera_link"
				if err := s.SetExecutionBrokerToken(context.Background(), "native-link-task", service.HashBrokerToken(token)); err != nil {
					t.Fatal(err)
				}
				dir, err := os.MkdirTemp("", "clb")
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
			r, w := io.Pipe()
			done := make(chan error, 1)
			go func() {
				done <- cli.RunChimeraLink(context.Background(), s, cli.ChimeraLinkOptions{TaskID: "link-task", ExecutionID: "native-link-task", Stdin: r, Host: host})
			}()
			defer w.Close()
			write := func(line string) {
				t.Helper()
				if _, err := io.WriteString(w, line+"\n"); err != nil {
					t.Fatal(err)
				}
			}
			write(`{"seq":1,"type":"session.start","sessionId":"chimera","cwd":"/repo","serverUrl":"http://127.0.0.1:4444/secret","apiVersion":1}`)
			write(`{"seq":2,"type":"turn.start","sessionId":"chimera"}`)
			write(`{"seq":3,"type":"permission.request","sessionId":"chimera","requestId":"p"}`)
			write(`{"seq":4,"type":"question.request","sessionId":"chimera","requestId":"q"}`)
			write(`{"seq":5,"type":"permission.resolved","sessionId":"chimera","requestId":"p"}`)
			waitFor(t, "one question still waiting", func() bool {
				a, e := s.GetAgentSessionByTaskID(context.Background(), "link-task")
				return e == nil && a.Activity == store.ActivityWaiting
			})
			write(`{"seq":6,"type":"question.resolved","sessionId":"chimera","requestId":"q"}`)
			write(`{"seq":7,"type":"turn.complete","sessionId":"chimera","reason":"error"}`)
			write(`{"seq":7,"type":"turn.start","sessionId":"chimera"}`)
			write(`{"seq":2,"type":"turn.start","sessionId":"chimera"}`)
			// A later observer is a barrier: duplicate/old turn events must not clear error.
			write(`{"seq":8,"type":"message.append","sessionId":"chimera"}`)
			w.Close()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			a, _ := s.GetAgentSessionByTaskID(context.Background(), "link-task")
			if a.Activity != store.ActivityError || a.ChimeraEndpoint != nil {
				t.Fatalf("after EOF = %+v", a)
			}
			saved, err := s.GetTaskNativeSession(context.Background(), "link-task", "chimera")
			if err != nil || saved == nil || saved.ProviderSessionID != "chimera" {
				t.Fatalf("saved=%+v error=%v", saved, err)
			}
			if _, err := os.Stat(secretPath); !os.IsNotExist(err) {
				t.Fatalf("bootstrap secret remains: %v", err)
			}
			if secret, _, err := s.GetChimeraLaunchSecret(context.Background(), owner.ID); err != nil || secret != "secret" {
				t.Fatalf("recovery secret lost: %q %v", secret, err)
			}
		})
	}
}
