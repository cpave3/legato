package service

import (
	"context"
	"errors"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// conversationTextTail is how many entries a capture reads to find the
// newest assistant text.
const conversationTextTail = 40

// ConversationTextOptions configures ConversationTextCapture.
type ConversationTextOptions struct {
	// Delay is how long after a turn ends the conversation is read, and so
	// the most often one agent's is read: 5 seconds when zero.
	Delay time.Duration
	// Owner says whether this process is the one that captures; nil is
	// always. Every Legato hears every agent's turns.
	Owner func() bool
}

// ConversationTextCapture keeps each conversation's first operator message
// and newest assistant message in the session history, read from its
// agent's native endpoint when a turn ends.
type ConversationTextCapture struct {
	store  *store.Store
	reader AgentConversationReader
	opts   ConversationTextOptions

	mu      sync.Mutex
	pending map[string]bool
}

// NewConversationTextCapture captures through reader into db.
func NewConversationTextCapture(db *store.Store, reader AgentConversationReader, opts ConversationTextOptions) *ConversationTextCapture {
	if opts.Delay <= 0 {
		opts.Delay = 5 * time.Second
	}
	return &ConversationTextCapture{store: db, reader: reader, opts: opts, pending: map[string]bool{}}
}

// TurnEnded reads the task's conversation after Delay. Turns that end
// meanwhile are covered by that one read.
func (c *ConversationTextCapture) TurnEnded(taskID string) {
	c.mu.Lock()
	if c.pending[taskID] {
		c.mu.Unlock()
		return
	}
	c.pending[taskID] = true
	c.mu.Unlock()
	time.AfterFunc(c.opts.Delay, func() {
		c.mu.Lock()
		delete(c.pending, taskID)
		c.mu.Unlock()
		if c.opts.Owner != nil && !c.opts.Owner() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.Capture(ctx, taskID); err != nil {
			log.Printf("session history: reading the conversation of %s: %v", taskID, err)
		}
	})
}

// Capture reads the task's main-slot conversation now and records its
// text. An agent with no native endpoint has none to read.
func (c *ConversationTextCapture) Capture(ctx context.Context, taskID string) error {
	session, err := c.store.GetAgentSessionByTaskID(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	if session.ExecutionID == "" {
		return nil
	}
	tail, err := c.reader.ConversationTail(ctx, taskID, conversationTextTail)
	if errors.Is(err, ErrNoNativeEndpoint) {
		return nil
	}
	if err != nil {
		return err
	}
	var last string
	for i := len(tail.Entries) - 1; i >= 0; i-- {
		if tail.Entries[i].Kind == EntryAssistant {
			last = tail.Entries[i].Text
			break
		}
	}
	return c.store.RecordConversationText(ctx, session.ExecutionID, cleanConversationText(tail.FirstUser), cleanConversationText(last), time.Now())
}

// StartEventLoop captures on each turn end the bus reports: an agent's
// activity becoming anything but working. It returns the stop.
func (c *ConversationTextCapture) StartEventLoop(ctx context.Context, bus *events.Bus) func() {
	ctx, cancel := context.WithCancel(ctx)
	ch := bus.Subscribe(events.EventAgentActivityChanged)
	go func() {
		defer bus.Unsubscribe(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-ch:
				if !ok {
					return
				}
				payload, _ := event.Payload.(events.AgentActivityChangedPayload)
				if payload.TaskID != "" && payload.Activity != store.ActivityWorking {
					c.TurnEnded(payload.TaskID)
				}
			}
		}
	}()
	return cancel
}

var ansiEscape = regexp.MustCompile(`\x1b(\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[@-Z\\-_])`)

// cleanConversationText is a message as the history keeps it: escape
// sequences and control characters out, cut to conversationTextLimit.
func cleanConversationText(text string) string {
	text = ansiEscape.ReplaceAllString(text, "")
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	text = strings.TrimSpace(text)
	if runes := []rune(text); len(runes) > conversationTextLimit {
		text = string(runes[:conversationTextLimit])
	}
	return text
}
