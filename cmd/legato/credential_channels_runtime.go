package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// The files of the channel host, in the runtime directory.
const (
	channelLockFile  = "credential-channels.lock"
	proxyAddressFile = "credential-proxy.json"
	sshSocketDirName = "ssh"
	wrapperDirName   = "bin"
)

// ghConfigDir is the empty gh configuration agent sessions get, or "" when
// the person lets agents' gh use their login.
func ghConfigDir(cfg *config.Config, dir string) string {
	if !cfg.CredentialChannels.GH.IsolateLogin() {
		return ""
	}
	return filepath.Join(dir, "gh-config")
}

// legatoBinary is this binary's path, which sessions run as git's
// credential helper.
func legatoBinary() string {
	if path, err := os.Executable(); err == nil {
		return path
	}
	return "legato"
}

// channelHost runs the credential channels for every Legato on the machine:
// the loopback proxy and an SSH agent socket for each running execution.
// One process holds the lock and serves them; the others give their agents
// the holder's address, and take over when it ends.
type channelHost struct {
	cfg      *config.Config
	db       *store.Store
	dir      string
	channels *service.CredentialChannels

	mu       sync.Mutex
	lock     *os.File
	owner    bool
	server   *http.Server
	proxyURL string
	ssh      *credential.SSHAgentServer
	sockets  map[string]io.Closer
	cancel   context.CancelFunc
	stopped  bool

	// reconcileSessions is the agent service's check for sessions whose
	// tmux session ended. legato serve has nothing else that runs it while
	// no browser watches a stream, and an ended session must lose its
	// socket, placeholders and grants.
	reconcileSessions func(context.Context) error
	lastSessionCheck  time.Time
}

// sessionCheckEvery is how often the host asks for ended sessions.
const sessionCheckEvery = 15 * time.Second

// startChannelHost builds the channels of the broker's credentials and,
// when this process can take the lock, serves them. dir is the runtime
// directory (ipc.SocketDir()). A broker with no proxied or ssh credential
// starts nothing.
func startChannelHost(ctx context.Context, cfg *config.Config, db *store.Store, broker *service.CredentialBroker, bus *events.Bus, dir string, reconcileSessions ...func(context.Context) error) *channelHost {
	h := &channelHost{cfg: cfg, db: db, dir: dir, sockets: map[string]io.Closer{}}
	if len(reconcileSessions) > 0 {
		h.reconcileSessions = reconcileSessions[0]
	}
	if broker == nil {
		return h
	}
	h.channels = service.NewCredentialChannels(broker, service.CredentialChannelsOptions{
		ProxyURL:     h.ProxyURL,
		SSHSocketDir: filepath.Join(dir, sshSocketDirName),
		KnownHosts:   cfg.CredentialChannels.SSHAgent.KnownHosts,
		OnSession:    h.sessionStarting,
		LegatoBin:    legatoBinary(),
		WrapperDir:   filepath.Join(dir, wrapperDirName),
		GHConfigDir:  ghConfigDir(cfg, dir),
	})
	if !h.channels.HasProxy() && !h.channels.HasSSH() {
		return h
	}
	if h.channels.HasSSH() {
		if warning := sshAgentOverride(runSSHG); warning != "" {
			log.Printf("credentials: %s", warning)
		}
	}
	ctx, h.cancel = context.WithCancel(ctx)
	h.tryOwn(ctx)
	go h.loop(ctx, bus)
	return h
}

// Channels are the channels, for the agent service. Nil without a broker.
func (h *channelHost) Channels() *service.CredentialChannels { return h.channels }

// Owner reports whether this process serves the channels.
func (h *channelHost) Owner() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.owner
}

// ProxyURL is the proxy's base address: this process's own, or the one the
// holder of the lock wrote.
func (h *channelHost) ProxyURL() string {
	h.mu.Lock()
	url := h.proxyURL
	h.mu.Unlock()
	if url != "" {
		return url
	}
	return readProxyURL(h.dir)
}

