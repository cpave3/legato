// Package codexbridge adapts one Codex session to the native agent protocol
// Legato already speaks to Chimera.
//
// Codex serves its own protocol: `codex app-server` holds the conversation,
// and the Codex terminal UI is one client of it (`codex --remote`). The
// bridge is another client of the same server. It follows the thread the
// terminal UI is on, turns what that thread does into the native events
// Legato's panel reads, and turns Legato's messages, interrupts and decisions
// into requests on that thread. The terminal UI and the panel therefore show
// one conversation, and either can drive it.
package codexbridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Options configures a Bridge.
type Options struct {
	// TaskID is the Legato task the Codex session runs for. It names the
	// session Legato sees, so a binding survives the person starting a new
	// Codex thread in the terminal.
	TaskID string
	// Token, when set, is a secret first path segment every request must carry.
	Token string
	// AppServerURL is the Codex app server's WebSocket address, and
	// AppServerToken the bearer token it expects.
	AppServerURL   string
	AppServerToken string
	// CWD is the directory the session was launched in. It is the session's
	// working directory until the terminal has opened a thread, which then
	// says where it works.
	CWD string
	// RejoinEvery is how often joining a thread is retried while its first
	// turn is starting. Zero means a sensible default.
	RejoinEvery time.Duration
	// Now overrides the clock in tests.
	Now func() time.Time
}

// Bridge holds one Codex session's conversation state and serves it.
type Bridge struct {
	taskID      string
	token       string
	appURL      string
	appToken    string
	rejoinEvery time.Duration
	now         func() time.Time
	// instance scopes event IDs to this bridge, so a cursor from an earlier
	// bridge for the same task is never mistaken for one of ours.
	instance string

	client *rpcClient
	// ended is closed when the app server connection is over; endErr says why.
	ended  chan struct{}
	endErr error

	closeOnce  sync.Once
	closed     chan struct{}
	attachOnce sync.Once
	attached   chan struct{}

	mu          sync.Mutex
	session     *session
	sequence    int64
	retained    []envelope
	subscribers map[chan envelope]struct{}
	// joining is the thread a join is in flight for, so only one runs.
	joining string
	models  []modelInfo
}

// errNoThread is a request made before the Codex terminal UI opened a thread.
var errNoThread = errors.New("the Codex session has not opened a thread yet")

const (
	defaultRejoinEvery = 150 * time.Millisecond
	// rejoinAttempts bounds the retries of one join: about three seconds.
	rejoinAttempts = 20
	callTimeout    = 30 * time.Second
)

// New creates a Bridge with an empty session. Connect attaches it to the app
// server.
func New(opts Options) *Bridge {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	rejoinEvery := opts.RejoinEvery
	if rejoinEvery <= 0 {
		rejoinEvery = defaultRejoinEvery
	}
	session := newSession()
	session.cwd = opts.CWD
	return &Bridge{
		taskID:      opts.TaskID,
		token:       opts.Token,
		appURL:      opts.AppServerURL,
		appToken:    opts.AppServerToken,
		rejoinEvery: rejoinEvery,
		now:         now,
		instance:    randomHex(4),
		ended:       make(chan struct{}),
		closed:      make(chan struct{}),
		attached:    make(chan struct{}),
		session:     session,
		subscribers: map[chan envelope]struct{}{},
	}
}

// Connect joins the app server as a client and introduces the bridge.
func (b *Bridge) Connect(ctx context.Context) error {
	client, err := dialAppServer(ctx, b.appURL, b.appToken)
	if err != nil {
		return err
	}
	b.client = client
	go b.pump()

	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	initialize := map[string]any{
		"clientInfo": map[string]any{"name": "legato", "title": "Legato", "version": protocolVersion},
		// The thread and approval methods the bridge uses are experimental.
		"capabilities": map[string]any{"experimentalApi": true},
	}
	if err := client.call(callCtx, "initialize", initialize, nil); err != nil {
		client.close()
		return err
	}
	if err := client.notify(callCtx, "initialized"); err != nil {
		client.close()
		return err
	}
	go func() {
		listCtx, stop := context.WithTimeout(context.Background(), callTimeout)
		defer stop()
		b.loadModels(listCtx)
	}()
	return nil
}

