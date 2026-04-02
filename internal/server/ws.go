package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/prompt"
	"nhooyr.io/websocket"
)

// WebSocket message types (client → server).
const (
	MsgSubscribeAgent   = "subscribe_agent"
	MsgUnsubscribeAgent = "unsubscribe_agent"
	MsgSendKeys         = "send_keys"
	MsgResize           = "resize"
	MsgDetectPrompt     = "detect_prompt"
	MsgRefreshPane      = "refresh_pane"
)

// WebSocket message types (server → client).
const (
	MsgAgentOutput   = "agent_output"
	MsgAgentList     = "agent_list"
	MsgAgentsChanged = "agents_changed"
	MsgPromptState   = "prompt_state"
	MsgError         = "error"
)

// WSMessage is the JSON envelope for all WebSocket messages.
type WSMessage struct {
	Type    string          `json:"type"`
	AgentID string          `json:"agent_id,omitempty"`
	Content string          `json:"content,omitempty"`
	Keys    string          `json:"keys,omitempty"`
	Cols    int             `json:"cols,omitempty"`
	Rows    int             `json:"rows,omitempty"`
	Agents  []AgentResponse `json:"agents,omitempty"`
	Prompt  *prompt.PromptState `json:"prompt,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// wsClient represents a connected WebSocket client.
type wsClient struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

func (c *wsClient) send(msg WSMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	c.conn.Write(ctx, websocket.MessageText, data)
}

// Hub manages connected WebSocket clients.
type Hub struct {
	mu      sync.RWMutex
	clients map[*wsClient]struct{}
}

func newHub() *Hub {
	return &Hub{
		clients: make(map[*wsClient]struct{}),
	}
}

func (h *Hub) add(c *wsClient) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(c *wsClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// Broadcast sends a message to all connected clients.
func (h *Hub) Broadcast(msg WSMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		go c.send(msg)
	}
}

func (s *Server) wsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true, // Allow connections from any origin (local network use).
		})
		if err != nil {
			return
		}

		ctx, cancel := context.WithCancel(r.Context())
		client := &wsClient{
			conn:   conn,
			ctx:    ctx,
			cancel: cancel,
		}

		s.hub.add(client)
		defer func() {
			s.streams.unsubscribeAll(client)
			s.hub.remove(client)
			conn.Close(websocket.StatusNormalClosure, "")
			cancel()
		}()

		// Send initial agent list.
		s.sendAgentList(client)

		// Read loop.
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}

			var msg WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				client.send(WSMessage{Type: MsgError, Error: "invalid JSON"})
				continue
			}

			s.handleWSMessage(client, msg)
		}
	}
}

func (s *Server) handleWSMessage(client *wsClient, msg WSMessage) {
	switch msg.Type {
	case MsgSubscribeAgent:
		s.subscribeAgent(client, msg.AgentID)
	case MsgUnsubscribeAgent:
		s.unsubscribeAgent(client, msg.AgentID)
	case MsgSendKeys:
		s.handleSendKeys(client, msg)
	case MsgResize:
		s.handleResize(client, msg)
	case MsgDetectPrompt:
		s.handleDetectPrompt(client, msg)
	case MsgRefreshPane:
		s.handleRefreshPane(client, msg)
	default:
		client.send(WSMessage{Type: MsgError, Error: "unknown message type: " + msg.Type})
	}
}

func (s *Server) sendAgentList(client *wsClient) {
	if s.agents == nil {
		client.send(WSMessage{Type: MsgAgentList, Agents: []AgentResponse{}})
		return
	}

	agents, err := s.agents.ListAgents(context.Background())
	if err != nil {
		client.send(WSMessage{Type: MsgError, Error: "failed to list agents"})
		return
	}

	resp := make([]AgentResponse, len(agents))
	for i, a := range agents {
		resp[i] = AgentResponse{
			ID:          a.ID,
			TaskID:      a.TaskID,
			Title:       a.Title,
			TmuxSession: a.TmuxSession,
			Command:     a.Command,
			Status:      a.Status,
			Activity:    a.Activity,
			StartedAt:   a.StartedAt,
			EndedAt:     a.EndedAt,
		}
	}
	client.send(WSMessage{Type: MsgAgentList, Agents: resp})
}

func (s *Server) subscribeAgent(client *wsClient, agentID string) {
	if s.tmux == nil {
		client.send(WSMessage{Type: MsgError, Error: "tmux not available"})
		return
	}

	// Register the client. The pipe-pane starts after the first resize
	// message so the pane is correctly sized before output flows.
	s.streams.subscribe(agentID, client)
}

func (s *Server) unsubscribeAgent(client *wsClient, agentID string) {
	s.streams.unsubscribe(agentID, client)
}

func (s *Server) handleResize(client *wsClient, msg WSMessage) {
	if msg.Cols <= 0 || msg.Rows <= 0 || msg.AgentID == "" {
		return
	}
	s.streams.updateClientSize(msg.AgentID, client, msg.Cols, msg.Rows)
}

func (s *Server) handleSendKeys(client *wsClient, msg WSMessage) {
	if s.tmux == nil {
		client.send(WSMessage{Type: MsgError, Error: "tmux not available"})
		return
	}

	if msg.AgentID == "" || msg.Keys == "" {
		client.send(WSMessage{Type: MsgError, Error: "agent_id and keys are required"})
		return
	}

	sessionName := "legato-" + msg.AgentID
	keys := msg.Keys

	// If keys ends with \n, send text literally (via --) then Enter as a named key.
	// If keys has no \n, send as a named key (e.g. "Escape", "S-Tab") without --.
	if len(keys) > 0 && keys[len(keys)-1] == '\n' {
		text := keys[:len(keys)-1]
		if text != "" {
			if err := s.tmux.SendKeys(sessionName, text); err != nil {
				client.send(WSMessage{Type: MsgError, Error: "send_keys failed: " + err.Error()})
				return
			}
		}
		if err := s.tmux.SendKey(sessionName, "Enter"); err != nil {
			client.send(WSMessage{Type: MsgError, Error: "send_keys failed: " + err.Error()})
		}
	} else {
		// Named keys — send without -- so tmux interprets key names.
		// Space-separated keys are sent in sequence (e.g. "Down Enter").
		for _, key := range strings.Fields(keys) {
			if err := s.tmux.SendKey(sessionName, key); err != nil {
				client.send(WSMessage{Type: MsgError, Error: "send_keys failed: " + err.Error()})
				return
			}
		}
	}
}

func (s *Server) handleDetectPrompt(client *wsClient, msg WSMessage) {
	if s.tmux == nil || msg.AgentID == "" {
		return
	}

	sessionName := "legato-" + msg.AgentID
	output, err := s.tmux.Capture(sessionName)
	if err != nil {
		return
	}

	state := prompt.Detect(output)
	client.send(WSMessage{
		Type:    MsgPromptState,
		AgentID: msg.AgentID,
		Prompt:  &state,
	})
}

func (s *Server) handleRefreshPane(client *wsClient, msg WSMessage) {
	if s.tmux == nil || msg.AgentID == "" {
		return
	}

	sessionName := "legato-" + msg.AgentID
	snapshot, err := s.tmux.CaptureWithEscapes(sessionName)
	if err != nil || snapshot == "" {
		return
	}

	snapshot = strings.ReplaceAll(snapshot, "\n", "\r\n")
	client.send(WSMessage{
		Type:    MsgAgentOutput,
		AgentID: msg.AgentID,
		Content: "\x1b[2J\x1b[H" + snapshot,
	})
}
