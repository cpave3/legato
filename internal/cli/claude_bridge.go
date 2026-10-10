package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/cpave3/legato/internal/engine/claudebridge"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

// ClaudeBridgeOptions configures one run of the Claude Code bridge.
type ClaudeBridgeOptions struct {
	// TaskID is the Legato task whose running Claude Code agent is bridged.
	TaskID string
	// ExecutionID, when set, must own that agent: a Claude process holding a
	// previous launch's environment is refused.
	ExecutionID string
	// Stdout carries commands to the Claude Code mod that spawned the bridge.
	Stdout io.Writer
	// RuntimeDir holds the bridge lockfile; empty means Legato's runtime directory.
	RuntimeDir string
	// Models are the model IDs the native panel offers beside the session's own.
	Models []string
	// TracePath, when set, records every event batch the mod posts and every
	// command the bridge writes, one JSON object per line. For debugging.
	TracePath string
}

// bridgeLivenessTimeout bounds the probe of an endpoint already bound to the task.
const bridgeLivenessTimeout = time.Second

// RunClaudeBridge serves the native agent protocol for the Claude Code session
// that spawned it until ctx ends or that session stops reading its commands.
//
// While it runs, the task's agent session is bound to the bridge exactly as a
// Chimera agent is bound to its discovered endpoint, so Legato's messages,
// interrupts, permission decisions and the web panel all go through it.
func RunClaudeBridge(ctx context.Context, s *store.Store, opts ClaudeBridgeOptions) error {
	if opts.TaskID == "" {
		return fmt.Errorf("task ID is required")
	}
	runtimeDir := opts.RuntimeDir
	if runtimeDir == "" {
		runtimeDir = ipc.SocketDir()
	}
	session, err := s.GetAgentSessionByTaskID(ctx, opts.TaskID)
	if err != nil {
		return fmt.Errorf("no running agent to bridge for task %s: %w", opts.TaskID, err)
	}
	if opts.ExecutionID != "" && session.ExecutionID != "" && session.ExecutionID != opts.ExecutionID {
		return fmt.Errorf("execution %s does not own the running agent for task %s", opts.ExecutionID, opts.TaskID)
	}
	if session.ChimeraEndpoint != nil && endpointAlive(ctx, *session.ChimeraEndpoint) {
		return fmt.Errorf("task %s is already bridged by a live session", opts.TaskID)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listening for the bridge: %w", err)
	}
	defer listener.Close()
	token, err := bridgeToken()
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/%s", port, token)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	trace, err := openBridgeTrace(opts.TracePath)
	if err != nil {
		return err
	}
	defer trace.Close()
	bridge := claudebridge.New(claudebridge.Options{
		TaskID:   opts.TaskID,
		Token:    token,
		Models:   opts.Models,
		Commands: trace.commands(opts.Stdout),
		// The mod stopped reading: its Claude session is gone.
		OnDisconnect: cancel,
	})
	defer bridge.Close()
	server := &http.Server{Handler: trace.handler(bridge.Handler())}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	defer func() {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		bridge.Close()
		_ = server.Shutdown(shutdownCtx)
	}()

	cwd, _ := os.Getwd()
	if err := claudebridge.WriteLockfile(runtimeDir, opts.TaskID, claudebridge.Lockfile{
		PID: os.Getpid(), Port: port, CWD: cwd, SessionID: opts.TaskID, URL: endpoint,
		StartedAt: time.Now().UnixMilli(), Version: "legato-claude-bridge/1",
	}); err != nil {
		return err
	}
	defer claudebridge.RemoveLockfile(runtimeDir, opts.TaskID, endpoint)

	// The session ID Legato binds is the task ID, not Claude's own: a /clear
	// continues under a new Claude session, and the binding must survive it.
	if err := s.UpdateChimeraDiscoveryForSession(ctx, session.ID, opts.TaskID, endpoint); err != nil {
		return fmt.Errorf("binding the bridge to task %s: %w", opts.TaskID, err)
	}
	defer func() {
		if cleared, err := s.ClearChimeraDiscoveryForSession(context.Background(), session.ID, opts.TaskID, endpoint); err == nil && cleared {
			ipc.Broadcast(ipc.Message{Type: "agents_changed", TaskID: opts.TaskID})
		}
	}()
	ipc.Broadcast(ipc.Message{Type: "agents_changed", TaskID: opts.TaskID})

	if err := bridge.Ready(endpoint); err != nil {
		return err
	}

	select {
	case <-runCtx.Done():
		return nil
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serving the bridge: %w", err)
	}
}

func endpointAlive(ctx context.Context, endpoint string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, bridgeLivenessTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, endpoint+"/v1/instance", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func bridgeToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating the bridge token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
