package slackbot

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Buttons: a message posted with buttons carries Block Kit blocks, its text
// in section blocks (each at most sectionLength characters) then one
// actions block (block_id "buttons") whose buttons are button_0, button_1,
// … A click arrives as an "interactive" envelope of type block_actions,
// acknowledged like every envelope before it is handled. Block Kit buttons
// cannot be disabled, so Settle replaces them with a context line.

// Button is a button on a message.
type Button struct {
	Label, Value string
	// Style is "primary", "danger", or "" for the default.
	Style string
}

// Click is a click on a button of a message the bot posted.
type Click struct {
	Channel, User string
	// TS is the message's ts; ThreadTS its thread's, "" when in none.
	TS, ThreadTS string
	ActionID     string
	Value, Label string
	// InteractionID is unique to the click: its trigger_id, else its
	// action_ts. Slack redelivers an envelope with the same payload.
	InteractionID string
	// Text and Blocks are the message as it was, for Settle.
	Text   string
	Blocks json.RawMessage
}

const (
	buttonsBlockID = "buttons"
	sectionLength  = 3000
	maxBlocks      = 50
)

type textObject struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

type buttonElement struct {
	Type     string     `json:"type"`
	ActionID string     `json:"action_id"`
	Text     textObject `json:"text"`
	Value    string     `json:"value"`
	Style    string     `json:"style,omitempty"`
}

type block struct {
	Type     string      `json:"type"`
	BlockID  string      `json:"block_id,omitempty"`
	Text     *textObject `json:"text,omitempty"`
	Elements any         `json:"elements,omitempty"`
}

// messageBlocks is text as sections followed by the buttons.
func messageBlocks(text string, buttons []Button) ([]block, error) {
	var blocks []block
	for _, part := range sections(text) {
		blocks = append(blocks, block{Type: "section", Text: &textObject{Type: "mrkdwn", Text: part}})
	}
	if len(blocks) >= maxBlocks {
		return nil, ErrTooLong
	}
	elements := make([]buttonElement, len(buttons))
	for i, b := range buttons {
		elements[i] = buttonElement{
			Type:     "button",
			ActionID: "button_" + strconv.Itoa(i),
			Text:     textObject{Type: "plain_text", Text: b.Label, Emoji: true},
			Value:    b.Value,
			Style:    b.Style,
		}
	}
	return append(blocks, block{Type: "actions", BlockID: buttonsBlockID, Elements: elements}), nil
}

// sections cuts text into parts of at most sectionLength runes, at line
// ends where it can and inside a line only when one is longer than that.
func sections(text string) []string {
	var parts []string
	var cur strings.Builder
	curLen := 0
	flush := func() {
		if curLen > 0 {
			parts = append(parts, cur.String())
			cur.Reset()
			curLen = 0
		}
	}
	for _, line := range strings.SplitAfter(text, "\n") {
		n := utf8.RuneCountInString(line)
		if curLen+n > sectionLength {
			flush()
		}
		for n > sectionLength {
			cut := runeOffset(line, sectionLength)
			parts = append(parts, line[:cut])
			line = line[cut:]
			n -= sectionLength
		}
		cur.WriteString(line)
		curLen += n
	}
	flush()
	return parts
}

// runeOffset is the byte offset of the n-th rune of s.
func runeOffset(s string, n int) int {
	i := 0
	for off := range s {
		if i == n {
			return off
		}
		i++
	}
	return len(s)
}

type rawClick struct {
	Type      string `json:"type"`
	TriggerID string `json:"trigger_id"`
	User      struct {
		ID string `json:"id"`
	} `json:"user"`
	Channel struct {
		ID string `json:"id"`
	} `json:"channel"`
	Container struct {
		MessageTS string `json:"message_ts"`
		ChannelID string `json:"channel_id"`
	} `json:"container"`
	Message struct {
		TS       string          `json:"ts"`
		ThreadTS string          `json:"thread_ts"`
		Text     string          `json:"text"`
		Blocks   json.RawMessage `json:"blocks"`
	} `json:"message"`
	Actions []struct {
		Type     string `json:"type"`
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
		ActionTS string `json:"action_ts"`
		Text     struct {
			Text string `json:"text"`
		} `json:"text"`
	} `json:"actions"`
}

// parseClick reads an interactive payload; ok is false for anything but a
// click on a button of a message.
func parseClick(payload json.RawMessage) (Click, bool) {
	var p rawClick
	if err := json.Unmarshal(payload, &p); err != nil || p.Type != "block_actions" || len(p.Actions) == 0 {
		return Click{}, false
	}
	a := p.Actions[0]
	if a.Type != "button" {
		return Click{}, false
	}
	c := Click{
		Channel:       p.Channel.ID,
		User:          p.User.ID,
		TS:            p.Container.MessageTS,
		ThreadTS:      p.Message.ThreadTS,
		ActionID:      a.ActionID,
		Value:         a.Value,
		Label:         a.Text.Text,
		InteractionID: p.TriggerID,
		Text:          p.Message.Text,
		Blocks:        p.Message.Blocks,
	}
	if c.Channel == "" {
		c.Channel = p.Container.ChannelID
	}
	if c.TS == "" {
		c.TS = p.Message.TS
	}
	if c.InteractionID == "" {
		c.InteractionID = a.ActionTS
	}
	if c.Channel == "" || c.TS == "" || c.InteractionID == "" {
		return Click{}, false
	}
	return c, true
}

// Settle rewrites the clicked message with chat.update: its buttons give
// way to line, in a context block, and the rest stays as it was.
func (c *Client) Settle(ctx context.Context, click Click, line string) error {
	var blocks []json.RawMessage
	if len(click.Blocks) > 0 {
		if err := json.Unmarshal(click.Blocks, &blocks); err != nil {
			return errors.New("slack: chat.update: the message's blocks are not a list")
		}
	}
	settled, _ := json.Marshal(block{Type: "context", Elements: []textObject{{Type: "mrkdwn", Text: line}}})
	out := make([]json.RawMessage, 0, len(blocks)+1)
	replaced := false
	for _, b := range blocks {
		var head struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(b, &head)
		if head.Type == "actions" {
			if !replaced {
				out = append(out, settled)
				replaced = true
			}
			continue
		}
		out = append(out, b)
	}
	if !replaced {
		out = append(out, settled)
	}
	body := struct {
		Channel string            `json:"channel"`
		TS      string            `json:"ts"`
		Text    string            `json:"text"`
		Blocks  []json.RawMessage `json:"blocks"`
	}{click.Channel, click.TS, click.Text, out}
	return c.callRetrying(ctx, "chat.update", body, nil)
}
