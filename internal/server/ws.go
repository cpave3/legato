package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/prompt"
	"github.com/cpave3/legato/internal/service"
	"nhooyr.io/websocket"
)

// WebSocket message types (client → server).
const (
	MsgSubscribeAgent   = "subscribe_agent"
	MsgUnsubscribeAgent = "unsubscribe_agent"
	MsgSendKeys         = "send_keys"
	MsgAgentMessage     = "agent_message"
	MsgAgentInterrupt   = "agent_interrupt"
	MsgAgentPermission  = "agent_permission"
	MsgAgentQuestion    = "agent_question"
	MsgResize           = "resize"
	MsgDetectPrompt     = "detect_prompt"
	MsgRefreshPane      = "refresh_pane"
)

// WebSocket message types (server → client).
const (
	MsgAgentOutput         = "agent_output"
	MsgAgentList           = "agent_list"
	MsgAgentsChanged       = "agents_changed"
	MsgChimeraEvent        = "chimera_event"
	MsgChimeraSyncRequired = "chimera_sync_required"
	MsgPromptState         = "prompt_state"
	MsgPlanProposed        = "plan_proposed"
	MsgPlanVerdict         = "plan_verdict"
	MsgSwarmChanged        = "swarm_changed"
	MsgCardsChanged        = "cards_changed"
	MsgReviewChanged       = "review_changed"
	MsgPlanChanged         = "plan_changed"
	MsgWorkflowChanged     = "workflow_changed"
	MsgAttentionChanged    = "attention_changed"
	MsgLearningChanged     = "learning_changed"
	MsgError               = "error"
)

// WSMessage is the JSON envelope for all WebSocket messages.
type WSMessage struct {
	Type           string              `json:"type"`
	AgentID        string              `json:"agent_id,omitempty"`
	Content        string              `json:"content,omitempty"`
	Keys           string              `json:"keys,omitempty"`
	Cols           int                 `json:"cols,omitempty"`
	Rows           int                 `json:"rows,omitempty"`
	Agents         []AgentResponse     `json:"agents,omitempty"`
	Prompt         *prompt.PromptState `json:"prompt,omitempty"`
	Error          string              `json:"error,omitempty"`
	PlanPath       string              `json:"plan_path,omitempty"`
	ReplySocket    string              `json:"reply_socket,omitempty"`
	ParentTaskID   string              `json:"parent_task_id,omitempty"`
	SubtaskID      string              `json:"subtask_id,omitempty"`
	Status         string              `json:"status,omitempty"`
	Notes          string              `json:"notes,omitempty"`
	Mode           string              `json:"mode,omitempty"`
	TaskID         string              `json:"task_id,omitempty"`
	TourID         string              `json:"tour_id,omitempty"`
	StepID         string              `json:"step_id,omitempty"`
	Kind           string              `json:"kind,omitempty"`
	PlanID         string              `json:"plan_id,omitempty"`
	RevisionID     string              `json:"revision_id,omitempty"`
	WorkflowRunID  string              `json:"workflow_run_id,omitempty"`
	WorkflowStepID string              `json:"workflow_step_id,omitempty"`
	ItemID         string              `json:"item_id,omitempty"`
	LearningID     string              `json:"learning_id,omitempty"`
	Producer       string              `json:"producer,omitempty"`
	SourceKey      string              `json:"source_key,omitempty"`
	RequestID      string              `json:"request_id,omitempty"`
	Cursor         string              `json:"cursor,omitempty"`
	Queue          bool                `json:"queue,omitempty"`
	Decision       string              `json:"decision,omitempty"`
	Feedback       *string             `json:"feedback,omitempty"`
	// Option names which of a permission request's options an allow takes.
	Option string `json:"option,omitempty"`
	// Answers and Dismissed answer a question the agent asked (agent_question).
	Answers   map[string][]string `json:"answers,omitempty"`
	Dismissed bool                `json:"dismissed,omitempty"`
}

// wsClient represents a connected WebSocket client.
type wsClient struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	sendFn func(WSMessage)
}

