package service

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"

	"github.com/cpave3/legato/internal/engine/chimera"
)

// conversationTextLimit is the most characters of one message that are
// handed on. The tail is for a glance, not for reading a whole reply.
const conversationTextLimit = 2000

// ErrNoNativeEndpoint means the agent is driven through its terminal only:
// there is no conversation to read, only what its pane shows.
var ErrNoNativeEndpoint = errors.New("agent has no native endpoint")

// ConversationEntryKind says what one entry of a conversation is.
type ConversationEntryKind string

const (
	EntryUser      ConversationEntryKind = "user"
	EntryAssistant ConversationEntryKind = "assistant"
	EntryTool      ConversationEntryKind = "tool"
)

// The states of a tool call.
const (
	ToolRunning = "running"
	ToolDone    = "done"
	ToolFailed  = "failed"
)

// ConversationEntry is one thing said or done in an agent's conversation.
type ConversationEntry struct {
	Kind ConversationEntryKind
	// Text is the message, or for a tool call one line that says what the
	// call is for.
	Text string
	// Tool, Command and State describe a tool call: the tool's name, the
	// command itself when the call runs one, and how the call stands.
	Tool    string
	Command string
	State   string
}

// ConversationPermission is something an agent waits to be allowed.
type ConversationPermission struct {
	Tool    string
	Command string
	Reason  string
	// Plan is the start of a plan the agent asks the person to approve.
	Plan string
}

// ConversationQuestion is a question an agent put to the person.
type ConversationQuestion struct {
	Text    string
	Options []string
}

// ConversationTail is the end of a native agent's conversation, as its
// endpoint reports it.
type ConversationTail struct {
	// Status is the endpoint's own: "running", "waiting_for_permission",
	// "waiting_for_input", or "idle".
	Status string
	// Entries are the newest things said and done, oldest first.
	Entries []ConversationEntry
	// Permission and Question are the first request of each kind that is
	// still open; nil when there is none.
	Permission *ConversationPermission
	Question   *ConversationQuestion
	// ContextUsed of ContextWindow tokens are in the model's context. The
	// window is zero when the endpoint did not say.
	ContextUsed   int
	ContextWindow int
}

// AgentConversationReader reads what a native agent said and did from its
// endpoint: the same data the web UI's native panel is drawn from.
type AgentConversationReader interface {
	// ConversationTail returns the newest limit entries of the agent's
	// conversation. It returns ErrNoNativeEndpoint for an agent that has
	// none to ask.
	ConversationTail(ctx context.Context, taskID string, limit int) (*ConversationTail, error)
}

func (a *agentService) ConversationTail(ctx context.Context, taskID string, limit int) (*ConversationTail, error) {
	session, err := a.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if session.ChimeraEndpoint == nil || session.ChimeraSessionID == nil {
		return nil, ErrNoNativeEndpoint
	}
	client := chimera.NewClient(*session.ChimeraEndpoint)
	snapshot, err := client.GetSession(ctx, *session.ChimeraSessionID)
	if err != nil {
		return nil, err
	}
	tail := conversationTail(snapshot, limit)
	// How full the context is comes from a second request. The conversation
	// is worth having without it.
	if sessionContext, err := client.GetSessionContext(ctx, *session.ChimeraSessionID); err == nil {
		tail.ContextWindow = sessionContext.ContextWindow
		if sessionContext.LastPromptTokens != nil {
			tail.ContextUsed = *sessionContext.LastPromptTokens
		}
	}
	return tail, nil
}

// snapshotToolCall is one tool call of a snapshot. Its arguments and result
// are left undecoded: only the calls at the tail are looked into, and a long
// session's snapshot holds megabytes of them.
type snapshotToolCall struct {
	Name    string          `json:"name"`
	Args    json.RawMessage `json:"args"`
	Result  json.RawMessage `json:"result"`
	Display struct {
		Summary string `json:"summary"`
	} `json:"display"`
}

