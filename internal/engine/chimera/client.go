// Package chimera provides a narrow HTTP/SSE client for Chimera's server protocol.
// It implements only the operations Legato needs: session inspection, message
// POST, interrupt, permission resolution, snapshot retrieval, and SSE parsing.
//
// Chimera starts an HTTP/SSE server in every interactive session. Legato
// discovers the server's endpoint and session ID from a lockfile written by
// the interactive command, then uses this client to proxy structured events
// to the web UI and send follow-up messages/interrupts/permissions through
// the HTTP API instead of tmux send-keys.
package chimera

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Client is the narrow Chimera protocol client.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient creates a client for the given base URL (e.g. http://127.0.0.1:38123).
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{},
	}
}

// InstanceInfo describes the Chimera server instance.
type InstanceInfo struct {
	PID         int    `json:"pid"`
	CWD         string `json:"cwd"`
	Version     string `json:"version"`
	SandboxMode string `json:"sandboxMode"`
}

// SessionSnapshot is the session state returned by GET /v1/sessions/:id.
// It includes the full conversation state (messages, tool calls, usage, etc.).
// Fields are kept as json.RawMessage for the parts Legato doesn't need to
// decode, so the client tolerates protocol additions without breaking.
type SessionSnapshot struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	CWD               string          `json:"cwd"`
	Model             json.RawMessage `json:"model"`
	SandboxMode       string          `json:"sandboxMode"`
	Mode              string          `json:"mode"`
	Status            string          `json:"status"`
	Messages          json.RawMessage `json:"messages"`
	ToolCalls         json.RawMessage `json:"toolCalls"`
	Usage             json.RawMessage `json:"usage"`
	Tasks             json.RawMessage `json:"tasks"`
	ContextWindow     int             `json:"contextWindow,omitempty"`
	UsedContextTokens int             `json:"usedContextTokens,omitempty"`
	UserModelOverride *string         `json:"userModelOverride"`
}

// SessionContext describes the provider-reported occupancy of a session's
// context window. LastPromptTokens is nil before the first model step.
type SessionContext struct {
	ContextWindow    int  `json:"contextWindow"`
	LastPromptTokens *int `json:"lastPromptTokens"`
}

// EventEnvelope is a single SSE event from Chimera's event stream.
// The Data field contains the raw JSON payload; the caller is responsible
// for decoding it into the appropriate AgentEvent type.
type EventEnvelope struct {
	EventID   string          `json:"eventId"`
	SessionID string          `json:"sessionId"`
	Timestamp int64           `json:"ts"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"-"` // raw SSE data field (full envelope JSON)
}

// MarshalJSON serializes the envelope as the raw event data, which
// includes all event-specific fields (content, text, delta, etc.).
// The EventID, SessionID, Timestamp, and Type fields are already part
// of the raw data, so we just marshal Data directly.
func (e EventEnvelope) MarshalJSON() ([]byte, error) {
	if len(e.Data) > 0 {
		return e.Data, nil
	}
	type alias EventEnvelope
	return json.Marshal(alias(e))
}

// ModeInfo describes a mode available in a Chimera session.
type ModeInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Cycle       bool   `json:"cycle"`
}

// CommandInfo describes a slash command available in a Chimera session.
type CommandInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
	Path        string `json:"path"`
	Source      string `json:"source"`
}

// SkillInfo describes a skill available in a Chimera session.
type SkillInfo struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Path        string            `json:"path"`
	Source      string            `json:"source"`
	Frontmatter map[string]string `json:"frontmatter"`
}

// PermissionRequest is the pending permission request state.
type PermissionRequest struct {
	RequestID string `json:"requestId"`
	ToolName  string `json:"toolName"`
	Args      any    `json:"args"`
	Target    string `json:"target"`
}

