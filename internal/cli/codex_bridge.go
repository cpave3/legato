package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/cpave3/legato/internal/engine/codexbridge"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

// CodexAppServer is the Codex app server one session's terminal UI and
// bridge are both clients of.
type CodexAppServer struct {
	// URL is its WebSocket address, and Token the bearer token it expects.
	URL   string
	Token string
	// Stop ends the server; Exited is closed when it has gone on its own.
	Stop   func()
	Exited <-chan struct{}
}

// CodexBridgeOptions configures one run of the Codex bridge.
type CodexBridgeOptions struct {
	// TaskID is the Legato task whose running Codex agent is bridged.
	TaskID string
	// ExecutionID, when set, must own that agent: a process holding a
	// previous launch's environment is refused.
	ExecutionID string
	// RuntimeDir holds the bridge's lockfile and the app server's token;
	// empty means Legato's runtime directory.
	RuntimeDir string
	// CodexBin is the codex executable; empty means "codex" on PATH.
	CodexBin string
	// ServerArgs are extra arguments for `codex app-server`: the session's
	// configuration overrides, which the server applies to every thread.
	ServerArgs []string
	// Ready is called once the app server is up and the bridge is connected
	// to it: the terminal UI can be started against the same server.
	Ready func(CodexAppServer)
	// StartServer starts the app server. Nil runs `codex app-server`.
	StartServer func(ctx context.Context) (CodexAppServer, error)
	// Host is how the bridge reaches Legato: the broker in Legato's sandbox.
	// Nil means the store given to RunCodexBridge.
	Host BridgeHost
}

