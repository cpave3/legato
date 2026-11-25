package slackbot

import (
	"encoding/json"
	"strings"
)

// Share is a message forwarded or shared into another: Slack sends it as an
// attachment marked is_share or is_msg_unfurl, and the outer message's text
// is only the note the sender added (often none).
type Share struct {
	AuthorID, AuthorName   string
	ChannelID, ChannelName string // ChannelName may be empty
	FromURL                string // the shared message's permalink
	TS                     string
	// Text is the shared message's text in Slack mrkdwn: the attachment's
	// text, else its rich text rendered plainly, else its fallback.
	Text string
}

type rawAttachment struct {
	IsShare       bool   `json:"is_share"`
	IsMsgUnfurl   bool   `json:"is_msg_unfurl"`
	AuthorID      string `json:"author_id"`
	AuthorName    string `json:"author_name"`
	ChannelID     string `json:"channel_id"`
	ChannelName   string `json:"channel_name"`
	FromURL       string `json:"from_url"`
	TS            string `json:"ts"`
	Text          string `json:"text"`
	Fallback      string `json:"fallback"`
	MessageBlocks []struct {
		Message struct {
			Blocks []richBlock `json:"blocks"`
		} `json:"message"`
	} `json:"message_blocks"`
}

// richBlock is a block or a rich text element: the fields of each kind
// that a plain rendering reads.
type richBlock struct {
	Type      string          `json:"type"`
	Text      json.RawMessage `json:"text"` // a string in rich text, an object in a section block
	URL       string          `json:"url"`
	UserID    string          `json:"user_id"`
	ChannelID string          `json:"channel_id"`
	Name      string          `json:"name"`
	Elements  []richBlock     `json:"elements"`
}

// shares are the attachments that share a message.
func shares(attachments []rawAttachment) []Share {
	var out []Share
	for _, a := range attachments {
		if !a.IsShare && !a.IsMsgUnfurl {
			continue
		}
		text := a.Text
		if text == "" {
			var lines []string
			for _, mb := range a.MessageBlocks {
				for _, b := range mb.Message.Blocks {
					if b.Type == "rich_text" {
						lines = append(lines, renderElements(b.Elements)...)
					}
				}
			}
			text = strings.Join(lines, "\n")
		}
		if text == "" {
			text = a.Fallback
		}
		out = append(out, Share{AuthorID: a.AuthorID, AuthorName: a.AuthorName, ChannelID: a.ChannelID, ChannelName: a.ChannelName,
			FromURL: a.FromURL, TS: a.TS, Text: text})
	}
	return out
}

// renderElements is a rich_text block's elements as lines: a section,
// preformatted text and a quote each a line of its own, a list one "- "
// line per item. Unknown elements are left out.
func renderElements(elements []richBlock) []string {
	var lines []string
	for _, e := range elements {
		switch e.Type {
		case "rich_text_section", "rich_text_preformatted", "rich_text_quote":
			lines = append(lines, renderInline(e.Elements))
		case "rich_text_list":
			for _, item := range e.Elements {
				lines = append(lines, "- "+renderInline(item.Elements))
			}
		}
	}
	return lines
}

// renderInline is a section's text, users and channels written as Slack
// writes them in a message's text.
func renderInline(elements []richBlock) string {
	var b strings.Builder
	for _, e := range elements {
		switch e.Type {
		case "text":
			b.WriteString(e.text())
		case "link":
			if t := e.text(); t != "" {
				b.WriteString(t)
			} else {
				b.WriteString(e.URL)
			}
		case "user":
			b.WriteString("<@" + e.UserID + ">")
		case "channel":
			b.WriteString("<#" + e.ChannelID + ">")
		case "emoji":
			b.WriteString(":" + e.Name + ":")
		}
	}
	return b.String()
}

func (e richBlock) text() string {
	var s string
	if json.Unmarshal(e.Text, &s) != nil {
		return ""
	}
	return s
}
