package codexbridge

import (
	"strconv"
	"strings"
	"time"
)

type itemKind int

const (
	itemUser itemKind = iota
	itemAssistant
	itemTool
)

// item is one entry of the conversation as the panel shows it.
type item struct {
	kind itemKind
	id   string
	text string
	// settled is false for a reply still streaming: its text is not final.
	settled bool
	tool    *toolCall
}

type toolCall struct {
	callID    string
	name      string
	args      map[string]any
	summary   string
	result    map[string]any
	startedAt int64
	endedAt   int64
}

type contextUsage struct {
	tokens *int
	window int
}

// threadInfo is what the app server says of a thread when it starts or is
// joined.
type threadInfo struct {
	ID              string  `json:"id"`
	Ephemeral       bool    `json:"ephemeral"`
	ParentThreadID  *string `json:"parentThreadId"`
	CWD             string  `json:"cwd"`
	Model           *string `json:"model"`
	ReasoningEffort *string `json:"reasoningEffort"`
	Status          struct {
		Type string `json:"type"`
	} `json:"status"`
	Turns []turnInfo `json:"turns"`
}

type turnInfo struct {
	ID     string       `json:"id"`
	Status string       `json:"status"`
	Items  []threadItem `json:"items"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// isOwnConversation reports whether the thread is one a person talks to.
// Codex also runs short-lived helper threads (naming a session) and
// subagent threads on the same server; neither is the session's conversation.
func (t threadInfo) isOwnConversation() bool {
	return t.ID != "" && !t.Ephemeral && (t.ParentThreadID == nil || *t.ParentThreadID == "")
}

// session is the conversation state of one Codex thread, kept in the shape
// the native panel reads.
type session struct {
	// threadID is the thread the Codex terminal UI is on, which the bridge
	// follows. joined is set once the bridge receives that thread's events:
	// a thread can only be joined after its first turn has started.
	threadID string
	joined   bool
	cwd      string
	// model and effort are the thread's own. The overrides are what was
	// chosen in Legato, applied to the next turn the bridge starts.
	model          string
	effort         string
	modelOverride  string
	effortOverride string
	busy           bool
	// turnID is the turn in progress, once the bridge knows it.
	turnID string

	items []item
	byID  map[string]int
	// pending are the requests Legato has yet to answer, in the order raised.
	pending []request
	context contextUsage
	// appended is context added from Legato for the next turn to carry.
	appended []string
	// held are messages sent before the terminal had opened a thread. Each
	// is already shown, as the item heldItems names.
	held      []string
	heldItems []int
	// echoes are the turn inputs the bridge sent, by the text Codex reports
	// them back as: how each is to be shown when that report arrives.
	echoes map[string]echo
}

// echo says how an input the bridge sent is shown when Codex reports it as a
// message of the turn.
type echo struct {
	// display is what the person typed, without the context attached to it.
	display string
	// shownAs is the item already showing it, or -1.
	shownAs int
}

func newSession() *session {
	s := &session{}
	s.clearConversation()
	return s
}

// hold keeps a message for a terminal that has no thread yet, and shows it:
// the person sees it land although nothing can read it so far.
func (s *session) hold(text string) []nativeEvent {
	s.held = append(s.held, text)
	s.heldItems = append(s.heldItems, s.add(item{kind: itemUser, id: "held-" + strconv.Itoa(len(s.held)), text: text, settled: true}))
	return []nativeEvent{{"type": "user_message", "content": text}}
}

func (s *session) clearConversation() {
	s.items = nil
	s.byID = map[string]int{}
	s.echoes = map[string]echo{}
	s.held, s.heldItems = nil, nil
	s.pending = nil
	s.joined = false
	s.busy = false
	s.turnID = ""
	s.context.tokens = nil
}

// follow makes thread the conversation the bridge serves. A second thread on
// the same server is the person starting over in the terminal (/new): the
// panel re-reads the snapshot on run_finished and so shows the new one.
func (s *session) follow(thread threadInfo) []nativeEvent {
	replaced := s.threadID != "" && s.threadID != thread.ID
	if replaced {
		s.clearConversation()
	}
	s.threadID = thread.ID
	s.describe(thread)
	if replaced {
		return []nativeEvent{{"type": "run_finished", "reason": "reset"}}
	}
	return nil
}

func (s *session) describe(thread threadInfo) {
	if thread.CWD != "" {
		s.cwd = thread.CWD
	}
	if thread.Model != nil && *thread.Model != "" {
		s.model = *thread.Model
	}
	if thread.ReasoningEffort != nil {
		s.effort = *thread.ReasoningEffort
	}
}

// load applies a joined thread's history. Nothing of it was known before the
// join, so every entry is announced as it would have been live.
func (s *session) load(thread threadInfo, now time.Time) []nativeEvent {
	s.joined = true
	s.describe(thread)
	var out []nativeEvent
	for index, turn := range thread.Turns {
		inProgress := turn.Status == "inProgress" && index == len(thread.Turns)-1
		if inProgress {
			out = append(out, s.startTurn(turn.ID)...)
		}
		for _, entry := range turn.Items {
			// An entry of a turn that is over is over too, whatever it says.
			out = append(out, s.applyItem(entry, !inProgress || entry.isSettled() || entry.Type == "userMessage", now)...)
		}
	}
	return out
}

func (s *session) startTurn(turnID string) []nativeEvent {
	if turnID != "" {
		s.turnID = turnID
	}
	if s.busy {
		return nil
	}
	s.busy = true
	return []nativeEvent{{"type": "session_started"}}
}

// finishTurn ends the turn in progress. Requests nobody answered in Legato
// went with it.
func (s *session) finishTurn(status, failure string) []nativeEvent {
	s.busy = false
	s.turnID = ""
	out := s.expireRequests()
	for index := range s.items {
		s.items[index].settled = true
	}
	if status == "failed" && failure != "" {
		out = append(out, nativeEvent{"type": "error", "error": failure})
	}
	return append(out, nativeEvent{"type": "run_finished", "reason": runFinishReason(status)})
}

// runFinishReason maps how a Codex turn ended onto the native protocol's
// word for it; "stop" is the only one the panel treats as a clean finish.
func runFinishReason(status string) string {
	switch status {
	case "", "completed":
		return "stop"
	case "failed":
		return "error"
	}
	return status
}

// applyItem folds one item of a turn into the conversation. settled says the
// item is in its final form (item/completed, or history).
func (s *session) applyItem(entry threadItem, settled bool, now time.Time) []nativeEvent {
	if entry.ID == "" {
		return nil
	}
	index, known := s.byID[entry.ID]
	switch entry.Type {
	case "userMessage":
		text := entry.userText()
		if known || strings.TrimSpace(text) == "" {
			return nil
		}
		if sent, ok := s.echoes[text]; ok {
			delete(s.echoes, text)
			if sent.shownAs >= 0 {
				// Shown since it was sent; this is Codex catching up.
				s.byID[entry.ID] = sent.shownAs
				return nil
			}
			text = sent.display
		}
		s.add(item{kind: itemUser, id: entry.ID, text: text, settled: true})
		return []nativeEvent{{"type": "user_message", "content": text}}
	case "agentMessage", "plan":
		if !known {
			index = s.add(item{kind: itemAssistant, id: entry.ID})
		}
		if !settled {
			return nil
		}
		s.items[index].text = entry.Text
		s.items[index].settled = true
		if strings.TrimSpace(entry.Text) == "" {
			return nil
		}
		return []nativeEvent{{"type": "assistant_text_done", "id": entry.ID, "text": entry.Text}}
	}

	name, args, summary, isTool := nativeTool(entry)
	if !isTool {
		return nil
	}
	var out []nativeEvent
	if !known {
		call := &toolCall{callID: entry.ID, name: name, args: args, summary: summary, startedAt: now.UnixMilli()}
		index = s.add(item{kind: itemTool, id: entry.ID, tool: call, settled: true})
		out = append(out, nativeEvent{
			"type": "tool_call_start", "callId": call.callID, "name": call.name, "args": call.args,
			"display": map[string]any{"summary": call.summary},
		})
	}
	call := s.items[index].tool
	if call == nil || call.result != nil || !(settled || entry.isSettled()) {
		return out
	}
	// A file change grows while it is drafted; its final form is the record.
	call.args, call.summary = args, summary
	call.result = nativeResult(entry)
	call.endedAt = now.UnixMilli()
	if entry.DurationMs != nil {
		call.endedAt = call.startedAt + *entry.DurationMs
	}
	// A call that is over cannot still be waiting on anyone.
	out = append(out, s.expireRequestsFor(entry.ID)...)
	return append(out, nativeEvent{
		"type": "tool_call_result", "callId": call.callID, "result": call.result,
		"display": map[string]any{"summary": call.summary},
	})
}

func (s *session) add(entry item) int {
	s.items = append(s.items, entry)
	s.byID[entry.id] = len(s.items) - 1
	return len(s.items) - 1
}

// applyDelta is a piece of a reply as it streams.
func (s *session) applyDelta(itemID, delta string) []nativeEvent {
	if itemID == "" || delta == "" {
		return nil
	}
	if _, known := s.byID[itemID]; !known {
		s.add(item{kind: itemAssistant, id: itemID})
	}
	return []nativeEvent{{"type": "assistant_text_delta", "id": itemID, "delta": delta}}
}

func (s *session) applyUsage(lastTokens, window int) []nativeEvent {
	if window <= 0 {
		return nil
	}
	s.context = contextUsage{tokens: &lastTokens, window: window}
	return []nativeEvent{{"type": "usage_updated", "contextWindow": window, "usedContextTokens": lastTokens}}
}

func (s *session) status() string {
	for _, pending := range s.pending {
		if pending.questions == nil {
			return "waiting_for_permission"
		}
	}
	if len(s.pending) > 0 {
		return "waiting_for_input"
	}
	if s.busy {
		return "running"
	}
	return "idle"
}

type snapshotModel struct {
	ProviderID       string            `json:"providerId"`
	ModelID          string            `json:"modelId"`
	Settings         map[string]any    `json:"settings"`
	SettingsDefaults map[string]any    `json:"settingsDefaults"`
	Capabilities     modelCapabilities `json:"capabilities"`
}

type snapshotPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type snapshotMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type snapshotToolCall struct {
	CallID    string          `json:"callId"`
	Name      string          `json:"name"`
	Args      map[string]any  `json:"args"`
	Result    map[string]any  `json:"result,omitempty"`
	StartedAt int64           `json:"startedAt,omitempty"`
	EndedAt   int64           `json:"endedAt,omitempty"`
	Display   snapshotDisplay `json:"display"`
}

type snapshotDisplay struct {
	Summary string `json:"summary,omitempty"`
}

// sessionSnapshot is GET /v1/sessions/:id as the native panel hydrates from it.
type sessionSnapshot struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	CWD       string             `json:"cwd"`
	Model     snapshotModel      `json:"model"`
	Mode      string             `json:"mode"`
	Status    string             `json:"status"`
	Messages  []snapshotMessage  `json:"messages"`
	ToolCalls []snapshotToolCall `json:"toolCalls"`
	// ModelSettingsOverrides holds what was chosen in Legato, by model ref.
	ModelSettingsOverrides map[string]map[string]any `json:"modelSettingsOverrides"`
	// Both lists are always present: the panel reads an absent list as
	// "unknown" and keeps showing requests that were since answered.
	PendingPermissions []snapshotPermission `json:"pendingPermissions"`
	PendingQuestions   []snapshotQuestion   `json:"pendingQuestions"`
	ContextWindow      int                  `json:"contextWindow,omitempty"`
	UsedContextTokens  *int                 `json:"usedContextTokens,omitempty"`
}

func (s *session) snapshot(taskID string, models []modelInfo) sessionSnapshot {
	// The native panel drops a leading user message, which for Chimera is the
	// launch prompt. A Codex thread has no such message, so an empty system
	// entry holds that slot and every real prompt is rendered.
	messages := []snapshotMessage{{Role: "system", Content: ""}}
	toolCalls := []snapshotToolCall{}
	// The panel pairs each assistant message with the next tool calls of the
	// flat list by counting its "tool-call" parts, so a call joins the
	// assistant message it follows and opens one of its own otherwise.
	var open []snapshotPart
	flush := func() {
		if open != nil {
			messages = append(messages, snapshotMessage{Role: "assistant", Content: open})
			open = nil
		}
	}
	for _, entry := range s.items {
		switch entry.kind {
		case itemUser:
			flush()
			messages = append(messages, snapshotMessage{Role: "user", Content: entry.text})
		case itemAssistant:
			// A reply still streaming is not in the snapshot: its pieces
			// reach the panel as events and would otherwise show twice.
			if !entry.settled || strings.TrimSpace(entry.text) == "" {
				continue
			}
			flush()
			open = []snapshotPart{{Type: "text", Text: entry.text}}
		case itemTool:
			open = append(open, snapshotPart{Type: "tool-call"})
			toolCalls = append(toolCalls, snapshotToolCall{
				CallID: entry.tool.callID, Name: entry.tool.name, Args: entry.tool.args,
				Result: entry.tool.result, StartedAt: entry.tool.startedAt, EndedAt: entry.tool.endedAt,
				Display: snapshotDisplay{Summary: entry.tool.summary},
			})
		}
	}
	flush()
	permissions, questions := s.pendingSnapshot()
	settings := s.settingsState(models)
	return sessionSnapshot{
		ID:   taskID,
		Name: taskID,
		CWD:  s.cwd,
		Model: snapshotModel{
			ProviderID: modelProvider, ModelID: s.effectiveModel(),
			Settings: settings.Effective, SettingsDefaults: settings.Defaults, Capabilities: settings.Capabilities,
		},
		Status:                 s.status(),
		Messages:               messages,
		ToolCalls:              toolCalls,
		ModelSettingsOverrides: map[string]map[string]any{settings.Model: settings.Overrides},
		PendingPermissions:     permissions,
		PendingQuestions:       questions,
		ContextWindow:          s.context.window,
		UsedContextTokens:      s.context.tokens,
	}
}
