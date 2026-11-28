package server

import (
	"strings"
	"time"
	"unicode"

	"github.com/cpave3/legato/internal/engine/prompt"
)

const (
	// MsgAnswerTerminalPrompt answers the prompt an agent's terminal shows
	// with one of the answers Legato read off it (client → server).
	MsgAnswerTerminalPrompt = "answer_terminal_prompt"
	// MsgTerminalPromptAnswered says the keys went in, with what the screen
	// shows after them (server → client).
	MsgTerminalPromptAnswered = "terminal_prompt_answered"
)

// terminalAnswerSettle is how long the terminal is given to redraw after an
// answer before its screen is read again.
var terminalAnswerSettle = 400 * time.Millisecond

// answerablePrompts are the prompt types whose actions answer a question.
var answerablePrompts = map[prompt.PromptType]bool{
	prompt.Menu:         true,
	prompt.ToolApproval: true,
	prompt.PlanApproval: true,
	prompt.Question:     true,
}

// handleAnswerTerminalPrompt answers a prompt in an agent's terminal, for a
// prompt that never reached Legato as a request of its own. Unlike
// send_keys it serves native agents too, so it presses only keys it has
// just read off the screen as an answer: the screen is read again, and the
// keys must be one of its actions. A row that takes text (Content) gets its
// text typed once the cursor is on it. The tmux delivery guard still applies.
func (s *Server) handleAnswerTerminalPrompt(client *wsClient, msg WSMessage) {
	refuse := func(code, message string, state *prompt.PromptState) {
		client.send(WSMessage{Type: MsgError, AgentID: msg.AgentID, RequestID: msg.RequestID, Code: code, Error: message, Prompt: state})
	}
	if s.tmux == nil {
		refuse("unavailable", "tmux not available", nil)
		return
	}
	if msg.AgentID == "" || msg.RequestID == "" || msg.Keys == "" {
		refuse("invalid", "agent_id, request_id and keys are required", nil)
		return
	}
	text := msg.Content
	if strings.IndexFunc(text, unicode.IsControl) >= 0 {
		refuse("invalid", "the answer must be one line of text", nil)
		return
	}

	sessionName := "legato-" + msg.AgentID
	output, err := s.tmux.Capture(sessionName)
	if err != nil {
		refuse("capture_failed", "could not read the terminal: "+err.Error(), nil)
		return
	}
	state := s.streams.detectPromptState(msg.AgentID, output).State
	var chosen *prompt.Action
	if answerablePrompts[state.Type] {
		for index := range state.Actions {
			if state.Actions[index].Keys == msg.Keys {
				chosen = &state.Actions[index]
				break
			}
		}
	}
	if chosen == nil {
		refuse("prompt_changed", "the terminal no longer shows that answer", &state)
		return
	}
	if chosen.TakesText && strings.TrimSpace(text) == "" {
		refuse("text_required", "type an answer for "+chosen.Label, &state)
		return
	}
	if !chosen.TakesText && text != "" {
		refuse("invalid", chosen.Label+" takes no text", &state)
		return
	}

	s.streams.clearDetectorWaiting(msg.AgentID)
	if err := s.typeTerminalAnswer(sessionName, *chosen, text); err != nil {
		refuse("send_failed", "the keys did not reach the terminal: "+err.Error(), nil)
		return
	}

	time.Sleep(terminalAnswerSettle)
	after := prompt.PromptState{Type: prompt.FreeText}
	if output, err := s.tmux.Capture(sessionName); err == nil {
		after = s.streams.detectPromptState(msg.AgentID, output).State
	}
	client.send(WSMessage{Type: MsgTerminalPromptAnswered, AgentID: msg.AgentID, RequestID: msg.RequestID, Prompt: &after})
}

// typeTerminalAnswer presses an action's keys. For a row that takes text,
// the cursor moves to it, the text is typed, and Enter answers with it only
// where Enter does not untick it again.
func (s *Server) typeTerminalAnswer(sessionName string, action prompt.Action, text string) error {
	keys := strings.Fields(action.Keys)
	if action.TakesText && len(keys) > 0 && keys[len(keys)-1] == "Enter" {
		keys = keys[:len(keys)-1]
	}
	for _, key := range keys {
		if err := s.tmux.SendKey(sessionName, key); err != nil {
			return err
		}
	}
	if !action.TakesText {
		return nil
	}
	if action.TextSubmits {
		return s.tmux.SendKeysLine(sessionName, text)
	}
	return s.tmux.SendKeys(sessionName, text)
}