// GetInstance fetches instance metadata from GET /v1/instance.
func (c *Client) GetInstance(ctx context.Context) (*InstanceInfo, error) {
	var info InstanceInfo
	if err := c.getJSON(ctx, "/v1/instance", &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// GetSession fetches the full session snapshot from GET /v1/sessions/:id.
func (c *Client) GetSession(ctx context.Context, sessionID string) (*SessionSnapshot, error) {
	var snap SessionSnapshot
	if err := c.getJSON(ctx, "/v1/sessions/"+url.PathEscape(sessionID), &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// GetSessionContext fetches the current context-window occupancy reported by Chimera.
func (c *Client) GetSessionContext(ctx context.Context, sessionID string) (*SessionContext, error) {
	var sessionContext SessionContext
	if err := c.getJSON(ctx, "/v1/sessions/"+url.PathEscape(sessionID)+"/context", &sessionContext); err != nil {
		return nil, err
	}
	return &sessionContext, nil
}

// PostMessage sends a user message to the session via POST /v1/sessions/:id/messages.
// Returns ErrBusy if a run is already in progress (HTTP 409).
func (c *Client) PostMessage(ctx context.Context, sessionID, content string) error {
	return c.postMessage(ctx, sessionID, content, false)
}

// PostMessageAppend appends a user message without starting a run.
func (c *Client) PostMessageAppend(ctx context.Context, sessionID, content string) error {
	return c.postMessage(ctx, sessionID, content, true)
}

func (c *Client) postMessage(ctx context.Context, sessionID, content string, append bool) error {
	body := map[string]string{"content": content}
	bodyBytes, _ := json.Marshal(body)

	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/messages"
	if append {
		path += "?append=true"
	}
	resp, err := c.do(ctx, http.MethodPost, path, bodyBytes)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 409 {
		return ErrBusy
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s: %s", path, resp.Status)
	}
	return nil
}

// Interrupt sends an interrupt to the session via POST /v1/sessions/:id/interrupt.
func (c *Client) Interrupt(ctx context.Context, sessionID string) error {
	resp, err := c.do(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/interrupt", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("interrupt: %s", resp.Status)
	}
	return nil
}

// ResolvePermission resolves a pending permission request via
// POST /v1/sessions/:id/permissions/:requestId.
// decision is "allow" or "deny". rememberScope is optional ("session" or "project").
func (c *Client) ResolvePermission(ctx context.Context, sessionID, requestID, decision, rememberScope string) error {
	body := map[string]string{"decision": decision}
	if rememberScope != "" {
		body["rememberScope"] = rememberScope
	}
	bodyBytes, _ := json.Marshal(body)

	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/permissions/" + url.PathEscape(requestID)
	resp, err := c.do(ctx, http.MethodPost, path, bodyBytes)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("resolvePermission: %s", resp.Status)
	}
	return nil
}

// Subscribe connects to the SSE event stream and calls onEvent for each
// event envelope. The since parameter is an optional event ID for replay;
// events after that ID are replayed before live events start.
// The function blocks until ctx is cancelled or the stream ends.
// It is safe to call Subscribe concurrently with PostMessage/Interrupt.
func (c *Client) Subscribe(ctx context.Context, sessionID, since string, onEvent func(EventEnvelope)) error {
	path := "/v1/sessions/" + url.PathEscape(sessionID) + "/events"
	if since != "" {
		path += "?since=" + url.QueryEscape(since)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("SSE subscribe: %s", resp.Status)
	}

	return parseSSE(resp.Body, onEvent)
}

// ListModes returns the available modes for a session.
func (c *Client) ListModes(ctx context.Context, sessionID string) ([]ModeInfo, error) {
	var modes []ModeInfo
	if err := c.getJSON(ctx, "/v1/sessions/"+url.PathEscape(sessionID)+"/modes", &modes); err != nil {
		return nil, err
	}
	return modes, nil
}

// SetMode changes the mode of a session.
func (c *Client) SetMode(ctx context.Context, sessionID, mode string) error {
	body := map[string]string{"mode": mode}
	bodyBytes, _ := json.Marshal(body)
	resp, err := c.do(ctx, http.MethodPost, "/v1/sessions/"+url.PathEscape(sessionID)+"/mode", bodyBytes)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("setMode: %s", resp.Status)
	}
	return nil
}

// ListCommands returns the slash commands available in a session.
func (c *Client) ListCommands(ctx context.Context, sessionID string) ([]CommandInfo, error) {
	var commands []CommandInfo
	if err := c.getJSON(ctx, "/v1/sessions/"+url.PathEscape(sessionID)+"/commands", &commands); err != nil {
		return nil, err
	}
	return commands, nil
}

// ListSkills returns the skills available in a session.
func (c *Client) ListSkills(ctx context.Context, sessionID string) ([]SkillInfo, error) {
	var skills []SkillInfo
	if err := c.getJSON(ctx, "/v1/sessions/"+url.PathEscape(sessionID)+"/skills", &skills); err != nil {
		return nil, err
	}
	return skills, nil
}

// ErrBusy is returned when a message POST is rejected because a run
// already in progress (HTTP 409).
var ErrBusy = fmt.Errorf("run already in progress")

// afterShortDelay returns a channel that fires after a brief pause,
// used for SSE reconnection backoff.
func afterShortDelay() <-chan time.Time {
	return time.After(2 * time.Second)
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	resp, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: %s %s", path, resp.Status, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// parseSSE reads an SSE stream and calls onEvent for each event block.
// Lines starting with ":" are heartbeats/comments and are ignored.
// An empty line delimits events.
func parseSSE(reader io.Reader, onEvent func(EventEnvelope)) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventID string
	var dataBuf strings.Builder

	flush := func() {
		if dataBuf.Len() == 0 {
			return
		}
		data := dataBuf.String()
		dataBuf.Reset()

		// The SSE data field is the full event envelope JSON.
		// Parse out the eventId and type for the envelope, but pass
		// the raw JSON to the caller via Data.
		var partial struct {
			EventID   string `json:"eventId"`
			SessionID string `json:"sessionId"`
			Ts        int64  `json:"ts"`
			Type      string `json:"type"`
		}
		if err := json.Unmarshal([]byte(data), &partial); err != nil {
			// Tolerate malformed events — skip rather than abort.
			return
		}
		env := EventEnvelope{
			EventID:   partial.EventID,
			SessionID: partial.SessionID,
			Timestamp: partial.Ts,
			Type:      partial.Type,
			Data:      json.RawMessage(data),
		}
		if eventID != "" {
			env.EventID = eventID
		}
		onEvent(env)
		eventID = ""
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			flush()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // heartbeat/comment
		}
		colon := strings.Index(line, ":")
		var field, value string
		if colon > 0 {
			field = line[:colon]
			value = strings.TrimPrefix(line[colon+1:], " ")
		} else {
			field = line
		}
		switch field {
		case "id":
			eventID = value
		case "data":
			if dataBuf.Len() > 0 {
				dataBuf.WriteString("\n")
			}
			dataBuf.WriteString(value)
		}
	}
	flush()
	return scanner.Err()
}

// Lockfile is the JSON content of ~/.chimera/instances/<pid>.json.
type Lockfile struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	CWD       string `json:"cwd"`
	SessionID string `json:"sessionId"`
	StartedAt int64  `json:"startedAt"`
	Version   string `json:"version"`
	URL       string `json:"url"`
}

