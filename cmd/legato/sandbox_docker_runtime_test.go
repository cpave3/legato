package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/sandbox"
	"github.com/cpave3/legato/internal/engine/store"
)

func socketClient(path string) *http.Client {
	return &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
	}}
}

// The host Legato serves a running sandboxed execution's Docker socket,
// passing what the policy allows to the daemon, and closes it when the
// execution ends.
func TestTheHostServesARunningExecutionsDockerSocket(t *testing.T) {
	runtime := shortDir(t)
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	daemonSock := filepath.Join(runtime, "d.sock")
	ln, err := net.Listen("unix", daemonSock)
	if err != nil {
		t.Fatal(err)
	}
	daemon := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"Version":"fake"}`)
	})}
	go daemon.Serve(ln)
	t.Cleanup(func() { daemon.Close() })

	cfg := &config.Config{DataDir: t.TempDir()}
	cfg.Agents.Sandbox.Enabled = true
	cfg.Agents.Sandbox.Docker = config.SandboxDockerConfig{Enabled: true, Host: "unix://" + daemonSock, Publish: "loopback"}
	db := channelsTestDB(t)
	ctx := context.Background()
	if err := db.CreateTask(ctx, store.Task{ID: "task-1", Title: "t", Status: "Doing"}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateExecution(ctx, store.Execution{ID: "ex-run", TaskID: "task-1", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	docker := sandboxDocker(cfg)
	if err := docker.WriteView("ex-run", sandbox.Plan{}); err != nil {
		t.Fatal(err)
	}

	host := startDockerHost(ctx, cfg, db, nil)
	t.Cleanup(host.Stop)
	socket := docker.SocketPath("ex-run")
	client := socketClient(socket)
	deadline := time.Now().Add(5 * time.Second)
	var resp *http.Response
	for {
		resp, err = client.Get("http://docker/v1.47/version")
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("the execution's socket does not answer: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != `{"Version":"fake"}` {
		t.Fatalf("version = %d %s", resp.StatusCode, body)
	}
	if info, err := os.Stat(socket); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket %v, %v; want it private", info, err)
	}

	if err := db.TerminalizeExecution(ctx, "ex-run", store.ExecutionSucceeded); err != nil {
		t.Fatal(err)
	}
	host.Reconcile(ctx)
	if _, err := client.Get("http://docker/_ping"); err == nil {
		t.Fatal("the socket of an ended execution still answers")
	}
}
