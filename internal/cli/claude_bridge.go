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
	// HandoverWait is how long a bridge already serving the task is given to
	// go away before this one is refused; zero means bridgeHandoverWait.
	HandoverWait time.Duration
	// Host is how the bridge reaches Legato: the broker in Legato's sandbox.
	// Nil means the store given to RunClaudeBridge.
	Host BridgeHost
}

// bridgeLivenessTimeout bounds the probe of an endpoint already bound to the task.
const bridgeLivenessTimeout = time.Second

// bridgeHandoverWait is how long the bridge of a session that just ended is
// given to unbind. A session sent to the background goes on in another Claude
// process, which starts its bridge while the old one is still leaving.
const bridgeHandoverWait = 10 * time.Second

const bridgeHandoverPoll = 100 * time.Millisecond

// RunClaudeBridge serves the native agent protocol for the Claude Code session
// that spawned it until ctx ends or that session stops reading its commands.
//
// While it runs, the task's agent session is bound to the bridge exactly as a
// Chimera agent is bound to its discovered endpoint, so Legato's messages,
// interrupts, permission decisions and the web panel all go through it.
func RunClaudeBridge(ctx context.Context, s *store.Store, opts ClaudeBridgeOptions) (runErr error) {
	if opts.TaskID == "" {
		return fmt.Errorf("task ID is required")
	}
	runtimeDir := opts.RuntimeDir
	if runtimeDir == "" {
		runtimeDir = ipc.SocketDir()
	}
	host := opts.Host
	if host == nil {
		host = StoreBridgeHost(s)
	}
	session, err := host.Session(ctx, opts.TaskID, opts.ExecutionID)
	if err != nil {
		return err
	}
	if session.Endpoint != "" && !endpointGone(ctx, session.Endpoint, opts.HandoverWait) {
		return fmt.Errorf("task %s is already bridged by a live session", opts.TaskID)
	}
	if len(opts.Models) == 0 {
		opts.Models = session.Models
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
	identityErr := make(chan error, 1)
	defer func() {
		select {
		case err := <-identityErr:
			runErr = err
		default:
		}
	}()
	recordIdentity := nativeIdentityRecorder(runCtx, host, opts.TaskID, session, "claude-code")
	bridge := claudebridge.New(claudebridge.Options{
		TaskID:   opts.TaskID,
		Token:    token,
		Models:   opts.Models,
		Commands: trace.commands(opts.Stdout),
		// The mod stopped reading: its Claude session is gone.
		OnDisconnect: cancel,
		OnIdentity: func(sessionID, cwd string) error {
			err := recordIdentity(sessionID, cwd)
			if err != nil {
				select {
				case identityErr <- err:
				default:
				}
			}
			return err
		},
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
	if err := host.Bind(ctx, endpoint); err != nil {
		return err
	}
	defer func() { _ = host.Unbind(context.Background(), endpoint) }()

	if err := bridge.Ready(endpoint); err != nil {
		return err
	}

	select {
	case err := <-identityErr:
		return err
	case <-runCtx.Done():
		return nil
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serving the bridge: %w", err)
	}
}

func nativeIdentityRecorder(ctx context.Context, host BridgeHost, taskID string, session BridgeSession, kind string) func(string, string) error {
	storageDir, storageErr := nativeSessionStorageDir(kind)
	return func(providerSessionID, cwd string) error {
		if !session.RecordsIdentity || providerSessionID == "" {
			return nil
		}
		if storageErr != nil {
			return fmt.Errorf("resolving %s session storage: %w", kind, storageErr)
		}
		// Provider exit must not cancel a save for an identity already observed.
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := host.SaveIdentity(saveCtx, kind, providerSessionID, cwd, storageDir); err != nil {
			return fmt.Errorf("could not save %s session %s for task %s; resume identity was not recorded: %w", kind, providerSessionID, taskID, err)
		}
		return nil
	}
}

// endpointGone reports whether nothing answers at endpoint, giving a bridge
// that is leaving up to wait to finish.
func endpointGone(ctx context.Context, endpoint string, wait time.Duration) bool {
	if wait <= 0 {
		wait = bridgeHandoverWait
	}
	deadline := time.Now().Add(wait)
	for endpointAlive(ctx, endpoint) {
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(bridgeHandoverPoll):
		}
	}
	return true
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