// conversationTail reads the newest limit entries out of a snapshot. A
// message's tool calls follow its text, matched to the snapshot's list of
// calls in order, as the web UI's native panel matches them.
func conversationTail(snapshot *chimera.SessionSnapshot, limit int) *ConversationTail {
	tail := &ConversationTail{Status: snapshot.Status}

	var messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	_ = json.Unmarshal(snapshot.Messages, &messages)
	var calls []snapshotToolCall
	_ = json.Unmarshal(snapshot.ToolCalls, &calls)

	// A tool call is kept as its place in the snapshot's list. It is described
	// only if it is among the entries kept, which is known once all are.
	type item struct {
		entry ConversationEntry
		call  int
	}
	var items []item
	said := func(kind ConversationEntryKind, text string) {
		if text = strings.TrimSpace(text); text != "" {
			items = append(items, item{entry: ConversationEntry{Kind: kind, Text: cutText(text)}})
		}
	}
	nextCall := 0
	for index, message := range messages {
		// The first message of a session is its own instructions.
		if index == 0 && message.Role == "user" {
			continue
		}
		kind := EntryAssistant
		if message.Role == "user" {
			kind = EntryUser
		}
		var text string
		if json.Unmarshal(message.Content, &text) == nil {
			said(kind, text)
			continue
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		_ = json.Unmarshal(message.Content, &parts)
		var words strings.Builder
		toolCalls := 0
		for _, part := range parts {
			switch part.Type {
			case "text":
				words.WriteString(part.Text)
			case "tool-call":
				toolCalls++
			}
		}
		said(kind, words.String())
		for ; toolCalls > 0 && nextCall < len(calls); toolCalls, nextCall = toolCalls-1, nextCall+1 {
			items = append(items, item{entry: ConversationEntry{Kind: EntryTool}, call: nextCall})
		}
	}

	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	tail.Entries = make([]ConversationEntry, len(items))
	for i, kept := range items {
		tail.Entries[i] = kept.entry
		if kept.entry.Kind == EntryTool {
			tail.Entries[i] = describeToolCall(calls[kept.call])
		}
	}

	if snapshot.PendingPermissions != nil && len(*snapshot.PendingPermissions) > 0 {
		pending := (*snapshot.PendingPermissions)[0]
		tail.Permission = &ConversationPermission{
			Tool:    pending.Tool,
			Command: pending.Command,
			Reason:  pending.Reason,
			Plan:    cutText(pending.Plan),
		}
	}
	var questions []struct {
		Questions []struct {
			Question string `json:"question"`
			Options  []struct {
				Label string `json:"label"`
			} `json:"options"`
		} `json:"questions"`
	}
	_ = json.Unmarshal(snapshot.PendingQuestions, &questions)
	if len(questions) > 0 && len(questions[0].Questions) > 0 {
		first := questions[0].Questions[0]
		question := &ConversationQuestion{Text: first.Question}
		for _, option := range first.Options {
			question.Options = append(question.Options, option.Label)
		}
		tail.Question = question
	}
	return tail
}

// describeToolCall says in one line what a tool call is for, and how it
// stands: running while it has no result, failed when its result says so.
func describeToolCall(call snapshotToolCall) ConversationEntry {
	var args struct {
		Command     string `json:"command"`
		Description string `json:"description"`
		Path        string `json:"path"`
	}
	_ = json.Unmarshal(call.Args, &args)

	entry := ConversationEntry{Kind: EntryTool, Tool: call.Name, Command: firstLineOf(args.Command), State: ToolDone}
	switch {
	case args.Path != "" && (call.Name == "read" || call.Name == "edit" || call.Name == "write"):
		entry.Text = path.Base(args.Path)
	case args.Description != "":
		// What the agent said the call is for reads better at a glance
		// than the call itself.
		entry.Text = firstLineOf(args.Description)
	case call.Display.Summary != "":
		entry.Text = firstLineOf(call.Display.Summary)
	default:
		entry.Text = entry.Command
	}

	if len(call.Result) == 0 || string(call.Result) == "null" {
		entry.State = ToolRunning
		return entry
	}
	var result struct {
		ExitCode *int   `json:"exit_code"`
		Error    string `json:"error"`
	}
	_ = json.Unmarshal(call.Result, &result)
	if result.Error != "" || (result.ExitCode != nil && *result.ExitCode != 0) {
		entry.State = ToolFailed
	}
	return entry
}

func firstLineOf(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return cutText(strings.TrimSpace(line))
}

// cutText cuts a text to conversationTextLimit characters.
func cutText(text string) string {
	if runes := []rune(text); len(runes) > conversationTextLimit {
		return string(runes[:conversationTextLimit-1]) + "…"
	}
	return text
}