// ParseLockfile parses a lockfile JSON blob.
func ParseLockfile(data []byte) (*Lockfile, error) {
	var lf Lockfile
	if err := json.Unmarshal(data, &lf); err != nil {
		return nil, fmt.Errorf("parsing chimera lockfile: %w", err)
	}
	return &lf, nil
}

// EventSubscriber manages a background SSE subscription with graceful
// shutdown. It ensures events are delivered to the handler without
// blocking the caller and handles reconnection.
type EventSubscriber struct {
	client   *Client
	sessionID string
	handler  func(EventEnvelope)
	mu       sync.Mutex
	lastID   string
	cancel   context.CancelFunc
}

// NewEventSubscriber creates a subscriber that tracks the last event ID
// for gap-free replay on reconnect.
func NewEventSubscriber(client *Client, sessionID string, handler func(EventEnvelope)) *EventSubscriber {
	return &EventSubscriber{
		client:   client,
		sessionID: sessionID,
		handler:  handler,
	}
}

// Start begins the SSE subscription in a background goroutine.
// The subscription runs until Stop is called.
func (s *EventSubscriber) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			err := s.client.Subscribe(ctx, s.sessionID, s.getLastID(), func(env EventEnvelope) {
				s.setLastID(env.EventID)
				s.handler(env)
			})
			if err != nil && ctx.Err() == nil {
				// Reconnect after a brief pause on error.
				select {
				case <-ctx.Done():
					return
				case <-afterShortDelay():
				}
			} else {
				return
			}
		}
	}()
}

// Stop cancels the SSE subscription.
func (s *EventSubscriber) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *EventSubscriber) getLastID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastID
}

func (s *EventSubscriber) setLastID(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != "" {
		s.lastID = id
	}
}
