package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
)

// ChatThreadStepType reads the thread a chat message is in (LEG-635), so a
// run a mention started can see what the mention was about.
const ChatThreadStepType = "chat.thread.read"

// MaxChatThreadTranscript is the most bytes of transcript a step gives.
// The oldest replies go first to fit it, never the parent.
const MaxChatThreadTranscript = 16 << 10

// Bounds of chat.thread.read's limit: how many messages it keeps, the
// parent included.
const (
	defaultChatThreadLimit = 50
	maxChatThreadLimit     = 200
)

// ChatThreadReadExecutor is chat.thread.read.
type ChatThreadReadExecutor struct {
	bots ChatThreadReader
}

// NewChatThreadReadExecutor reads through bots; nil fails every step, as in
// a process with no chat bots.
func NewChatThreadReadExecutor(bots ChatThreadReader) *ChatThreadReadExecutor {
	return &ChatThreadReadExecutor{bots: bots}
}

func (e *ChatThreadReadExecutor) Type() string { return ChatThreadStepType }

func (e *ChatThreadReadExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func (e *ChatThreadReadExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	bot, err := workflowString(step.With, "bot", true)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	threadID, err := workflowString(step.With, "thread_id", true)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	channel, err := workflowString(step.With, "channel", false)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	limit := defaultChatThreadLimit
	if value, ok := step.With["limit"]; ok && value != nil {
		if limit, err = workflowInt(value); err != nil {
			return WorkflowExecutionResult{}, fmt.Errorf("limit %w", err)
		}
	}
	if limit < 1 || limit > maxChatThreadLimit {
		return WorkflowExecutionResult{}, fmt.Errorf("limit must be from 1 to %d, not %d", maxChatThreadLimit, limit)
	}
	includeBots, err := workflowBool(step.With, "include_bots")
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if e.bots == nil {
		return WorkflowExecutionResult{}, fmt.Errorf("bot %s: chat bots are not available in this process", bot)
	}
	thread, err := e.bots.ReadThread(ctx, bot, channel, threadID, limit, includeBots)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	transcript, kept := chatTranscript(thread, MaxChatThreadTranscript)
	messages := make([]map[string]any, 0, len(kept))
	for _, m := range kept {
		at := ""
		if !m.At.IsZero() {
			at = m.At.UTC().Format(time.RFC3339Nano)
		}
		messages = append(messages, map[string]any{
			"id": m.ID, "author_id": m.AuthorID, "author_name": m.AuthorName, "at": at,
			"text": keptChatText(m.Text, m.ForwardedText), "forwarded": m.Forwarded, "forwarded_url": m.ForwardedURL,
			"parent": m.Parent, "from_bot": m.FromBot,
		})
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{
		"transcript": transcript, "messages": messages, "count": len(kept),
	}}, nil
}

// chatTranscript is a thread as plain text, oldest first: a block per
// message, its author and time on a line, then its text (with what it
// forwards quoted, cut as a trigger's event.text is). When the blocks are
// over max bytes the oldest replies go until they fit, and a line after the
// parent says how many replies are left out, the limit's included. It
// returns the messages the transcript holds.
func chatTranscript(thread ChatThread, max int) (string, []ChatThreadMessage) {
	msgs := thread.Messages
	if len(msgs) == 0 {
		return "", nil
	}
	blocks := make([]string, len(msgs))
	for i, m := range msgs {
		blocks[i] = chatTranscriptBlock(m)
	}
	leftOut := thread.LeftOut
	first := 1 // the oldest reply still in
	size := func() int {
		n := len(blocks[0])
		if leftOut > 0 {
			n += len("\n\n") + len(leftOutLine(leftOut))
		}
		for _, b := range blocks[first:] {
			n += len("\n\n") + len(b)
		}
		return n
	}
	for first < len(blocks) && size() > max {
		first++
		leftOut++
	}
	parts := []string{blocks[0]}
	if leftOut > 0 {
		parts = append(parts, leftOutLine(leftOut))
	}
	parts = append(parts, blocks[first:]...)
	kept := append([]ChatThreadMessage{msgs[0]}, msgs[first:]...)
	return cutUTF8(strings.Join(parts, "\n\n"), max), kept
}

func chatTranscriptBlock(m ChatThreadMessage) string {
	author := m.AuthorName
	switch {
	case author != "":
	case m.AuthorID != "":
		author = "<@" + m.AuthorID + ">"
	default:
		author = "someone"
	}
	header := author
	if !m.At.IsZero() {
		header += " · " + m.At.UTC().Format("2006-01-02 15:04 UTC")
	}
	if text := keptChatText(m.Text, m.ForwardedText); text != "" {
		return header + "\n" + text
	}
	return header
}

func leftOutLine(n int) string {
	if n == 1 {
		return "[… 1 earlier reply left out]"
	}
	return fmt.Sprintf("[… %d earlier replies left out]", n)
}
