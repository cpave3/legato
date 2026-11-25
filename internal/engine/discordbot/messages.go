package discordbot

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// rawMessage is a message object as the Gateway's MESSAGE_CREATE and the
// REST API both write it.
type rawMessage struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	GuildID   string `json:"guild_id"`
	Type      int    `json:"type"`
	Author    struct {
		ID         string `json:"id"`
		Username   string `json:"username"`
		GlobalName string `json:"global_name"`
		Bot        bool   `json:"bot"`
	} `json:"author"`
	Content  string `json:"content"`
	Mentions []struct {
		ID string `json:"id"`
	} `json:"mentions"`
	MessageReference *struct {
		// Type is 0 for a reply, 1 for a forward.
		Type      int    `json:"type"`
		MessageID string `json:"message_id"`
		ChannelID string `json:"channel_id"`
		GuildID   string `json:"guild_id"`
	} `json:"message_reference"`
	MessageSnapshots []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"message_snapshots"`
	Timestamp string `json:"timestamp"`
	// Position is set only on a message in a thread.
	Position *int `json:"position"`
}

func (d rawMessage) message() Message {
	m := Message{
		ID: d.ID, ChannelID: d.ChannelID, GuildID: d.GuildID, Type: d.Type,
		AuthorID: d.Author.ID, AuthorName: d.Author.GlobalName, AuthorIsBot: d.Author.Bot,
		Content: d.Content,
	}
	if m.AuthorName == "" {
		m.AuthorName = d.Author.Username
	}
	for _, u := range d.Mentions {
		m.Mentions = append(m.Mentions, u.ID)
	}
	switch ref := d.MessageReference; {
	case ref == nil:
	case ref.Type == 1:
		for _, snapshot := range d.MessageSnapshots {
			m.Forwards = append(m.Forwards, Forward{Content: snapshot.Message.Content,
				ChannelID: ref.ChannelID, GuildID: ref.GuildID, MessageID: ref.MessageID})
		}
	default:
		m.ReferenceID = ref.MessageID
	}
	if t, err := time.Parse(time.RFC3339Nano, d.Timestamp); err == nil {
		m.Timestamp = t
	}
	return m
}

// Channel is a channel's kind and place: a thread's ParentID is the
// channel it was started in.
type Channel struct {
	ID       string
	Type     int
	ParentID string
	GuildID  string
}

// GetChannel reads a channel. It needs View Channel.
func (c *Client) GetChannel(ctx context.Context, channelID string) (Channel, error) {
	if channelID == "" {
		return Channel{}, errors.New("discord: no channel")
	}
	var out struct {
		ID       string `json:"id"`
		Type     int    `json:"type"`
		ParentID string `json:"parent_id"`
		GuildID  string `json:"guild_id"`
	}
	if err := c.doRetrying(ctx, http.MethodGet, c.apiBase()+"/channels/"+url.PathEscape(channelID), nil, &out); err != nil {
		return Channel{}, err
	}
	return Channel{ID: out.ID, Type: out.Type, ParentID: out.ParentID, GuildID: out.GuildID}, nil
}

// GetMessage reads one message; one that is gone is ErrNotFound. It needs
// Read Message History, and the Message Content intent for its content.
func (c *Client) GetMessage(ctx context.Context, channelID, messageID string) (Message, error) {
	if channelID == "" || messageID == "" {
		return Message{}, errors.New("discord: a message needs a channel and an ID")
	}
	var out rawMessage
	endpoint := c.apiBase() + "/channels/" + url.PathEscape(channelID) + "/messages/" + url.PathEscape(messageID)
	if err := c.doRetrying(ctx, http.MethodGet, endpoint, nil, &out); err != nil {
		return Message{}, err
	}
	m := out.message()
	if m.ChannelID == "" {
		m.ChannelID = channelID
	}
	return m, nil
}

// ListMessages reads up to limit (at most 100) of a channel's messages,
// newest first: the newest of all when before is "", else those before
// that message.
func (c *Client) ListMessages(ctx context.Context, channelID, before string, limit int) ([]Message, error) {
	if channelID == "" {
		return nil, errors.New("discord: no channel")
	}
	q := url.Values{"limit": {strconv.Itoa(min(max(limit, 1), 100))}}
	if before != "" {
		q.Set("before", before)
	}
	var out []rawMessage
	endpoint := c.apiBase() + "/channels/" + url.PathEscape(channelID) + "/messages?" + q.Encode()
	if err := c.doRetrying(ctx, http.MethodGet, endpoint, nil, &out); err != nil {
		return nil, err
	}
	msgs := make([]Message, 0, len(out))
	for _, d := range out {
		m := d.message()
		if m.ChannelID == "" {
			m.ChannelID = channelID
		}
		msgs = append(msgs, m)
	}
	return msgs, nil
}