// RunCodexBridge serves the native agent protocol for one Codex session
// until ctx ends or the session's app server does.
//
// It starts the Codex app server the session's terminal UI will attach to,
// joins it as a second client, and binds the task's agent session to itself
// exactly as a Chimera agent is bound to its discovered endpoint. Legato's
// messages, interrupts, decisions and the web panel then go through it.
func RunCodexBridge(ctx context.Context, s *store.Store, opts CodexBridgeOptions) (runErr error) {
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
	if session.Endpoint != "" && endpointAlive(ctx, session.Endpoint) {
		return fmt.Errorf("task %s is already bridged by a live session", opts.TaskID)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	startServer := opts.StartServer
	if startServer == nil {
		startServer = func(ctx context.Context) (CodexAppServer, error) {
			return startCodexAppServer(ctx, opts.CodexBin, opts.ServerArgs, codexbridge.StatePath(runtimeDir, opts.TaskID, ".token"))
		}
	}
	appServer, err := startServer(runCtx)
	if err != nil {
		return err
	}
	if appServer.Stop != nil {
		defer appServer.Stop()
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

	cwd, _ := os.Getwd()
	bridge := codexbridge.New(codexbridge.Options{
		TaskID: opts.TaskID, Token: token, AppServerURL: appServer.URL, AppServerToken: appServer.Token, CWD: cwd,
		OnIdentity: nativeIdentityRecorder(runCtx, host, opts.TaskID, session, "codex"),
	})
	if err := bridge.Connect(runCtx); err != nil {
		return err
	}
	defer bridge.Close()
	bridgeDone := make(chan struct{})
	var bridgeErr error
	go func() {
		bridgeErr = bridge.Run(runCtx)
		close(bridgeDone)
	}()
	defer func() {
		bridge.Close()
		<-bridgeDone
		var identityErr *codexbridge.IdentityError
		if errors.As(bridgeErr, &identityErr) {
			runErr = bridgeErr
		}
	}()

	server := &http.Server{Handler: bridge.Handler()}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	defer func() {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		bridge.Close()
		_ = server.Shutdown(shutdownCtx)
	}()

	if err := codexbridge.WriteLockfile(runtimeDir, opts.TaskID, codexbridge.Lockfile{
		PID: os.Getpid(), Port: port, CWD: cwd, SessionID: opts.TaskID, URL: endpoint,
		StartedAt: time.Now().UnixMilli(), Version: codexbridge.Version,
	}); err != nil {
		return err
	}
	defer codexbridge.RemoveLockfile(runtimeDir, opts.TaskID, endpoint)

	// The session ID Legato binds is the task ID, not the Codex thread's: a
	// new thread started in the terminal continues under the same binding.
	if err := host.Bind(ctx, endpoint); err != nil {
		return err
	}
	defer func() { _ = host.Unbind(context.Background(), endpoint) }()

	// The agent is bound before its terminal exists: the panel can show what
	// the terminal asks on its way up, and the bridge holds any message sent
	// meanwhile until the terminal has opened a thread.
	if opts.Ready != nil {
		opts.Ready(appServer)
	}

	select {
	case <-runCtx.Done():
		return nil
	case <-appServer.Exited:
		return nil
	case <-bridgeDone:
		var identityErr *codexbridge.IdentityError
		if errors.As(bridgeErr, &identityErr) {
			return bridgeErr
		}
		// The app server going away is how a session ends, not a failure.
		return nil
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serving the bridge: %w", err)
	}
}

var appServerListening = regexp.MustCompile(`listening on:\s*(ws://\S+)`)

// CodexAppServerAddress reads the WebSocket address `codex app-server`
// prints once it is listening.
func CodexAppServerAddress(output io.Reader) (string, error) {
	scanner := bufio.NewScanner(output)
	var seen []string
	for scanner.Scan() {
		line := scanner.Text()
		if match := appServerListening.FindStringSubmatch(line); match != nil {
			return match[1], nil
		}
		if len(seen) < 8 {
			seen = append(seen, line)
		}
	}
	return "", fmt.Errorf("the Codex app server never said where it listens; it printed: %q", seen)
}

// appServerStartTimeout bounds the wait for the app server to listen.
const appServerStartTimeout = 20 * time.Second

// startCodexAppServer runs `codex app-server` on a loopback port of its own
// choosing, behind a token only this session's clients are given.
func startCodexAppServer(ctx context.Context, codexBin string, serverArgs []string, tokenPath string) (CodexAppServer, error) {
	if codexBin == "" {
		codexBin = "codex"
	}
	token, err := bridgeToken()
	if err != nil {
		return CodexAppServer{}, err
	}
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
		return CodexAppServer{}, fmt.Errorf("creating the bridge state directory: %w", err)
	}
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		return CodexAppServer{}, fmt.Errorf("writing the app server token: %w", err)
	}

	args := append([]string{
		"app-server", "--listen", "ws://127.0.0.1:0",
		"--ws-auth", "capability-token", "--ws-token-file", tokenPath,
	}, serverArgs...)
	cmd := exec.Command(codexBin, args...)
	cmd.SysProcAttr = childOfBridge()
	reader, writer := io.Pipe()
	cmd.Stdout, cmd.Stderr = writer, writer
	if err := cmd.Start(); err != nil {
		os.Remove(tokenPath)
		return CodexAppServer{}, fmt.Errorf("starting the Codex app server: %w", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		writer.Close()
		close(exited)
	}()
	stop := func() {
		_ = cmd.Process.Kill()
		<-exited
		os.Remove(tokenPath)
	}

	found := make(chan string, 1)
	failed := make(chan error, 1)
	go func() {
		address, err := CodexAppServerAddress(reader)
		if err != nil {
			failed <- err
			return
		}
		found <- address
		// Keep the pipe drained so the server never blocks on its own log.
		_, _ = io.Copy(io.Discard, reader)
	}()
	select {
	case address := <-found:
		return CodexAppServer{URL: address, Token: token, Stop: stop, Exited: exited}, nil
	case err := <-failed:
		stop()
		return CodexAppServer{}, err
	case <-ctx.Done():
		stop()
		return CodexAppServer{}, ctx.Err()
	case <-time.After(appServerStartTimeout):
		stop()
		return CodexAppServer{}, fmt.Errorf("the Codex app server did not start listening within %s", appServerStartTimeout)
	}
}
