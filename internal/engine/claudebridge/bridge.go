// Package claudebridge adapts one Claude Code session to the native agent
// protocol Legato already speaks to Chimera.
//
// A Claude Code session cannot listen for connections: its Legato mod can only
// dial out and spawn a child. The bridge is that child. The mod posts the
// engine's events to it and reads commands from its stdout; Legato reads a
// session snapshot and an SSE event stream from it and posts messages,
// interrupts and permission decisions, exactly as it does for a Chimera
// endpoint. The bridge keeps the conversation state between the two.
package claudebridge

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Options configures a Bridge.
type Options struct {
	// TaskID is the Legato task the Claude session runs for. It names the
	// session Legato sees, so a binding survives Claude's own session changes
	// (a /clear continues under a new Claude session ID).
	TaskID string
	// Token, when set, is a secret first path segment every request must carry.
	Token string
	// Commands receives newline-delimited JSON commands for the mod.
	Commands io.Writer
	// Models are the model IDs the panel offers beside the session's own.
	Models []string
	// OnDisconnect is called once when the session is gone: a command can no
	// longer be written because the mod stopped reading, or the mod said its
	// session ended.
	OnDisconnect func()
	// OnIdentity records the provider conversation, independently of TaskID.
	// It is called in event order when a nonempty identity or its cwd changes.
	OnIdentity func(sessionID, cwd string) error
	// Now overrides the clock in tests.
	Now func() time.Time
}

// Bridge holds one Claude Code session's conversation state and serves it.
type Bridge struct {
	taskID string
	token  string
	models []string
	now    func() time.Time
	// instance scopes event IDs to this bridge, so a cursor from an earlier
	// bridge for the same task is never mistaken for one of ours.
	instance string

	commandMu      sync.Mutex
	commands       io.Writer
	onDisconnect   func()
	disconnectOnce sync.Once

	// eventMu orders whole mod batches and identity saves with Close. Panel
	// reads use only mu, which is never held during an identity callback.
	eventMu     sync.Mutex
	onIdentity  func(string, string) error
	identityID  string
	identityCWD string

	closeOnce sync.Once
	closed    chan struct{}

	mu          sync.Mutex
	session     *session
	sequence    int64
	retained    []envelope
	subscribers map[chan envelope]struct{}
	// waiters are the mod's held permission requests, by request ID.
	waiters map[string]chan permissionAnswer
}

// New creates a Bridge with an empty session.
func New(opts Options) *Bridge {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	commands := opts.Commands
	if commands == nil {
		commands = io.Discard
	}
	return &Bridge{
		taskID:       opts.TaskID,
		token:        opts.Token,
		models:       opts.Models,
		now:          now,
		instance:     randomHex(4),
		commands:     commands,
		onDisconnect: opts.OnDisconnect,
		onIdentity:   opts.OnIdentity,
		closed:       make(chan struct{}),
		session:      newSession(),
		subscribers:  map[chan envelope]struct{}{},
		waiters:      map[string]chan permissionAnswer{},
	}
}

// Close waits for an in-flight identity save and ends every open event stream.
func (b *Bridge) Close() {
	b.eventMu.Lock()
	defer b.eventMu.Unlock()
	b.closeOnce.Do(func() { close(b.closed) })
}

// Handler serves both peers: the mod under /mod and Legato under /v1.
func (b *Bridge) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /mod/events", b.handleModEvents)
	mux.HandleFunc("POST /mod/permissions", b.handleModPermission)
	mux.HandleFunc("GET /v1/sessions/{id}", b.handleSnapshot)
	mux.HandleFunc("GET /v1/sessions/{id}/events", b.handleEvents)
	mux.HandleFunc("POST /v1/sessions/{id}/messages", b.handleMessage)
	mux.HandleFunc("POST /v1/sessions/{id}/interrupt", b.handleInterrupt)
	mux.HandleFunc("POST /v1/sessions/{id}/permissions/{requestId}", b.handlePermission)
	mux.HandleFunc("POST /v1/sessions/{id}/questions/{requestId}", b.handleQuestion)
	b.registerDescriptionRoutes(mux)
	if b.token == "" {
		return mux
	}
	prefix := "/" + b.token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != prefix && !strings.HasPrefix(r.URL.Path, prefix+"/") {
			http.NotFound(w, r)
			return
		}
		http.StripPrefix(prefix, mux).ServeHTTP(w, r)
	})
}

func (b *Bridge) handleModEvents(w http.ResponseWriter, r *http.Request) {
	var batch struct {
		Events []modEvent `json:"events"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&batch); err != nil {
		http.Error(w, "invalid events JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	ended := false
	var identityErr error
	b.eventMu.Lock()
	b.mu.Lock()
	select {
	case <-b.closed:
		b.mu.Unlock()
		b.eventMu.Unlock()
		http.Error(w, "the Claude bridge has closed", http.StatusServiceUnavailable)
		return
	default:
	}
	for _, event := range batch.Events {
		if event.Type == "ended" {
			ended = true
			continue
		}
		b.publish(b.session.apply(event, b.now()))
		if (event.Type == "hello" || event.Type == "reset" || event.Type == "identity") && event.SessionID != "" && b.onIdentity != nil {
			cwd := b.session.cwd
			if event.SessionID != b.identityID || cwd != b.identityCWD {
				b.mu.Unlock()
				err := b.onIdentity(event.SessionID, cwd)
				b.mu.Lock()
				if err != nil {
					identityErr = fmt.Errorf("saving Claude session %s for resume: %w", event.SessionID, err)
					break
				}
				b.identityID, b.identityCWD = event.SessionID, cwd
			}
		}
	}
	b.releaseSettledWaiters()
	b.mu.Unlock()
	b.eventMu.Unlock()
	if identityErr != nil {
		http.Error(w, identityErr.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	// The session ended in this Claude process. When it was sent to the
	// background it goes on in another, whose bridge is waiting for this one
	// to let go of the task.
	if ended {
		b.disconnect()
	}
}

func (b *Bridge) disconnect() {
	if b.onDisconnect != nil {
		b.disconnectOnce.Do(b.onDisconnect)
	}
}

func (b *Bridge) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	snap := b.session.snapshot(b.taskID)
	b.mu.Unlock()
	writeJSON(w, http.StatusOK, snap)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func randomHex(bytes int) string {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "0"
	}
	return hex.EncodeToString(buf)
}