// Run serves until ctx ends or the app server connection does.
func (b *Bridge) Run(ctx context.Context) error {
	select {
	case <-ctx.Done():
		b.Close()
		return nil
	case <-b.ended:
		b.Close()
		return b.endErr
	}
}

// Attached is closed once the Codex terminal UI has opened a thread: from
// then on there is a conversation for Legato to show and drive.
func (b *Bridge) Attached() <-chan struct{} {
	return b.attached
}

// Close ends every open event stream and leaves the app server.
func (b *Bridge) Close() {
	b.closeOnce.Do(func() {
		close(b.closed)
		if b.client != nil {
			b.client.close()
		}
	})
}

func (b *Bridge) joined() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.session.joined
}

// pump reads the app server until the connection ends. Nothing it does may
// wait on a call's answer: this goroutine is what delivers them.
func (b *Bridge) pump() {
	for {
		incoming, err := b.client.read(context.Background())
		if err != nil {
			b.endErr = err
			close(b.ended)
			return
		}
		b.handle(incoming)
	}
}

func (b *Bridge) handle(incoming frame) {
	if len(incoming.ID) > 0 {
		// The app server asking its clients to decide something.
		b.raise(incoming)
		return
	}
	switch incoming.Method {
	case "thread/started":
		var params struct {
			Thread threadInfo `json:"thread"`
		}
		if json.Unmarshal(incoming.Params, &params) != nil || !params.Thread.isOwnConversation() {
			return
		}
		b.mu.Lock()
		b.publish(b.session.follow(params.Thread))
		b.mu.Unlock()
		b.attachOnce.Do(func() { close(b.attached) })
		go func() {
			// A thread resumed from history can be joined at once; a new one
			// cannot until its first turn starts, and says so.
			b.join(params.Thread.ID, 1)
			b.deliverHeld()
		}()
	case "thread/status/changed":
		var params struct {
			ThreadID string `json:"threadId"`
			Status   struct {
				Type string `json:"type"`
			} `json:"status"`
		}
		if json.Unmarshal(incoming.Params, &params) != nil {
			return
		}
		b.mu.Lock()
		if params.ThreadID != b.session.threadID {
			b.mu.Unlock()
			return
		}
		joined := b.session.joined
		switch {
		case params.Status.Type == "active":
			b.publish(b.session.startTurn(""))
		case params.Status.Type == "idle" && !joined && b.session.busy:
			// Unjoined, the bridge is never told the turn completed.
			b.publish(b.session.finishTurn("completed", ""))
		}
		b.mu.Unlock()
		if params.Status.Type == "active" && !joined {
			go b.join(params.ThreadID, rejoinAttempts)
		}
	case "turn/started":
		var params struct {
			ThreadID string   `json:"threadId"`
			Turn     turnInfo `json:"turn"`
		}
		if json.Unmarshal(incoming.Params, &params) != nil {
			return
		}
		b.onThread(params.ThreadID, func(s *session) []nativeEvent { return s.startTurn(params.Turn.ID) })
	case "turn/completed":
		var params struct {
			ThreadID string   `json:"threadId"`
			Turn     turnInfo `json:"turn"`
		}
		if json.Unmarshal(incoming.Params, &params) != nil {
			return
		}
		failure := ""
		if params.Turn.Error != nil {
			failure = params.Turn.Error.Message
		}
		b.onThread(params.ThreadID, func(s *session) []nativeEvent { return s.finishTurn(params.Turn.Status, failure) })
	case "item/started", "item/completed":
		var params struct {
			ThreadID string     `json:"threadId"`
			Item     threadItem `json:"item"`
		}
		if json.Unmarshal(incoming.Params, &params) != nil {
			return
		}
		settled := incoming.Method == "item/completed"
		b.onThread(params.ThreadID, func(s *session) []nativeEvent { return s.applyItem(params.Item, settled, b.now()) })
	case "item/agentMessage/delta", "item/plan/delta":
		var params struct {
			ThreadID string `json:"threadId"`
			ItemID   string `json:"itemId"`
			Delta    string `json:"delta"`
		}
		if json.Unmarshal(incoming.Params, &params) != nil {
			return
		}
		b.onThread(params.ThreadID, func(s *session) []nativeEvent { return s.applyDelta(params.ItemID, params.Delta) })
	case "thread/tokenUsage/updated":
		var params struct {
			ThreadID   string `json:"threadId"`
			TokenUsage struct {
				Last struct {
					TotalTokens int `json:"totalTokens"`
				} `json:"last"`
				ModelContextWindow *int `json:"modelContextWindow"`
			} `json:"tokenUsage"`
		}
		if json.Unmarshal(incoming.Params, &params) != nil || params.TokenUsage.ModelContextWindow == nil {
			return
		}
		b.onThread(params.ThreadID, func(s *session) []nativeEvent {
			return s.applyUsage(params.TokenUsage.Last.TotalTokens, *params.TokenUsage.ModelContextWindow)
		})
	case "thread/settings/updated":
		var params struct {
			ThreadID string `json:"threadId"`
			Settings struct {
				Model  string  `json:"model"`
				Effort *string `json:"effort"`
				CWD    string  `json:"cwd"`
			} `json:"threadSettings"`
		}
		if json.Unmarshal(incoming.Params, &params) != nil {
			return
		}
		effort := ""
		if params.Settings.Effort != nil {
			effort = *params.Settings.Effort
		}
		b.onThread(params.ThreadID, func(*session) []nativeEvent {
			return b.applySettings(params.Settings.Model, effort, params.Settings.CWD)
		})
	case "serverRequest/resolved":
		var params struct {
			ThreadID  string          `json:"threadId"`
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(incoming.Params, &params) != nil {
			return
		}
		// Still open here means another client, the terminal, answered it.
		b.onThread(params.ThreadID, func(s *session) []nativeEvent { return s.expireRequestAnswered(params.RequestID) })
	}
}

// onThread applies something that happened on the followed thread and
// publishes what it changed. Other threads on the server are not this
// session's conversation.
func (b *Bridge) onThread(threadID string, apply func(*session) []nativeEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if threadID == "" || threadID != b.session.threadID {
		return
	}
	b.publish(apply(b.session))
}

// join subscribes the bridge to a thread's events and loads its history. A
// thread has none until its first turn starts, so a join made as that turn
// starts is retried for a moment.
func (b *Bridge) join(threadID string, attempts int) {
	b.mu.Lock()
	if b.joining == threadID || b.session.threadID != threadID || b.session.joined {
		b.mu.Unlock()
		return
	}
	b.joining = threadID
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		if b.joining == threadID {
			b.joining = ""
		}
		b.mu.Unlock()
	}()

	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-b.closed:
				return
			case <-time.After(b.rejoinEvery):
			}
		}
		var joined struct {
			Thread          threadInfo `json:"thread"`
			Model           string     `json:"model"`
			ReasoningEffort *string    `json:"reasoningEffort"`
		}
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		err := b.client.call(ctx, "thread/resume", map[string]any{"threadId": threadID}, &joined)
		cancel()
		if err != nil {
			continue
		}
		if joined.Model != "" {
			joined.Thread.Model = &joined.Model
		}
		if joined.ReasoningEffort != nil {
			joined.Thread.ReasoningEffort = joined.ReasoningEffort
		}
		b.mu.Lock()
		if b.session.threadID == threadID && !b.session.joined {
			b.publish(b.session.load(joined.Thread, b.now()))
		}
		b.mu.Unlock()
		return
	}
}

// Handler serves Legato the native agent protocol under /v1.
func (b *Bridge) Handler() http.Handler {
	mux := http.NewServeMux()
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

func (b *Bridge) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	snap := b.session.snapshot(b.taskID, b.models)
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
