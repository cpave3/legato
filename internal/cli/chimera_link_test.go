package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
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
	if err := cli.AgentSessionCreatedFor(s, "link-task", "ended-execution", "stale-execution"); err == nil {
		t.Fatal("accepted an identity report from a different execution")
	}
	if _, err := cli.AgentSessionCreatedFromHookFor(s, "link-task", "ended-execution", "stale-execution", strings.NewReader(`{"session_id":"stale-execution","session_name":"link-task"}`)); err == nil {
		t.Fatal("accepted a hook identity from a different execution")
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

func TestChimeraLinkRequiresAnEventSequence(t *testing.T) {
	s := newTestStore(t)
	seedChimeraLinkAgent(t, s)
	err := cli.RunChimeraLink(context.Background(), s, cli.ChimeraLinkOptions{TaskID: "link-task", Stdin: strings.NewReader(`{"type":"session.start","sessionId":"s","cwd":"/repo","serverUrl":"http://127.0.0.1:4444","apiVersion":1}` + "\n")})
	if err == nil {
		t.Fatal("accepted an unordered event stream")
	}
}

// Synthetic overlap and duplicate-sequence cases complement runtime recordings.
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

func TestChimeraLinkRetainsSubagentLifecycle(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/chimera-subagents.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Stages []struct {
			Label  string
			Mod    json.RawMessage
			Jobs   []string
			States map[string]string
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, remote := range []bool{false, true} {
		t.Run(fmt.Sprint("broker=", remote), func(t *testing.T) {
			s := newTestStore(t)
			seedChimeraLinkAgent(t, s)
			var host cli.BridgeHost
			if remote {
				token := "lgb_subagents"
				if err := s.SetExecutionBrokerToken(context.Background(), "native-link-task", service.HashBrokerToken(token)); err != nil {
					t.Fatal(err)
				}
				socket := filepath.Join(t.TempDir(), "broker.sock")
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
			for _, stage := range fixture.Stages {
				var compact bytes.Buffer
				if err := json.Compact(&compact, stage.Mod); err != nil {
					t.Fatal(err)
				}
				if _, err := fmt.Fprintln(w, compact.String()); err != nil {
					t.Fatal(err)
				}
				waitFor(t, stage.Label, func() bool {
					a, err := s.GetAgentSessionByTaskID(context.Background(), "link-task")
					if err != nil || a.ChimeraSessionID == nil {
						return false
					}
					var children []struct {
						ID        string `json:"subagentId"`
						Status    string `json:"status"`
						SessionID string `json:"sessionId"`
					}
					if err := json.Unmarshal([]byte(a.ChimeraSubagents), &children); err != nil {
						return false
					}
					got := map[string]string{}
					for _, child := range children {
						if child.SessionID != "parent" {
							return false
						}
						got[child.ID] = child.Status
					}
					var jobs []string
					for _, job := range a.BackgroundJobs {
						jobs = append(jobs, job.ID)
					}
					if stage.Label == "parent idle" && a.Activity != store.ActivityBackground {
						return false
					}
					return reflect.DeepEqual(got, stage.States) && strings.Join(jobs, ",") == strings.Join(stage.Jobs, ",")
				})
			}
			w.Close()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			a, _ := s.GetAgentSessionByTaskID(context.Background(), "link-task")
			if !strings.Contains(a.ChimeraSubagents, `"interrupted"`) {
				t.Fatal("EOF discarded terminal child evidence")
			}
		})
	}
}
