package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/dockerproxy"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// dockerProxyLock is held by the process of this Legato that serves its
// sandboxed agents' Docker sockets. It is in the data folder: each Legato
// serves its own executions.
const dockerProxyLock = "docker-proxy.lock"

// dockerUpstream is the daemon sandboxed agents' Docker requests go to.
func dockerUpstream(cfg *config.Config) (dockerproxy.Upstream, error) {
	home, _ := os.UserHomeDir()
	return dockerproxy.ParseHost(dockerproxy.FindHost(cfg.Agents.Sandbox.Docker.Host, os.Getenv, home))
}

// sandboxDocker is agents.sandbox.docker for the agent service; nil when it
// is off, or when the daemon it names cannot be spoken to.
func sandboxDocker(cfg *config.Config) *service.SandboxDocker {
	if !cfg.Agents.Sandbox.Docker.Enabled {
		return nil
	}
	upstream, err := dockerUpstream(cfg)
	if err != nil {
		log.Printf("docker: sandboxed agents get no Docker socket: %v", err)
		return nil
	}
	return &service.SandboxDocker{RuntimeDir: ipc.SocketDir(), Cleaner: dockerproxy.Cleaner{Upstream: upstream}}
}

// dockerHost serves a Docker socket for each running sandboxed execution of
// this Legato that was started with one, and closes it when the execution
// ends. One process holds the lock; another takes over when it ends, and
// makes the sockets again (the sandbox mounts their folder, so agents see
// the new ones).
type dockerHost struct {
	cfg      *config.Config
	db       *store.Store
	docker   *service.SandboxDocker
	upstream dockerproxy.Upstream
	lockPath string

	mu      sync.Mutex
	lock    *os.File
	owner   bool
	servers map[string]*http.Server
	cancel  context.CancelFunc
	stopped bool
}

// startDockerHost serves sandboxed agents' Docker sockets when
// agents.sandbox.docker is on. Nil when it is off.
func startDockerHost(ctx context.Context, cfg *config.Config, db *store.Store, bus *events.Bus) *dockerHost {
	if !cfg.Agents.Sandbox.Enabled {
		return nil
	}
	docker := sandboxDocker(cfg)
	if docker == nil {
		return nil
	}
	upstream, _ := dockerUpstream(cfg)
	h := &dockerHost{cfg: cfg, db: db, docker: docker, upstream: upstream, servers: map[string]*http.Server{},
		lockPath: filepath.Join(config.ResolveDataDir(cfg), dockerProxyLock)}
	if err := upstream.Ping(ctx); err != nil {
		log.Printf("docker: %s does not answer yet (%v); sandboxed agents' Docker sockets will pass requests to it once it does", upstream, err)
	}
	ctx, h.cancel = context.WithCancel(ctx)
	go h.loop(ctx, bus)
	return h
}

func (h *dockerHost) loop(ctx context.Context, bus *events.Bus) {
	var started <-chan events.Event
	if bus != nil {
		s := bus.Subscribe(events.EventAgentStarted)
		defer bus.Unsubscribe(s)
		started = s
	}
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		h.tryOwn()
		h.Reconcile(ctx)
		select {
		case <-ctx.Done():
			return
		case _, ok := <-started:
			if !ok {
				started = nil
			}
		case <-tick.C:
		}
	}
}

func (h *dockerHost) tryOwn() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.owner || h.stopped {
		return
	}
	if err := os.MkdirAll(filepath.Dir(h.lockPath), 0o700); err != nil {
		return
	}
	lock, err := os.OpenFile(h.lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		log.Printf("docker: %v", err)
		return
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return
	}
	h.lock, h.owner = lock, true
}

// Reconcile serves each running execution that has a Docker view and closes
// the sockets of those that ended.
func (h *dockerHost) Reconcile(ctx context.Context) {
	h.mu.Lock()
	owner := h.owner && !h.stopped
	h.mu.Unlock()
	if !owner {
		return
	}
	running, err := h.db.ListRunningExecutions(ctx)
	if err != nil {
		return
	}
	live := map[string]bool{}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range running {
		live[e.ID] = true
		if h.servers[e.ID] == nil && h.docker.HasProxy(e.ID) {
			h.serveLocked(e.ID)
		}
	}
	for id, server := range h.servers {
		if !live[id] {
			_ = server.Close()
			delete(h.servers, id)
		}
	}
}

func (h *dockerHost) serveLocked(executionID string) {
	view, err := h.docker.View(executionID)
	if err != nil {
		log.Printf("docker: the sandbox view of %s: %v", executionID, err)
		return
	}
	path := h.docker.SocketPath(executionID)
	_ = os.Remove(path) // left by a host that ended
	ln, err := net.Listen("unix", path)
	if err != nil {
		log.Printf("docker: the socket of %s: %v", executionID, err)
		return
	}
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		log.Printf("docker: the socket of %s: %v", executionID, err)
		return
	}
	d := h.cfg.Agents.Sandbox.Docker
	proxy := dockerproxy.New(dockerproxy.Options{
		Execution: executionID, View: view, CapAdd: d.CapAdd, PublishAnywhere: d.Publish == "any",
		Upstream: h.upstream, Logf: log.Printf,
	})
	server := proxy.Server()
	h.servers[executionID] = server
	go func() {
		if err := server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("docker: serving %s: %v", executionID, err)
		}
	}()
}

// Stop closes every socket and lets another process take over.
func (h *dockerHost) Stop() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return
	}
	h.stopped = true
	if h.cancel != nil {
		h.cancel()
	}
	for id, server := range h.servers {
		_ = server.Close()
		delete(h.servers, id)
	}
	if h.owner {
		_ = syscall.Flock(int(h.lock.Fd()), syscall.LOCK_UN)
		_ = h.lock.Close()
		h.owner = false
	}
}
