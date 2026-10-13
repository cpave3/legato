package claudebridge

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// modEvent is one engine event as the Claude Code mod forwards it. Fields a
// given type does not use stay zero.
type modEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Model     string `json:"model"`

	// hello
	Busy     bool           `json:"busy"`
	Messages []seedMessage  `json:"messages"`
	Commands []slashCommand `json:"commands"`
	Context  *contextUsage  `json:"context"`

	// agent_spawn
	ToolUseID   string `json:"toolUseId"`
	Description string `json:"description"`

	// settings
	Effort string `json:"effort"`

	// turn_start, turn_complete, text_delta, thinking_delta
	TurnID string `json:"turnId"`
	Reason string `json:"reason"`
	Step   int    `json:"step"`
	Index  int    `json:"index"`
	Text   string `json:"text"`

	// row
	UUID    string     `json:"uuid"`
	Door    string     `json:"door"`
	Origin  rowOrigin  `json:"origin"`
	AgentID string     `json:"agentId"`
	Message rowMessage `json:"message"`
}

// seedMessage is one transcript message as $.session.messages() returns it,
// which a resumed session hydrates from.
type seedMessage struct {
	Role     string        `json:"role"`
	Text     string        `json:"text"`
	ToolUses []seedToolUse `json:"toolUses"`
}

type seedToolUse struct {
	ToolUseID string         `json:"tool_use_id"`
	Tool      string         `json:"tool"`
	Input     map[string]any `json:"input"`
	// Text is absent while the call is still in flight.
	Text    *string `json:"text"`
	IsError bool    `json:"isError"`
}

// slashCommand is a command the person can run in the session.
type slashCommand struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// contextUsage is the live context window as the status line has it.
type contextUsage struct {
	Tokens *int `json:"tokens"`
	Window int  `json:"window"`
}

// rowOrigin says who caused a transcript row, in the engine's own words.
type rowOrigin struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// rowMessage is a transcript row as session.append hands it to the mod.
type rowMessage struct {
	Type    string         `json:"type"`
	Role    string         `json:"role"`
	IsMeta  bool           `json:"isMeta"`
	Name    string         `json:"name"`
	Content []contentBlock `json:"content"`
}

