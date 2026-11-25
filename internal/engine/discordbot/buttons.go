package discordbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Buttons: a message posted with buttons carries one action row of them,
// each button's custom_id its value (at most 100 characters, unique in the
// message). A click arrives over the Gateway as INTERACTION_CREATE (type 3,
// a message component), which needs no intent and must be answered within
// 3 seconds: Serve sends a deferred update (callback type 6) on a goroutine
// of its own before it hands the click on. The interaction's token is a
// credential for 15 minutes and never appears in an error.

// Button is a button on a message.
type Button struct {
	Label, Value string
	// Style is "primary", "danger", or "" for the default (secondary).
	Style string
}

// Interaction is a click on a button of a message the bot posted.
type Interaction struct {
	ID, Token, ApplicationID string
	ChannelID, GuildID       string // GuildID is "" in a DM
	InThread                 bool   // ChannelID is a thread
	UserID                   string // who clicked
	// MessageID and Content are the message with the buttons; Buttons are
	// its buttons, for Settle.
	MessageID, Content string
	Value, Label       string
	Buttons            []Button
}

// deferredTimeout bounds the deferred response, inside Discord's 3 seconds.
const deferredTimeout = 2500 * time.Millisecond

const (
	componentActionRow = 1
	componentButton    = 2
	styleSecondary     = 2
)

var styles = map[string]int{"primary": 1, "": styleSecondary, "secondary": styleSecondary, "success": 3, "danger": 4}

type buttonComponent struct {
	Type     int    `json:"type"`
	Style    int    `json:"style"`
	Label    string `json:"label"`
	CustomID string `json:"custom_id"`
	Disabled bool   `json:"disabled,omitempty"`
}

type actionRow struct {
	Type       int               `json:"type"`
	Components []buttonComponent `json:"components"`
}

func components(buttons []Button, disabled bool) []actionRow {
	if len(buttons) == 0 {
		return nil
	}
	row := actionRow{Type: componentActionRow}
	for _, b := range buttons {
		style, ok := styles[b.Style]
		if !ok {
			style = styleSecondary
		}
		row.Components = append(row.Components, buttonComponent{
			Type: componentButton, Style: style, Label: b.Label, CustomID: b.Value, Disabled: disabled,
		})
	}
	return []actionRow{row}
}

func styleName(style int) string {
	for name, n := range styles {
		if n == style && name != "secondary" {
			return name
		}
	}
	return ""
}

type rawInteraction struct {
	ID            string   `json:"id"`
	ApplicationID string   `json:"application_id"`
	Type          int      `json:"type"`
	Token         string   `json:"token"`
	ChannelID     string   `json:"channel_id"`
	GuildID       string   `json:"guild_id"`
	Channel       *channel `json:"channel"`
	Member        *struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	} `json:"member"`
	User *struct {
		ID string `json:"id"`
	} `json:"user"`
	Message struct {
		ID         string `json:"id"`
		Content    string `json:"content"`
		Components []struct {
			Type       int `json:"type"`
			Components []struct {
				Type     int    `json:"type"`
				Style    int    `json:"style"`
				Label    string `json:"label"`
				CustomID string `json:"custom_id"`
			} `json:"components"`
		} `json:"components"`
	} `json:"message"`
	Data struct {
		CustomID      string `json:"custom_id"`
		ComponentType int    `json:"component_type"`
	} `json:"data"`
}

// parseInteraction reads INTERACTION_CREATE; ok is false for anything but
// a click on a button.
func (c *Client) parseInteraction(raw json.RawMessage) (Interaction, bool) {
	var d rawInteraction
	if json.Unmarshal(raw, &d) != nil || d.Type != 3 || d.Data.ComponentType != componentButton || d.ID == "" || d.Token == "" {
		return Interaction{}, false
	}
	i := Interaction{
		ID: d.ID, Token: d.Token, ApplicationID: d.ApplicationID,
		ChannelID: d.ChannelID, GuildID: d.GuildID,
		MessageID: d.Message.ID, Content: d.Message.Content,
		Value: d.Data.CustomID,
	}
	switch {
	case d.Member != nil && d.Member.User.ID != "":
		i.UserID = d.Member.User.ID
	case d.User != nil:
		i.UserID = d.User.ID
	}
	if d.Channel != nil && d.Channel.Type != 0 {
		i.InThread = isThreadType(d.Channel.Type)
	} else {
		c.mu.Lock()
		i.InThread = c.threads[d.ChannelID]
		c.mu.Unlock()
	}
	for _, row := range d.Message.Components {
		for _, b := range row.Components {
			if b.Type != componentButton {
				continue
			}
			i.Buttons = append(i.Buttons, Button{Label: b.Label, Value: b.CustomID, Style: styleName(b.Style)})
			if b.CustomID == i.Value && i.Label == "" {
				i.Label = b.Label
			}
		}
	}
	return i, true
}

// RespondDeferredUpdate answers an interaction with a deferred update
// (callback type 6): Discord stops waiting, and the message stays as it is.
func (c *Client) RespondDeferredUpdate(ctx context.Context, id, token string) error {
	endpoint := c.apiBase() + "/interactions/" + url.PathEscape(id) + "/" + url.PathEscape(token) + "/callback"
	err := c.do(ctx, http.MethodPost, endpoint, []byte(`{"type":6}`), nil)
	return withoutSecret(err, token)
}

// Settle edits the clicked message: the line under its content, as
// subtext, and every button disabled. The content is cut to fit.
func (c *Client) Settle(ctx context.Context, i Interaction, line string) error {
	if i.ChannelID == "" || i.MessageID == "" {
		return errors.New("discord: no message to settle")
	}
	suffix := "\n-# " + line
	content := []rune(i.Content)
	room := MaxContentLength - len([]rune(suffix))
	if room < 1 {
		return ErrTooLong
	}
	if len(content) > room {
		content = append(content[:room-1], '…')
	}
	body := struct {
		Content         string      `json:"content"`
		Components      []actionRow `json:"components"`
		AllowedMentions struct {
			Parse []string `json:"parse"`
		} `json:"allowed_mentions"`
	}{Content: string(content) + suffix, Components: components(i.Buttons, true)}
	if body.Components == nil {
		body.Components = []actionRow{}
	}
	body.AllowedMentions.Parse = []string{}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := c.apiBase() + "/channels/" + url.PathEscape(i.ChannelID) + "/messages/" + url.PathEscape(i.MessageID)
	return c.do(ctx, http.MethodPatch, endpoint, payload, nil)
}

// answer sends the deferred response for a click, bounded by
// deferredTimeout, and reports a failure to onError.
func (c *Client) answer(ctx context.Context, i Interaction, onError func(error)) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deferredTimeout)
	defer cancel()
	if err := c.RespondDeferredUpdate(ctx, i.ID, i.Token); err != nil && onError != nil {
		onError(err)
	}
}

// withoutSecret is err with secret taken out of its text.
func withoutSecret(err error, secret string) error {
	if err == nil || secret == "" || !strings.Contains(err.Error(), secret) {
		return err
	}
	return fmt.Errorf("discord: %s", strings.ReplaceAll(strings.TrimPrefix(err.Error(), "discord: "), secret, "[redacted]"))
}
