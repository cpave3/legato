package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

const (
	// The socket is alone in a folder of its own, which the sandbox mounts:
	// a mounted socket file would go stale when another host replaces it,
	// and the runtime folder also holds every Legato's IPC socket.
	brokerSocketDir  = "broker"
	brokerSocketName = "broker.sock"
	brokerLockFile   = "broker.lock"
)

type brokerHostOptions struct {
	Deps cli.BrokerDeps
	// Retry is how often a process that does not serve the broker tries to
	// take it over; 5 seconds when zero.
	Retry time.Duration
}

// brokerHost serves the broker for every Legato on this machine: one
// process holds the lock and listens at a fixed path, so agents keep their
// broker when the Legato that spawned them restarts. Tokens are in the
// shared database, so any host resolves any session's token.
type brokerHost struct {
	db     *store.Store
	dir    string
	opts   brokerHostOptions
	mu     sync.Mutex
	lock   *os.File
	server *broker.Server
	cancel context.CancelFunc
	done   chan struct{}
}

func startBrokerHost(ctx context.Context, db *store.Store, dir string, opts brokerHostOptions) *brokerHost {
	if opts.Retry == 0 {
		opts.Retry = 5 * time.Second
	}
	h := &brokerHost{db: db, dir: dir, opts: opts, done: make(chan struct{})}
	ctx, h.cancel = context.WithCancel(ctx)
	h.tryOwn()
	go func() {
		defer close(h.done)
		ticker := time.NewTicker(opts.Retry)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				h.tryOwn()
			}
		}
	}()
	return h
}

// SocketPath is where agents reach the broker, whichever process serves it.
func (h *brokerHost) SocketPath() string {
	return filepath.Join(h.dir, brokerSocketDir, brokerSocketName)
}

// Owner reports whether this process serves the broker.
func (h *brokerHost) Owner() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.server != nil
}

func (h *brokerHost) tryOwn() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.server != nil {
		return
	}
	if err := os.MkdirAll(h.dir, 0o700); err != nil {
		log.Printf("broker: %v", err)
		return
	}
	lock, err := os.OpenFile(filepath.Join(h.dir, brokerLockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		log.Printf("broker: %v", err)
		return
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return
	}
	// The socket a previous holder left is stale now that the lock is ours.
	_ = os.Remove(h.SocketPath())
	server, err := broker.Listen(h.SocketPath(), cli.NewBrokerHandler(h.db, h.opts.Deps))
	if err != nil {
		log.Printf("broker: %v", err)
		lock.Close()
		return
	}
	h.lock, h.server = lock, server
}

// Stop stops serving and lets another Legato take the broker over.
func (h *brokerHost) Stop() {
	h.cancel()
	<-h.done
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.server != nil {
		_ = h.server.Close()
		h.server = nil
	}
	if h.lock != nil {
		_ = h.lock.Close()
		h.lock = nil
	}
}

// giveSessionsBroker makes an agent service give its sessions the broker's
// socket and a token each.
func giveSessionsBroker(agents service.AgentService, db *store.Store, socket string) {
	if setter, ok := agents.(interface {
		SetBrokerEnv(service.SessionEnvProvider)
	}); ok && agents != nil {
		setter.SetBrokerEnv(service.NewBrokerSessionEnv(db, socket))
	}
}
