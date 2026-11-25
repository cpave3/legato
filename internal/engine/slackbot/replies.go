package slackbot

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
)

// Reading a thread: conversations.replies (with the channels:history,
// groups:history, im:history or mpim:history scope of the channel's kind)
// answers the thread's parent first, then its replies oldest first, a page
// at a time; a ts that starts no thread answers that message alone.
// users.info needs users:read, which a bot may not have: it then answers
// missing_scope.

// repliesPage is how many messages one conversations.replies call asks
// for; it keeps a page far below maxBody.
const repliesPage = 100

// ThreadMessage is one message of a thread, as conversations.replies gives it.
type ThreadMessage struct {
	User     string // "" for an integration's post
	BotID    string // set when posted by a bot
	Username string // a bot's name, when Slack gives one
	Text     string
	TS       string
	ThreadTS string
	Subtype  string
	// Shares are the messages this one forwards or shares, as on Event.
	Shares []Share
}

// Replies reads the thread whose parent is ts in channel: the parent, then
// at most max-1 replies, oldest first. Channel events (joins, topic changes)
// among the replies are left out; the parent never is. A rate limit is
// waited out once per page, as PostMessage does.
func (c *Client) Replies(ctx context.Context, channel, ts string, max int) ([]ThreadMessage, error) {
	if channel == "" || ts == "" {
		return nil, errors.New("slack: conversations.replies needs a channel and a ts")
	}
	var out []ThreadMessage
	cursor := ""
	for len(out) < max {
		form := url.Values{"channel": {channel}, "ts": {ts}, "limit": {strconv.Itoa(repliesPage)}}
		if cursor != "" {
			form.Set("cursor", cursor)
		}
		var page struct {
			Messages []struct {
				Type        string                 `json:"type"`
				Subtype     string                 `json:"subtype"`
				User        string                 `json:"user"`
				BotID       string                 `json:"bot_id"`
				Username    string                 `json:"username"`
				BotProfile  *struct{ Name string } `json:"bot_profile"`
				Text        string                 `json:"text"`
				TS          string                 `json:"ts"`
				ThreadTS    string                 `json:"thread_ts"`
				Attachments []json.RawMessage      `json:"attachments"`
			} `json:"messages"`
			HasMore  bool `json:"has_more"`
			Metadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := c.callRetrying(ctx, "conversations.replies", form, &page); err != nil {
			return nil, err
		}
		for _, m := range page.Messages {
			if len(out) == max {
				break
			}
			if len(out) > 0 && !isPost(m.Subtype) {
				continue
			}
			name := m.Username
			if name == "" && m.BotProfile != nil {
				name = m.BotProfile.Name
			}
			out = append(out, ThreadMessage{User: m.User, BotID: m.BotID, Username: name, Text: m.Text, TS: m.TS,
				ThreadTS: m.ThreadTS, Subtype: m.Subtype, Shares: shares(m.Attachments)})
		}
		cursor = page.Metadata.NextCursor
		if !page.HasMore || cursor == "" || len(page.Messages) == 0 {
			break
		}
	}
	return out, nil
}

// UserName is the name Slack shows for a user: their display name, else
// their full name, else their username. Without users:read it fails with an
// *APIError of Code "missing_scope".
func (c *Client) UserName(ctx context.Context, userID string) (string, error) {
	var out struct {
		User struct {
			Name     string `json:"name"`
			RealName string `json:"real_name"`
			Profile  struct {
				DisplayName string `json:"display_name"`
				RealName    string `json:"real_name"`
			} `json:"profile"`
		} `json:"user"`
	}
	if err := c.callRetrying(ctx, "users.info", url.Values{"user": {userID}}, &out); err != nil {
		return "", err
	}
	u := out.User
	for _, name := range []string{u.Profile.DisplayName, u.RealName, u.Profile.RealName, u.Name} {
		if name != "" {
			return name, nil
		}
	}
	return "", nil
}
