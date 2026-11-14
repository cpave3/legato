package cli_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/service"
)

func TestRecordedChimeraModEventsThroughStoreAndBroker(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	var manifest struct {
		Cases []struct {
			Name       string     `json:"name"`
			Activities []string   `json:"activities"`
			JobKinds   [][]string `json:"job_kinds"`
		} `json:"cases"`
	}
	raw, err := os.ReadFile("testdata/chimera-mod/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range manifest.Cases {
		for _, remote := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/broker=%v", fixture.Name, remote), func(t *testing.T) {
				s := newTestStore(t)
				seedChimeraLinkAgent(t, s)
				// The permission recording was manually answered. CHIM-69's
				// interception alone does not say when its dialog appeared;
				// faithfully supply that human-pending snapshot separately.
				var mu sync.Mutex
				var permissionID string
				endpointServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					pending := []map[string]string{}
					if permissionID != "" {
						pending = append(pending, map[string]string{"requestId": permissionID})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"pendingPermissions": pending})
				}))
				defer endpointServer.Close()
				var host cli.BridgeHost
				if remote {
					const token = "lgb_recorded_chimera"
					if err := s.SetExecutionBrokerToken(context.Background(), "native-link-task", service.HashBrokerToken(token)); err != nil {
						t.Fatal(err)
					}
					dir, err := os.MkdirTemp("", "cfr")
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { os.RemoveAll(dir) })
					socket := filepath.Join(dir, "b.sock")
					server, err := broker.Listen(socket, cli.NewBrokerHandler(s, cli.BrokerDeps{}))
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { server.Close() })
					host = cli.BrokerBridgeHost(socket, token)
				}
				input, err := os.Open(filepath.Join("testdata/chimera-mod", fixture.Name+".ndjson"))
				if err != nil {
					t.Fatal(err)
				}
				defer input.Close()
				reader, writer := io.Pipe()
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				result := make(chan error, 1)
				finished := make(chan struct{})
				go func() {
					defer close(finished)
					result <- cli.RunChimeraLink(ctx, s, cli.ChimeraLinkOptions{TaskID: "link-task", ExecutionID: "native-link-task", Stdin: reader, Host: host})
				}()
				t.Cleanup(func() {
					writer.Close()
					reader.Close()
					cancel()
					select {
					case <-finished:
					case <-time.After(time.Second):
						t.Error("link did not exit")
					}
				})
				var sessionID, endpoint string
				scanner := bufio.NewScanner(input)
				index := 0
				for scanner.Scan() {
					line := scanner.Text()
					var event struct {
						Type      string `json:"type"`
						SessionID string `json:"sessionId"`
						ServerURL string `json:"serverUrl"`
						Reason    string `json:"reason"`
						RequestID string `json:"requestId"`
					}
					if err := json.Unmarshal([]byte(line), &event); err != nil {
						t.Fatal(err)
					}
					if event.Type == "session.start" {
						sessionID, endpoint = event.SessionID, endpointServer.URL
						var record map[string]any
						if err := json.Unmarshal([]byte(line), &record); err != nil {
							t.Fatal(err)
						}
						record["serverUrl"] = endpoint
						raw, err := json.Marshal(record)
						if err != nil {
							t.Fatal(err)
						}
						line = string(raw)
					}
					if event.Type == "session.end" && event.Reason != "switch" {
						sessionID, endpoint = "", ""
					}
					if index >= len(fixture.Activities) || index >= len(fixture.JobKinds) {
						t.Fatal("recording has no expected observation")
					}
					activity, kinds := fixture.Activities[index], fixture.JobKinds[index]
					mu.Lock()
					if event.Type == "permission.request" {
						permissionID = event.RequestID
					}
					if event.Type == "permission.resolved" {
						permissionID = ""
					}
					mu.Unlock()
					if _, err := io.WriteString(writer, line+"\n"); err != nil {
						t.Fatal(err)
					}
					waitFor(t, fmt.Sprintf("%s record %d: %s", fixture.Name, index+1, event.Type), func() bool {
						current, err := s.GetAgentSessionByTaskID(context.Background(), "link-task")
						if err != nil || current.Activity != activity || current.HookSource != "mod" {
							return false
						}
						gotKinds := make([]string, len(current.BackgroundJobs))
						for i, job := range current.BackgroundJobs {
							gotKinds[i] = string(job.Kind)
						}
						if !reflect.DeepEqual(gotKinds, kinds) {
							return false
						}
						if sessionID == "" {
							return current.ChimeraEndpoint == nil && current.ChimeraSessionID == nil
						}
						return current.ChimeraSessionID != nil && *current.ChimeraSessionID == sessionID && current.ChimeraEndpoint != nil && *current.ChimeraEndpoint == endpoint
					})
					if event.Type == "session.start" {
						saved, err := s.GetTaskNativeSession(context.Background(), "link-task", "chimera")
						if err != nil || saved == nil || saved.ProviderSessionID != event.SessionID {
							t.Fatalf("saved identity=%+v error=%v", saved, err)
						}
					}
					index++
				}
				if err := scanner.Err(); err != nil {
					t.Fatal(err)
				}
				if index != len(fixture.Activities) {
					t.Fatalf("observed %d records, want %d", index, len(fixture.Activities))
				}
				writer.Close()
				if err := <-result; err != nil {
					t.Fatal(err)
				}
				current, err := s.GetAgentSessionByTaskID(context.Background(), "link-task")
				if err != nil || current.ChimeraEndpoint != nil || current.ChimeraSessionID != nil {
					t.Fatalf("binding remains after exit/EOF: %+v, %v", current, err)
				}
			})
		}
	}
}