func (h *channelHost) loop(ctx context.Context, bus *events.Bus) {
	var started, died <-chan events.Event
	if bus != nil {
		s, d := bus.Subscribe(events.EventAgentStarted), bus.Subscribe(events.EventAgentDied)
		defer bus.Unsubscribe(s)
		defer bus.Unsubscribe(d)
		started, died = s, d
	}
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-started:
			if !ok {
				started = nil
				continue
			}
		case _, ok := <-died:
			if !ok {
				died = nil
				continue
			}
		case <-tick.C:
		}
		if !h.Owner() {
			h.tryOwn(ctx)
		}
		h.Reconcile(ctx)
	}
}

// tryOwn takes the lock and starts serving, when no other process does.
func (h *channelHost) tryOwn(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.owner || h.stopped {
		return
	}
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		log.Printf("credentials: %v", err)
		return
	}
	lock, err := os.OpenFile(filepath.Join(h.dir, channelLockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		log.Printf("credentials: %v", err)
		return
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return
	}
	h.lock, h.owner = lock, true
	if h.channels.HasProxy() {
		if err := h.serveProxy(); err != nil {
			log.Printf("credentials: the proxy does not start: %v", err)
		}
	}
	if h.channels.HasSSH() {
		h.ssh = h.channels.SSHServer()
	}
	go h.Reconcile(ctx)
}

func (h *channelHost) serveProxy() error {
	listener, err := net.Listen("tcp", h.cfg.CredentialChannels.HTTPProxy.Listen)
	if err != nil {
		return err
	}
	h.server = &http.Server{Handler: h.channels.Proxy(), ReadHeaderTimeout: 10 * time.Second}
	h.proxyURL = "http://" + listener.Addr().String()
	go func(server *http.Server) {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("credentials: proxy: %v", err)
		}
	}(h.server)
	data, _ := json.Marshal(proxyAddress{URL: h.proxyURL, PID: os.Getpid()})
	tmp := filepath.Join(h.dir, proxyAddressFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(h.dir, proxyAddressFile))
}

type proxyAddress struct {
	URL string `json:"url"`
	PID int    `json:"pid"`
}

// readProxyURL is the address the channel host wrote, "" when no live
// process serves a proxy.
func readProxyURL(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, proxyAddressFile))
	if err != nil {
		return ""
	}
	var a proxyAddress
	if json.Unmarshal(data, &a) != nil || a.URL == "" {
		return ""
	}
	if a.PID > 0 {
		if err := syscall.Kill(a.PID, 0); err != nil && !errors.Is(err, syscall.EPERM) {
			return "" // the host died without removing its file
		}
	}
	return a.URL
}

// sessionStarting opens a session's socket before the agent can use it,
// when this process serves the channels.
func (h *channelHost) sessionStarting(taskID, executionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.openLocked(credential.Execution{TaskID: taskID, ExecutionID: executionID})
}

func (h *channelHost) openLocked(execution credential.Execution) {
	if !h.owner || h.ssh == nil || h.sockets[execution.ExecutionID] != nil {
		return
	}
	closer, err := h.ssh.Listen(h.channels.SSHSocketPath(execution.ExecutionID), execution)
	if err != nil {
		log.Printf("credentials: SSH agent of %s: %v", execution.ExecutionID, err)
		return
	}
	h.sockets[execution.ExecutionID] = closer
}

// Reconcile opens a socket for each running execution and closes those of
// executions that ended, ending their placeholders too.
func (h *channelHost) Reconcile(ctx context.Context) {
	if !h.Owner() {
		return
	}
	h.mu.Lock()
	check := h.reconcileSessions
	due := time.Since(h.lastSessionCheck) >= sessionCheckEvery
	if check != nil && due {
		h.lastSessionCheck = time.Now()
	}
	h.mu.Unlock()
	if check != nil && due {
		_ = check(ctx)
	}
	running, err := h.db.ListRunningExecutions(ctx)
	if err != nil {
		return
	}
	live := map[string]bool{}
	h.mu.Lock()
	for _, e := range running {
		live[e.ID] = true
		h.openLocked(credential.Execution{TaskID: e.ExecutionTaskID, ExecutionID: e.ID, AgentKind: e.AgentKind})
	}
	var ended []string
	for id, closer := range h.sockets {
		if !live[id] {
			_ = closer.Close()
			delete(h.sockets, id)
			ended = append(ended, id)
		}
	}
	h.mu.Unlock()
	for _, id := range ended {
		_ = h.channels.EndSession(ctx, id)
	}
}