// contentBlock is a Messages API content block, narrowed to what the bridge reads.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`

	// thinking: what the model lets be read of its thinking, often nothing
	Thinking string `json:"thinking"`

	// tool_use
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`

	// tool_result; its content is a string or a list of blocks
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// resultText returns a tool_result block's content as the model read it.
func (b contentBlock) resultText() string {
	if len(b.Content) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(b.Content, &text) == nil {
		return text
	}
	var parts []contentBlock
	if json.Unmarshal(b.Content, &parts) == nil {
		return joinText(parts)
	}
	return ""
}

// nativeEvent is one native protocol event before the bridge stamps its
// envelope (eventId, sessionId, ts).
type nativeEvent map[string]any

type itemKind int

const (
	itemUser itemKind = iota
	itemAssistant
	itemTool
	// itemReasoning is what the model thought on its way to a reply or a call.
	itemReasoning
)

// item is one entry of the conversation in display order.
type item struct {
	kind itemKind
	text string
	tool *toolCall
}

// toolCall is one tool call as the native panel draws it.
type toolCall struct {
	// key is the call as Claude Code named it (see callKey).
	key       string
	callID    string
	name      string
	args      map[string]any
	summary   string
	result    map[string]any
	startedAt int64
	endedAt   int64
}

// permission is a tool call waiting on a person's decision.
type permission struct {
	requestID string
	tool      string
	command   string
	reason    string
	// call identifies the tool call being decided, by what was asked for: the
	// engine's permission event carries no call ID.
	call string
	// questions are set when the call asks the person something rather than
	// for leave to run: the request is then answered, not allowed or denied.
	questions []question
	// plan is the Markdown plan the call asks the person to approve, if any.
	plan string
	// options are the ways the request can be allowed, when there are several.
	options []permissionOption
}

// timeoutEvent says the request can no longer be answered in Legato.
func (p permission) timeoutEvent() nativeEvent {
	kind := "permission_timeout"
	if p.questions != nil {
		kind = "question_timeout"
	}
	return nativeEvent{"type": kind, "requestId": p.requestID}
}

// callKey identifies a tool call by its tool and arguments. Go marshals map
// keys in order, so equal arguments give equal keys.
func callKey(tool string, input map[string]any) string {
	encoded, err := json.Marshal(input)
	if err != nil {
		return tool
	}
	return tool + "\x00" + string(encoded)
}

// session is the conversation state of one Claude Code session, kept in the
// shape the native panel reads.
type session struct {
	claudeSessionID string
	cwd             string
	// model and effort are the session's own, as Claude Code runs it. The
	// overrides are what was chosen in Legato; the mod applies them to each
	// request, so they hold for this session alone and are saved nowhere.
	model          string
	effort         string
	modelOverride  string
	effortOverride string
	busy           bool

	items    []item
	tools    map[string]*toolCall
	seenRows map[string]bool
	// openStreams are the text blocks streamed this turn that no stored row
	// has settled yet, oldest first.
	openStreams []string
	streamSeen  map[string]bool
	// openThoughts are the thinking blocks streamed this turn that no stored
	// row has settled yet, oldest first, and thoughtText what each streamed.
	openThoughts []string
	thoughtText  map[string]string
	// pending are the permission requests Legato has yet to answer, in the
	// order they were raised.
	pending []permission
	// subagents maps a subagent's loop id to the tool call that spawned it.
	subagents map[string]string
	commands  []slashCommand
	context   contextUsage
	// described is set once the mod has said what the session is.
	described bool
}

func newSession() *session {
	s := &session{}
	s.clearConversation()
	return s
}

// clearConversation drops everything a /clear drops; what describes the
// process itself (cwd, model, commands) stays.
func (s *session) clearConversation() {
	s.items = nil
	s.tools = map[string]*toolCall{}
	s.seenRows = map[string]bool{}
	s.openStreams = nil
	s.streamSeen = map[string]bool{}
	s.openThoughts = nil
	s.thoughtText = map[string]string{}
	s.pending = nil
	s.subagents = map[string]string{}
}

// apply folds one mod event into the session and returns the native events
// subscribers should see for it.
func (s *session) apply(event modEvent, now time.Time) []nativeEvent {
	switch event.Type {
	case "hello":
		// A session described again is another session under the same
		// process (a resume). The panel reads the snapshot again on
		// run_finished, which is how it picks up the replaced conversation.
		described := s.described
		s.applyHello(event, now)
		if described {
			return []nativeEvent{{"type": "run_finished", "reason": "reset"}}
		}
	case "usage":
		return s.applyContext(event.Context)
	case "identity":
		if event.SessionID != "" {
			s.claudeSessionID = event.SessionID
		}
		if event.CWD != "" {
			s.cwd = event.CWD
		}
	case "reset":
		// A /clear: the process goes on under a new Claude session with an
		// empty conversation. The panel re-reads the snapshot on run_finished.
		s.clearConversation()
		s.claudeSessionID = event.SessionID
		if event.CWD != "" {
			s.cwd = event.CWD
		}
		s.busy = false
		// The old conversation's fill says nothing about the new one.
		s.context.Tokens = nil
		return []nativeEvent{{"type": "run_finished", "reason": "reset"}}
	case "model":
		if event.Model == "" || event.Model == s.model {
			return nil
		}
		s.model = event.Model
		if s.modelOverride != "" {
			// Requests still go to the model chosen in Legato.
			return nil
		}
		return s.modelChanged()
	case "settings":
		if event.Effort == s.effort {
			return nil
		}
		s.effort = event.Effort
		if s.effortOverride != "" {
			return nil
		}
		return []nativeEvent{s.settingsChanged()}
	case "agent_spawn":
		return s.applyAgentSpawn(event)
	case "user_note":
		// A message the mod folded into the running turn. Its stored row is
		// framed for the model, so the mod reports the words as sent.
		if strings.TrimSpace(event.Text) == "" {
			return nil
		}
		s.items = append(s.items, item{kind: itemUser, text: event.Text})
		return []nativeEvent{{"type": "user_message", "content": event.Text}}
	case "turn_start":
		s.busy = true
		return []nativeEvent{{"type": "session_started"}}
	case "turn_complete":
		if event.AgentID != "" {
			if _, known := s.subagents[event.AgentID]; !known {
				return nil
			}
			return []nativeEvent{{"type": "subagent_finished", "subagentId": event.AgentID, "reason": runFinishReason(event.Reason)}}
		}
		s.busy = false
		s.openStreams = nil
		s.streamSeen = map[string]bool{}
		s.openThoughts = nil
		s.thoughtText = map[string]string{}
		// A turn that ended took its unanswered permission prompts with it.
		out := s.expirePermissions()
		out = append(out, s.applyContext(event.Context)...)
		return append(out, nativeEvent{"type": "run_finished", "reason": runFinishReason(event.Reason)})
	case "text_delta":
		return s.applyTextDelta(event)
	case "thinking_delta":
		return s.applyThinkingDelta(event)
	case "row":
		return s.applyRow(event, now)
	}
	return nil
}

// applyHello starts the session over from what the mod reports at load: a
// fresh session is empty, a resumed one hydrates from its transcript.
func (s *session) applyHello(event modEvent, now time.Time) {
	s.clearConversation()
	s.described = true
	s.claudeSessionID = event.SessionID
	s.cwd = event.CWD
	s.model = event.Model
	s.busy = event.Busy
	s.commands = event.Commands
	s.context = contextUsage{}
	if event.Context != nil {
		s.context = *event.Context
	}
	for _, message := range event.Messages {
		switch message.Role {
		case "user":
			if text := strings.TrimSpace(message.Text); text != "" && !looksInjected(text) {
				s.items = append(s.items, item{kind: itemUser, text: message.Text})
			}
		case "assistant":
			if strings.TrimSpace(message.Text) != "" && message.Text != emptyMessageText {
				s.items = append(s.items, item{kind: itemAssistant, text: message.Text})
			}
			for _, use := range message.ToolUses {
				if use.ToolUseID == "" || s.tools[use.ToolUseID] != nil {
					continue
				}
				call := s.addToolCall(use.ToolUseID, use.Tool, use.Input, now)
				if use.Text != nil {
					call.result = nativeToolResult(call.name, *use.Text, use.IsError)
					call.endedAt = now.UnixMilli()
				}
			}
		}
	}
}

// injectedPrefixes open the user-role text the engine writes beside the
// conversation. A transcript read back carries no provenance, so a resumed
// session tells them from a person's prompt by how they begin.
var injectedPrefixes = []string{
	"<system-reminder>", "<task-notification>", "<command-name>", "<command-message>",
	"<local-command-", "<bash-input>", "<bash-stdout>", "<user-prompt-submit-hook>",
	"Caveat: The messages below were generated by the user while running local commands",
}

func looksInjected(text string) bool {
	for _, prefix := range injectedPrefixes {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func (s *session) applyContext(usage *contextUsage) []nativeEvent {
	if usage == nil || usage.Window == 0 {
		return nil
	}
	s.context = *usage
	if usage.Tokens == nil {
		return nil
	}
	return []nativeEvent{{"type": "usage_updated", "contextWindow": usage.Window, "usedContextTokens": *usage.Tokens}}
}

func (s *session) applyAgentSpawn(event modEvent) []nativeEvent {
	if event.AgentID == "" || event.ToolUseID == "" || s.tools[event.ToolUseID] == nil {
		return nil
	}
	s.subagents[event.AgentID] = event.ToolUseID
	return []nativeEvent{{
		"type": "subagent_spawned", "parentCallId": event.ToolUseID,
		"subagentId": event.AgentID, "purpose": event.Description,
	}}
}

// applySubagentRow reports a subagent's own row as activity under the call
// that spawned it; its rows never join the main conversation.
func (s *session) applySubagentRow(event modEvent) []nativeEvent {
	if _, known := s.subagents[event.AgentID]; !known {
		return nil
	}
	wrap := func(inner nativeEvent) nativeEvent {
		return nativeEvent{"type": "subagent_event", "subagentId": event.AgentID, "event": inner}
	}
	var out []nativeEvent
	for _, block := range event.Message.Content {
		switch block.Type {
		case "text":
			if event.Message.Role == "assistant" && strings.TrimSpace(block.Text) != "" {
				out = append(out, wrap(nativeEvent{"type": "assistant_text_done", "text": block.Text}))
			}
		case "thinking":
			if strings.TrimSpace(block.Thinking) != "" {
				out = append(out, wrap(nativeEvent{"type": "reasoning_text_done", "text": block.Thinking}))
			}
		case "tool_use":
			_, _, summary := nativeToolCall(block.Name, block.Input)
			out = append(out, wrap(nativeEvent{
				"type": "tool_call_start", "callId": block.ID, "name": block.Name,
				"display": map[string]any{"summary": summary},
			}))
		case "tool_result":
			if block.IsError {
				out = append(out, wrap(nativeEvent{"type": "tool_call_error", "callId": block.ToolUseID, "error": truncate(firstLine(block.resultText()), 200)}))
			}
		}
	}
	return out
}

// effectiveModel is the model the session's requests go to.
func (s *session) effectiveModel() string {
	if s.modelOverride != "" {
		return s.modelOverride
	}
	return s.model
}

func (s *session) effectiveEffort() string {
	if s.effortOverride != "" {
		return s.effortOverride
	}
	return s.effort
}

func (s *session) modelRef() string {
	return modelProvider + "/" + s.effectiveModel()
}

// modelChanged announces the model requests now go to, with its settings: the
// panel drops the settings it held when the model under them changes.
func (s *session) modelChanged() []nativeEvent {
	return []nativeEvent{{"type": "model_changed", "to": s.modelRef()}, s.settingsChanged()}
}

func (s *session) settingsChanged() nativeEvent {
	return nativeEvent{"type": "model_settings_changed", "settingsState": s.settingsState()}
}

// requestPermission opens a request for a person's decision on a tool call.
// It returns nil for a request that is already open.
func (s *session) requestPermission(requestID, tool string, input map[string]any, reason string, suggestions []json.RawMessage) []nativeEvent {
	if requestID == "" || s.pendingIndex(requestID) >= 0 {
		return nil
	}
	request := permission{requestID: requestID, tool: tool, command: permissionCommand(tool, input), reason: reason, call: callKey(tool, input)}
	if request.questions = questionsOf(tool, input); request.questions != nil {
		s.pending = append(s.pending, request)
		return []nativeEvent{{"type": "question_request", "requestId": request.requestID, "questions": request.questions}}
	}
	request.plan = planOf(tool, input)
	request.options = allowOptions(tool, suggestions)
	s.pending = append(s.pending, request)
	event := nativeEvent{
		"type": "permission_request", "requestId": request.requestID, "tool": request.tool,
		"target": permissionTarget, "command": request.command, "reason": request.reason,
	}
	if request.plan != "" {
		event["plan"] = request.plan
	}
	if len(request.options) > 0 {
		event["options"] = request.options
	}
	return []nativeEvent{event}
}

// resolvePermission settles an open request with Legato's decision. An allow
// may name which of the request's options it takes; it returns the request
// and that option so the caller can tell the mod what the allow carries.
func (s *session) resolvePermission(requestID, decision, optionID string) ([]nativeEvent, permission, permissionOption, error) {
	index := s.pendingIndex(requestID)
	if index < 0 || s.pending[index].questions != nil {
		return nil, permission{}, permissionOption{}, errNoSuchRequest
	}
	request := s.pending[index]
	event := nativeEvent{"type": "permission_resolved", "requestId": requestID, "decision": decision}
	var option permissionOption
	if decision == "allow" {
		var err error
		if option, err = request.chosenOption(optionID); err != nil {
			return nil, permission{}, permissionOption{}, err
		}
		if option.ID != "" {
			event["option"] = option.ID
		}
	} else if optionID != "" {
		return nil, permission{}, permissionOption{}, fmt.Errorf("%w: an option is a way to allow, not to deny", errInvalidAnswer)
	}
	s.pending = append(s.pending[:index], s.pending[index+1:]...)
	return []nativeEvent{event}, request, option, nil
}

// expirePermission closes one request nobody answered in Legato; the prompt
// went back to the terminal (or its tool call was dropped).
func (s *session) expirePermission(requestID string) []nativeEvent {
	index := s.pendingIndex(requestID)
	if index < 0 {
		return nil
	}
	expired := s.pending[index]
	s.pending = append(s.pending[:index], s.pending[index+1:]...)
	return []nativeEvent{expired.timeoutEvent()}
}

// expirePermissionsFor closes the requests open on one tool call.
func (s *session) expirePermissionsFor(call string) []nativeEvent {
	var out []nativeEvent
	kept := s.pending[:0]
	for _, request := range s.pending {
		if request.call == call {
			out = append(out, request.timeoutEvent())
			continue
		}
		kept = append(kept, request)
	}
	s.pending = kept
	return out
}

func (s *session) expirePermissions() []nativeEvent {
	var out []nativeEvent
	for _, request := range s.pending {
		out = append(out, request.timeoutEvent())
	}
	s.pending = nil
	return out
}

func (s *session) pendingIndex(requestID string) int {
	for index, request := range s.pending {
		if request.requestID == requestID {
			return index
		}
	}
	return -1
}

// permissionTarget is the only target the native panel models: the machine
// the agent runs on.
const permissionTarget = "host"

// runFinishReason maps why a Claude turn ended onto the native protocol's
// word for it; "stop" is the only one the panel treats as a clean finish.
func runFinishReason(reason string) string {
	switch reason {
	case "", "answer":
		return "stop"
	}
	return reason
}

func (s *session) applyTextDelta(event modEvent) []nativeEvent {
	if event.AgentID != "" || event.Text == "" {
		return nil
	}
	streamID := fmt.Sprintf("%s:%d:%d", event.TurnID, event.Step, event.Index)
	if !s.streamSeen[streamID] {
		s.streamSeen[streamID] = true
		s.openStreams = append(s.openStreams, streamID)
	}
	return []nativeEvent{{"type": "assistant_text_delta", "id": streamID, "delta": event.Text}}
}

// applyThinkingDelta is a piece of the model's thinking as it streams. What
// streamed is kept: the row that settles the block often holds none of it.
func (s *session) applyThinkingDelta(event modEvent) []nativeEvent {
	if event.AgentID != "" || event.Text == "" {
		return nil
	}
	streamID := fmt.Sprintf("%s:%d:%d", event.TurnID, event.Step, event.Index)
	if _, open := s.thoughtText[streamID]; !open {
		s.openThoughts = append(s.openThoughts, streamID)
	}
	s.thoughtText[streamID] += event.Text
	return []nativeEvent{{"type": "reasoning_text_delta", "id": streamID, "delta": event.Text}}
}

func (s *session) applyRow(event modEvent, now time.Time) []nativeEvent {
	if event.UUID != "" {
		if s.seenRows[event.UUID] {
			return nil
		}
		s.seenRows[event.UUID] = true
	}
	if event.AgentID != "" {
		return s.applySubagentRow(event)
	}
	switch event.Message.Role {
	case "assistant":
		return s.applyAssistantRow(event, now)
	case "user":
		if results := s.applyToolResults(event, now); results != nil {
			return results
		}
		if !isPersonFacingPrompt(event) {
			return nil
		}
		text := joinText(event.Message.Content)
		if strings.TrimSpace(text) == "" {
			return nil
		}
		s.items = append(s.items, item{kind: itemUser, text: text})
		return []nativeEvent{{"type": "user_message", "content": text}}
	}
	return nil
}

func (s *session) applyAssistantRow(event modEvent, now time.Time) []nativeEvent {
	var out []nativeEvent
	for index, block := range event.Message.Content {
		switch block.Type {
		case "thinking":
			// A stored thinking block settles the oldest one still streaming.
			// The transcript keeps the block the model signed, which for most
			// models has no text: what streamed is then what there is to show.
			streamID := fmt.Sprintf("%s:%d", event.UUID, index)
			text := block.Thinking
			if len(s.openThoughts) > 0 {
				streamID = s.openThoughts[0]
				s.openThoughts = s.openThoughts[1:]
				if strings.TrimSpace(text) == "" {
					text = s.thoughtText[streamID]
				}
				delete(s.thoughtText, streamID)
			}
			if strings.TrimSpace(text) == "" {
				continue
			}
			s.items = append(s.items, item{kind: itemReasoning, text: text})
			out = append(out, nativeEvent{"type": "reasoning_text_done", "id": streamID, "text": text})
		case "text":
			if strings.TrimSpace(block.Text) == "" || block.Text == emptyMessageText {
				continue
			}
			// A stored text block settles the oldest block still streaming;
			// one that never streamed (a resumed turn, a host that does not
			// stream) stands alone under its row.
			streamID := fmt.Sprintf("%s:%d", event.UUID, index)
			if len(s.openStreams) > 0 {
				streamID = s.openStreams[0]
				s.openStreams = s.openStreams[1:]
			}
			s.items = append(s.items, item{kind: itemAssistant, text: block.Text})
			out = append(out, nativeEvent{"type": "assistant_text_done", "id": streamID, "text": block.Text})
		case "tool_use":
			if block.ID == "" || s.tools[block.ID] != nil {
				continue
			}
			call := s.addToolCall(block.ID, block.Name, block.Input, now)
			out = append(out, nativeEvent{
				"type": "tool_call_start", "callId": call.callID, "name": call.name,
				"args": call.args, "display": map[string]any{"summary": call.summary},
			})
		}
	}
	return out
}

func (s *session) addToolCall(id, tool string, input map[string]any, now time.Time) *toolCall {
	name, args, summary := nativeToolCall(tool, input)
	call := &toolCall{key: callKey(tool, input), callID: id, name: name, args: args, summary: summary, startedAt: now.UnixMilli()}
	s.tools[id] = call
	s.items = append(s.items, item{kind: itemTool, tool: call})
	return call
}

// applyToolResults settles the tool calls a user row answers. It returns nil
// when the row carries no tool result, so the caller reads it as a prompt.
func (s *session) applyToolResults(event modEvent, now time.Time) []nativeEvent {
	var out []nativeEvent
	answered := false
	for _, block := range event.Message.Content {
		if block.Type != "tool_result" {
			continue
		}
		answered = true
		call := s.tools[block.ToolUseID]
		if call == nil {
			continue
		}
		// Claude Code keeps its own dialog open while Legato is asked. A
		// result for the call being decided means the person answered there.
		out = append(out, s.expirePermissionsFor(call.key)...)
		call.result = nativeToolResult(call.name, block.resultText(), block.IsError)
		call.endedAt = now.UnixMilli()
		out = append(out, nativeEvent{
			"type": "tool_call_result", "callId": call.callID, "result": call.result,
			"display": map[string]any{"summary": call.summary},
		})
	}
	if answered && out == nil {
		return []nativeEvent{}
	}
	return out
}

// emptyMessageText is what the engine stores for a row left with no block.
const emptyMessageText = "(no content)"

// machineOrigins are the senders of prompt-door rows no person wrote: a
// background task reporting in, a routine firing, an observer's digest.
var machineOrigins = map[string]bool{
	"task-notification": true, "scheduled-trigger": true, "auto-continuation": true,
	"observer": true, "observer-activity": true, "projects-relay": true,
}

// isPersonFacingPrompt reports whether a user row is a message to the agent
// (typed at the terminal, sent from Legato or by a peer) rather than
// something the engine wrote beside the conversation. Origins are excluded,
// not listed: a prompt from a channel the engine cannot name is still a
// prompt, and that is how a headless host's arrives.
func isPersonFacingPrompt(event modEvent) bool {
	if event.Message.Type != "user" || event.Message.IsMeta {
		return false
	}
	switch event.Door {
	case "prompt", "delivery":
		return !machineOrigins[event.Origin.Kind]
	}
	return false
}

func joinText(blocks []contentBlock) string {
	var out strings.Builder
	for _, block := range blocks {
		if block.Type == "text" {
			out.WriteString(block.Text)
		}
	}
	return out.String()
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

type snapshotPermission struct {
	RequestID string `json:"requestId"`
	Tool      string `json:"tool"`
	Target    string `json:"target"`
	Command   string `json:"command"`
	Reason    string `json:"reason,omitempty"`
	CWD       string `json:"cwd"`
	Plan      string `json:"plan,omitempty"`
	// Options are the ways the request can be allowed, when there are several.
	Options []permissionOption `json:"options,omitempty"`
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
	// PendingPermissions is always present: the panel reads an absent list
	// as "unknown" and keeps showing requests that were since answered.
	PendingPermissions []snapshotPermission `json:"pendingPermissions"`
	// PendingQuestions is always present for the same reason.
	PendingQuestions []snapshotQuestion `json:"pendingQuestions"`
}

type snapshotQuestion struct {
	RequestID string     `json:"requestId"`
	Questions []question `json:"questions"`
}

func (s *session) snapshot(taskID string) sessionSnapshot {
	// The native panel drops a leading user message, which for Chimera is the
	// launch prompt. A Claude session has no such message, so an empty system
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
		case itemReasoning:
			// A thought opens the step it led to: the reply or the calls that
			// follow join it.
			flush()
			open = []snapshotPart{{Type: "reasoning", Text: entry.text}}
		case itemAssistant:
			// A reply follows the thought that led to it in the same step.
			if len(open) != 1 || open[0].Type != "reasoning" {
				flush()
			}
			open = append(open, snapshotPart{Type: "text", Text: entry.text})
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
	pending := make([]snapshotPermission, 0, len(s.pending))
	questions := []snapshotQuestion{}
	for _, request := range s.pending {
		if request.questions != nil {
			questions = append(questions, snapshotQuestion{RequestID: request.requestID, Questions: request.questions})
			continue
		}
		pending = append(pending, snapshotPermission{
			RequestID: request.requestID, Tool: request.tool, Target: permissionTarget,
			Command: request.command, Reason: request.reason, CWD: s.cwd,
			Plan: request.plan, Options: request.options,
		})
	}
	settings := s.settingsState()
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
		PendingPermissions:     pending,
		PendingQuestions:       questions,
	}
}

const modelProvider = "anthropic"

func (s *session) status() string {
	for _, request := range s.pending {
		if request.questions == nil {
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
