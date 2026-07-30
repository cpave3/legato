// Package legato implements a client for the Legato HTTP + WebSocket API.
//
// The client connects to a running Legato server, subscribes to live events
// over WebSocket, and falls back to HTTP polling when the WebSocket is down.
// It is designed to recover automatically when Legato stops and restarts.
package legato

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

// Agent represents a running agent session from GET /api/agents.
type Agent struct {
	ID             int     `json:"id"`
	TaskID         string  `json:"task_id"`
	Title          string  `json:"task_title"`
	AgentKind      string  `json:"agent_kind"`
	Status         string  `json:"status"`
	Activity       string  `json:"activity"` // "working", "waiting", or "" (idle)
	Role           string  `json:"role,omitempty"`
	ParentTaskID   string  `json:"parent_task_id,omitempty"`
	SubtaskID      string  `json:"subtask_id,omitempty"`
	WorkingSeconds float64 `json:"working_seconds"`
	WaitingSeconds float64 `json:"waiting_seconds"`
	NotifyEnabled  bool    `json:"notify_enabled"`
	HasChimeraEndpoint bool `json:"has_chimera_endpoint"`

	// Context pressure (0-1), fetched from chimera snapshot. -1 = unknown.
	ContextPressure float64 `json:"-"`
}

// AttentionItem represents an actionable item from GET /api/attention.
type AttentionItem struct {
	ID             string              `json:"id"`
	Producer       string              `json:"producer"`
	SourceRevision string              `json:"source_revision"`
	Title          string              `json:"title"`
	Reason         string              `json:"reason"`
	Category       string              `json:"category"`
	Urgency        string              `json:"urgency"`
	Navigation     AttentionNavigation `json:"navigation"`
	Actions        []AttentionAction   `json:"actions"`
}

type AttentionNavigation struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
}

type AttentionAction struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Kind            string `json:"kind"`
	Intent          string `json:"intent,omitempty"`
	RequiresConfirm bool   `json:"requires_confirmation,omitempty"`
}

// Config holds the connection parameters.
type Config struct {
	BaseURL      string
	Token        string
	DataDir      string // XDG data dir for auth-token file (default: ~/.local/share/legato)
	PollInterval time.Duration
}

// Snapshot is the current view of Legato state.
type Snapshot struct {
	Agents     []Agent
	Attention  []AttentionItem
	Connected  bool
	HasToken   bool
}

// StateCallback is called whenever the snapshot changes (new agents,
// attention items, or connection state transitions).
type StateCallback func(snap Snapshot)

// Client connects to Legato and maintains a live snapshot.
type Client struct {
	cfg     Config
	http    *http.Client
	mu      sync.Mutex
	snap    Snapshot
	cb      StateCallback
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
}

// NewClient creates a Legato client. Call Start to begin connecting.
func NewClient(cfg Config, cb StateCallback) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:3080"
	}
	if cfg.DataDir == "" {
		home, _ := os.UserHomeDir()
		cfg.DataDir = filepath.Join(home, ".local", "share", "legato")
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 5 * time.Second
	}
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 10 * time.Second},
		cb:   cb,
	}
}

// Start begins the connection goroutines. Call Stop to clean up.
func (c *Client) Start(ctx context.Context) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()

	c.wg.Add(1)
	go c.connectLoop(ctx)
}

// Stop shuts down the client goroutines.
func (c *Client) Stop() {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.running = false
	c.mu.Unlock()
	c.wg.Wait()
}

// Snapshot returns the current state snapshot (thread-safe).
func (c *Client) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap
}

// ToggleNotify PATCHes /api/agents/{task_id}/notify to enable/disable notifications.
func (c *Client) ToggleNotify(ctx context.Context, taskID string, enabled bool) error {
	token, err := c.resolveToken()
	if err != nil {
		return err
	}
	body := fmt.Sprintf(`{"enabled":%t}`, enabled)
	req, err := http.NewRequestWithContext(ctx, "PATCH",
		c.cfg.BaseURL+"/api/agents/"+taskID+"/notify",
		strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("toggle notify: %s: %s", resp.Status, string(b))
	}
	return nil
}