// Stop ends serving and lets another process take over.
func (h *channelHost) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return
	}
	h.stopped = true
	if h.cancel != nil {
		h.cancel()
	}
	if !h.owner {
		return
	}
	if h.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = h.server.Shutdown(ctx)
		cancel()
		_ = os.Remove(filepath.Join(h.dir, proxyAddressFile))
		h.proxyURL = ""
	}
	for id, closer := range h.sockets {
		_ = closer.Close()
		delete(h.sockets, id)
	}
	_ = syscall.Flock(int(h.lock.Fd()), syscall.LOCK_UN)
	_ = h.lock.Close()
	h.owner = false
}

// giveSessionsChannels makes an agent service give its sessions the
// channels' environment.
// It does so with no channel credential too: every session gets the
// variables that unlock Legato's secrets set empty.
func giveSessionsChannels(agents service.AgentService, channels *service.CredentialChannels) {
	if agents == nil || channels == nil {
		return
	}
	if setter, ok := agents.(interface {
		SetSessionEnv(service.SessionEnvProvider)
	}); ok {
		setter.SetSessionEnv(channels)
	}
}

// attachCredentialChannels gives the sessions of a command that spawns
// agents (a swarm or workflow command) their channels. The command serves
// none: the host Legato's proxy and sockets serve its agents.
func attachCredentialChannels(cfg *config.Config, db *store.Store, agents service.AgentService) {
	broker, err := configuredCredentialBroker(cfg, db, newApprovalService(cfg, db, nil, nil), nil)
	if err != nil {
		log.Printf("credentials: %v", err)
		return
	}
	dir := ipc.SocketDir()
	giveSessionsChannels(agents, service.NewCredentialChannels(broker, service.CredentialChannelsOptions{
		ProxyURL:     func() string { return readProxyURL(dir) },
		SSHSocketDir: filepath.Join(dir, sshSocketDirName),
		KnownHosts:   cfg.CredentialChannels.SSHAgent.KnownHosts,
		LegatoBin:    legatoBinary(),
		WrapperDir:   filepath.Join(dir, wrapperDirName),
		GHConfigDir:  ghConfigDir(cfg, dir),
	}))
}

// dropSecretEnv removes from this process's environment the variables that
// unlock Legato's secrets, once the configuration and the stores have read
// them, so the processes it starts (the tmux server, and through it every
// agent) do not inherit them. Agents can still read what Legato reads
// outside the sandbox; this keeps the token out of their environment.
func dropSecretEnv() {
	for _, name := range []string{"OP_SERVICE_ACCOUNT_TOKEN", "LEGATO_SECRETS_PASSPHRASE"} {
		_ = os.Unsetenv(name)
	}
}

// sshAgentOverride says what is wrong when ssh's own configuration names an
// agent of its own (IdentityAgent), which wins over the SSH_AUTH_SOCK agent
// sessions get: their logins would then never reach Legato. "" when ssh
// would use SSH_AUTH_SOCK. ssh -G does not run Match exec blocks, so one
// that sets IdentityAgent outside Legato's sessions is not seen.
func sshAgentOverride(sshG func(env []string) ([]byte, error)) string {
	out, err := sshG([]string{"LEGATO_EXECUTION_ID=ex-legato-check", "SSH_AUTH_SOCK=" + filepath.Join(os.TempDir(), "legato-check.sock")})
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok || !strings.EqualFold(key, "identityagent") {
			continue
		}
		value = strings.TrimSpace(value)
		if value == "SSH_AUTH_SOCK" || value == "$SSH_AUTH_SOCK" || value == "${SSH_AUTH_SOCK}" {
			return ""
		}
		return fmt.Sprintf("ssh is configured to use the agent %s for every host (IdentityAgent), so agents' ssh logins skip Legato's SSH agent. In ~/.ssh/config, set it only outside agent sessions: Match exec \"test -z \\\"$LEGATO_EXECUTION_ID\\\"\" then IdentityAgent %s (see docs/credentials.md)", value, value)
	}
	return ""
}

// runSSHG runs ssh -G for a host no configuration names specially.
func runSSHG(env []string) ([]byte, error) {
	cmd := exec.Command("ssh", "-G", "legato-agent-check.invalid")
	cmd.Env = append(os.Environ(), env...)
	return cmd.Output()
}