func (c *wsClient) send(msg WSMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sendFn != nil {
		c.sendFn(msg)
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	_ = c.conn.Write(ctx, websocket.MessageText, data)
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
			s.chimeraStreams.unsubscribeAll(client)
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
		s.subscribeAgent(client, msg.AgentID, msg.Cursor)
	case MsgUnsubscribeAgent:
		s.unsubscribeAgent(client, msg.AgentID)
	case MsgSendKeys:
		s.handleSendKeys(client, msg)
	case MsgAgentMessage:
		s.handleAgentMessage(client, msg)
	case MsgAgentInterrupt:
		s.handleAgentInterrupt(client, msg)
	case MsgAgentPermission:
		s.handleAgentPermission(client, msg)
	case MsgAgentQuestion:
		s.handleAgentQuestion(client, msg)
	case MsgResize:
		s.handleResize(client, msg)
	case MsgDetectPrompt:
		s.handleDetectPrompt(client, msg)
	case MsgRefreshPane:
		s.handleRefreshPane(client, msg)
	case MsgPlanVerdict:
		s.handlePlanVerdict(client, msg)
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

	taskIDs := make([]string, len(agents))
	for i, a := range agents {
		taskIDs[i] = a.TaskID
	}
	durations, _ := s.agents.GetTaskDurations(context.Background(), taskIDs)

	resp := make([]AgentResponse, len(agents))
	for i, a := range agents {
		r := agentResponse(a)
		if d, ok := durations[a.TaskID]; ok {
			r.WorkingSeconds = d.Working.Seconds()
			r.WaitingSeconds = d.Waiting.Seconds()
			r.ErrorSeconds = d.Error.Seconds()
		}
		r.HasChimeraEndpoint = s.agents.HasChimeraEndpoint(context.Background(), a.TaskID)
		resp[i] = r
	}
	client.send(WSMessage{Type: MsgAgentList, Agents: resp})
}

func (s *Server) subscribeAgent(client *wsClient, agentID, cursor string) {
	if s.agents != nil && s.agents.HasChimeraEndpoint(context.Background(), agentID) {
		// Chimera agent: subscribe to the shared SSE fan-out.
		s.chimeraStreams.subscribe(client, agentID, cursor)
		return
	}
	if s.tmux == nil {
		client.send(WSMessage{Type: MsgError, Error: "tmux not available"})
		return
	}

	// Register the client. The pipe-pane starts after the first resize
	// message so the pane is correctly sized before output flows.
	s.streams.subscribe(agentID, client)
}

func (s *Server) unsubscribeAgent(client *wsClient, agentID string) {
	// For Chimera agents, unsubscribe from the shared SSE fan-out.
	s.chimeraStreams.unsubscribe(client, agentID)
	s.streams.unsubscribe(agentID, client)
}

// handleAgentMessage sends a text message to the agent through the
// AgentService interaction adapter (HTTP for Chimera, send-keys for others).
func (s *Server) handleAgentMessage(client *wsClient, msg WSMessage) {
	if s.agents == nil {
		client.send(WSMessage{Type: MsgError, Error: "agent service not available"})
		return
	}
	if msg.AgentID == "" || msg.Content == "" {
		client.send(WSMessage{Type: MsgError, Error: "agent_id and content are required"})
		return
	}
	var err error
	if msg.Queue {
		queuer, ok := s.agents.(service.AgentMessageQueuer)
		if !ok {
			client.send(WSMessage{Type: MsgError, Error: "agent does not support queued messages"})
			return
		}
		err = queuer.QueueMessage(context.Background(), msg.AgentID, msg.Content)
	} else {
		err = s.agents.SendMessage(context.Background(), msg.AgentID, msg.Content)
	}
	if err != nil {
		client.send(WSMessage{Type: MsgError, Error: "send message failed: " + err.Error()})
	}
}

func (s *Server) handleAgentInterrupt(client *wsClient, msg WSMessage) {
	if s.agents == nil {
		client.send(WSMessage{Type: MsgError, Error: "agent service not available"})
		return
	}
	if msg.AgentID == "" {
		client.send(WSMessage{Type: MsgError, Error: "agent_id is required"})
		return
	}
	if err := s.agents.InterruptAgent(context.Background(), msg.AgentID); err != nil {
		client.send(WSMessage{Type: MsgError, Error: "interrupt failed: " + err.Error()})
	}
}

func (s *Server) handleAgentPermission(client *wsClient, msg WSMessage) {
	sendError := func(message string) {
		client.send(WSMessage{Type: MsgError, AgentID: msg.AgentID, RequestID: msg.RequestID, Error: message})
	}
	if s.agents == nil {
		sendError("agent service not available")
		return
	}
	if msg.AgentID == "" || msg.RequestID == "" {
		sendError("agent_id and request_id are required")
		return
	}
	feedback := ""
	if msg.Feedback != nil {
		feedback = strings.TrimSpace(*msg.Feedback)
		if feedback == "" {
			sendError("feedback must not be blank")
			return
		}
	}
	if msg.Decision != "allow" && msg.Decision != "deny" {
		sendError("decision must be allow or deny")
		return
	}
	if msg.Decision == "allow" && msg.Feedback != nil {
		sendError("feedback is only valid when denying permission")
		return
	}
	if msg.Decision != "allow" && msg.Option != "" {
		sendError("an option is only valid when allowing permission")
		return
	}
	action := service.PermissionAction{Decision: msg.Decision, Feedback: feedback, Option: msg.Option}
	if err := s.agents.ResolvePermission(context.Background(), msg.AgentID, msg.RequestID, action); err != nil {
		sendError("resolve permission failed: " + err.Error())
	}
}

// handleAgentQuestion answers, or dismisses, a question the agent put to the
// person. The agent's endpoint checks the answers against what it asked.
func (s *Server) handleAgentQuestion(client *wsClient, msg WSMessage) {
	sendError := func(message string) {
		client.send(WSMessage{Type: MsgError, AgentID: msg.AgentID, RequestID: msg.RequestID, Error: message})
	}
	if s.agents == nil {
		sendError("agent service not available")
		return
	}
	if msg.AgentID == "" || msg.RequestID == "" {
		sendError("agent_id and request_id are required")
		return
	}
	if msg.Dismissed && len(msg.Answers) > 0 {
		sendError("a dismissed question takes no answers")
		return
	}
	if !msg.Dismissed && len(msg.Answers) == 0 {
		sendError("answers are required unless the question is dismissed")
		return
	}
	answer := service.QuestionAnswer{Answers: msg.Answers, Dismissed: msg.Dismissed}
	if err := s.agents.AnswerQuestion(context.Background(), msg.AgentID, msg.RequestID, answer); err != nil {
		sendError("answer question failed: " + err.Error())
	}
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
	if s.streams != nil {
		s.streams.clearDetectorWaiting(msg.AgentID)
	}

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

	detection := s.streams.detectPromptState(msg.AgentID, output)
	state := detection.State
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

	// Query tmux for the cursor position so we can restore it after
	// writing the snapshot.  Without this the snapshot renders from
	// the top but live pipe output continues at the real cursor,
	// causing visual artefacts.
	cursorSuffix := ""
	if tmuxPath, lookErr := exec.LookPath("tmux"); lookErr == nil {
		out, err := exec.Command(tmuxPath, "display-message", "-t", sessionName,
			"-p", "#{cursor_y} #{cursor_x}").CombinedOutput()
		if err == nil {
			var row, col int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d %d", &row, &col); err == nil {
				// ANSI CUP is 1-based.
				cursorSuffix = fmt.Sprintf("\x1b[%d;%dH", row+1, col+1)
			}
		}
	}

	snapshot = trimTrailingBlankLines(snapshot)
	snapshot = collapseBlankRuns(snapshot)
	snapshot = strings.ReplaceAll(snapshot, "\n", "\r\n")
	client.send(WSMessage{
		Type:    MsgAgentOutput,
		AgentID: msg.AgentID,
		Content: "\x1b[2J\x1b[H" + snapshot + cursorSuffix,
	})
}