// FetchContextPressure queries the chimera snapshot for an agent and returns
// a 0-1 pressure value (used tokens / context window). Returns -1 if unavailable.
func (c *Client) FetchContextPressure(ctx context.Context, taskID string) (float64, error) {
	token, err := c.resolveToken()
	if err != nil {
		return -1, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET",
		c.cfg.BaseURL+"/api/agents/"+taskID+"/chimera/snapshot", nil)
	if err != nil {
		return -1, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return -1, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return -1, fmt.Errorf("snapshot: %s", resp.Status)
	}
	var snap struct {
		ContextWindow      int   `json:"contextWindow"`
		UsedContextTokens  *int  `json:"usedContextTokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return -1, err
	}
	if snap.ContextWindow == 0 || snap.UsedContextTokens == nil {
		return -1, nil
	}
	return float64(*snap.UsedContextTokens) / float64(snap.ContextWindow), nil
}

// ExecuteAction POSTs to /api/attention/{id}/actions/{action_id}.
func (c *Client) ExecuteAction(ctx context.Context, itemID, actionID, expectedRevision string) error {
	token, err := c.resolveToken()
	if err != nil {
		return err
	}
	body := fmt.Sprintf(`{"expected_revision":%q,"confirmed":true}`, expectedRevision)
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.cfg.BaseURL+"/api/attention/"+itemID+"/actions/"+actionID,
		strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("execute action: %s: %s", resp.Status, string(b))
	}
	return nil
}

// resolveToken returns the token from config, or reads it from the auth-token
// file if not explicitly configured.
func (c *Client) resolveToken() (string, error) {
	if c.cfg.Token != "" {
		return c.cfg.Token, nil
	}
	tokenPath := filepath.Join(c.cfg.DataDir, "auth-token")
	data, err := os.ReadFile(tokenPath)
	if err != nil {
		return "", fmt.Errorf("read auth token: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// connectLoop runs the main recovery state machine. It tries to connect,
// and on failure backs off and retries. When connected, it runs the WS read
// loop. When the WS drops, it falls back to polling, then retries WS.
func (c *Client) connectLoop(ctx context.Context) {
	defer c.wg.Done()

	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		// Health check first — cheap, unauthenticated.
		healthy, err := c.healthCheck(ctx)
		if err != nil || !healthy {
			c.setConnected(false)
			c.notify()
			select {
			case <-time.After(backoff):
				backoff = min(backoff*2, maxBackoff)
				continue
			case <-ctx.Done():
				return
			}
		}

		// We have a healthy server. Reset backoff.
		backoff = time.Second

		// Resolve token.
		token, err := c.resolveToken()
		if err != nil {
			log.Printf("legato: cannot resolve token: %v (will retry)", err)
			c.setConnected(false)
			c.setHasToken(false)
			c.notify()
			select {
			case <-time.After(backoff):
				backoff = min(backoff*2, maxBackoff)
				continue
			case <-ctx.Done():
				return
			}
		}
		c.setHasToken(true)

		// Fetch initial state via REST.
		if err := c.refreshState(ctx, token); err != nil {
			log.Printf("legato: initial refresh failed: %v", err)
			c.setConnected(false)
			c.notify()
			select {
			case <-time.After(backoff):
				continue
			case <-ctx.Done():
				return
			}
		}
		c.setConnected(true)
		snap := c.Snapshot()
		log.Printf("legato: connected — %d agents, %d attention items", len(snap.Agents), len(snap.Attention))
		c.notify()

		// Try WebSocket for live updates. If it fails, fall back to polling.
		c.runWithWS(ctx, token)
	}
}

// runWithWS connects the WebSocket and blocks on its read loop. When the
// WS drops, it switches to poll mode until the next reconnect cycle.
func (c *Client) runWithWS(ctx context.Context, token string) {
	wsURL := strings.Replace(c.cfg.BaseURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL += "/ws?token=" + token

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: map[string][]string{},
	})
	if err != nil {
		log.Printf("legato: ws dial failed, falling back to poll: %v", err)
		c.runPollFallback(ctx, token)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Read loop — process WS events until the connection drops.
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			log.Printf("legato: ws closed: %v", err)
			// Fall back to polling until the outer loop retries WS.
			c.runPollFallback(ctx, token)
			return
		}
		var msg struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "agents_changed", "attention_changed", "cards_changed":
			if err := c.refreshState(ctx, token); err != nil {
				log.Printf("legato: refresh on event failed: %v", err)
			} else {
				c.notify()
			}
		}
	}
}

// runPollFallback polls REST endpoints until the context is cancelled or
// a poll fails (which triggers the outer reconnect loop).
func (c *Client) runPollFallback(ctx context.Context, token string) {
	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.refreshState(ctx, token); err != nil {
				log.Printf("legato: poll failed: %v", err)
				c.setConnected(false)
				c.notify()
				return // return to outer loop for reconnect
			}
			c.setConnected(true)
			c.notify()
		}
	}
}

// healthCheck hits GET /health (unauthenticated).
func (c *Client) healthCheck(ctx context.Context) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.cfg.BaseURL+"/health", nil)
	if err != nil {
		return false, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

// refreshState fetches agents and attention items via REST, then enriches
// agents with context pressure from the chimera snapshot endpoint.
func (c *Client) refreshState(ctx context.Context, token string) error {
	agents, err := c.fetchAgents(ctx, token)
	if err != nil {
		return fmt.Errorf("fetch agents: %w", err)
	}
	attention, err := c.fetchAttention(ctx, token)
	if err != nil {
		return fmt.Errorf("fetch attention: %w", err)
	}

	// Fetch context pressure for agents with chimera endpoints.
	// Use a sub-context with a short timeout so a slow snapshot doesn't
	// block the whole refresh.
	pressCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for i := range agents {
		if agents[i].HasChimeraEndpoint {
			pressure, err := c.FetchContextPressure(pressCtx, agents[i].TaskID)
			if err != nil {
				agents[i].ContextPressure = -1
			} else {
				agents[i].ContextPressure = pressure
			}
		} else {
			agents[i].ContextPressure = -1
		}
	}

	c.mu.Lock()
	c.snap.Agents = agents
	c.snap.Attention = attention
	c.mu.Unlock()
	return nil
}

func (c *Client) fetchAgents(ctx context.Context, token string) ([]Agent, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.cfg.BaseURL+"/api/agents", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: %s", resp.Status, string(b))
	}
	var agents []Agent
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return nil, err
	}
	// Filter to running agents only.
	var running []Agent
	for _, a := range agents {
		if a.Status == "running" {
			running = append(running, a)
		}
	}
	return running, nil
}

func (c *Client) fetchAttention(ctx context.Context, token string) ([]AttentionItem, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.cfg.BaseURL+"/api/attention", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: %s", resp.Status, string(b))
	}
	var raw struct {
		Items []AttentionItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return raw.Items, nil
}

func (c *Client) setConnected(v bool) {
	c.mu.Lock()
	c.snap.Connected = v
	c.mu.Unlock()
}

func (c *Client) setHasToken(v bool) {
	c.mu.Lock()
	c.snap.HasToken = v
	c.mu.Unlock()
}

func (c *Client) notify() {
	c.mu.Lock()
	snap := c.snap
	c.mu.Unlock()
	if c.cb != nil {
		c.cb(snap)
	}
}

// DeepLink builds a browser URL for an agent or attention item.
func (c *Client) DeepLink(path string) string {
	return c.cfg.BaseURL + path
}

// AttentionDeepLink returns the browser URL for an attention item's navigation target.
func AttentionDeepLink(baseURL string, nav AttentionNavigation) string {
	switch nav.Kind {
	case "plan":
		return baseURL + "/plans/" + nav.Target
	case "review":
		return baseURL + "/review/" + nav.Target
	case "workflow_run":
		return baseURL + "/workflows/runs/" + nav.Target
	case "task":
		return baseURL + "/agents?task=" + nav.Target
	default:
		return baseURL + "/attention"
	}
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
