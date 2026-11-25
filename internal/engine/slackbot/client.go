// Package slackbot is a thin client for a Slack bot: one Socket Mode session
// at a time, and the Web API methods auth.test, apps.connections.open,
// chat.postMessage, reactions.add, reactions.remove, and conversations.replies
// and users.info for reading a thread. It knows nothing of Legato.
//
// Protocol facts it relies on:
//
//   - Web API methods are POST https://slack.com/api/<method> with
//     "Authorization: Bearer <token>" and answer {"ok": bool, "error": code}.
//     A rate limit is HTTP 429 (or error "ratelimited") with Retry-After in
//     seconds. A missing scope is error "missing_scope" with "needed" naming
//     the scope.
//   - reactions.add answers "already_reacted" for a reaction the bot has
//     already left, and reactions.remove "no_reaction" for one it has not;
//     both are treated as success.
//   - apps.connections.open takes the app-level token (xapp-) and returns a
//     single-use wss:// URL whose query string carries a ticket. That URL is a
//     secret, like the tokens, and never appears in an error.
//   - On the socket Slack first sends {"type":"hello"}. Every envelope with an
//     envelope_id must be acknowledged with {"envelope_id": id} within a few
//     seconds or Slack redelivers it, so the ack goes out before the event is
//     handled. Event subscriptions arrive as "events_api" envelopes whose
//     payload.event is the event.
//   - {"type":"disconnect"} asks the client to reconnect (refresh_requested,
//     warning, too_many_websockets, ...); reason "link_disabled" means the
//     app's Socket Mode was turned off, which is treated as an auth failure.
package slackbot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"nhooyr.io/websocket"
)

var (
	// ErrAuth is an authentication failure; retrying will not help.
	ErrAuth = errors.New("slack: authentication failed")
	// ErrReconnect is returned by Run when Slack asks for a new connection.
	ErrReconnect = errors.New("slack: reconnect requested")
	// ErrTooLong is returned by PostMessage for text over MaxTextLength.
	ErrTooLong = errors.New("slack: message too long")
)

// MaxTextLength is the most characters chat.postMessage accepts in text.
const MaxTextLength = 40000

const (
	defaultAPIBase = "https://slack.com/api"
	readLimit      = 1 << 20
	maxRetryWait   = 5 * time.Second
	maxBody        = 1 << 20
)

var authCodes = map[string]bool{
	"invalid_auth":           true,
	"not_authed":             true,
	"account_inactive":       true,
	"token_revoked":          true,
	"token_expired":          true,
	"link_disabled":          true,
	"not_allowed_token_type": true,
}

// APIError is a Web API ok:false answer, or an HTTP failure (Code "http_<status>").
type APIError struct {
	Method     string
	Code       string
	RetryAfter time.Duration
	// Needed and Provided are the scopes Slack names for a missing_scope.
	Needed, Provided string
}

func (e *APIError) Error() string {
	s := "slack: " + e.Method + ": " + e.Code
	if e.Needed != "" {
		s += " (needs " + e.Needed + ")"
	}
	if e.RetryAfter > 0 {
		s += " (retry after " + e.RetryAfter.String() + ")"
	}
	return s
}

// Is makes errors.Is(err, ErrAuth) true for authentication codes.
func (e *APIError) Is(target error) bool {
	return target == ErrAuth && authCodes[e.Code]
}

// Client talks to Slack. Its zero value needs at least the tokens.
type Client struct {
	AppToken string       // xapp-…, for apps.connections.open
	BotToken string       // xoxb-…, for auth.test, chat.postMessage and reactions
	HTTP     *http.Client // nil = a default client with a 15s timeout
	APIBase  string       // "" = https://slack.com/api
}

// Identity is who the bot token belongs to.
type Identity struct{ UserID, BotID, TeamID string }

// Event is a message or a mention delivered over Socket Mode.
type Event struct {
	Type        string // "message" or "mention"
	Channel     string
	ChannelType string // "channel", "group", "im", "mpim"; may be empty
	User        string
	BotID       string // set when posted by a bot
	Text        string
	TS          string
	ThreadTS    string // "" when not in a thread
	EventTS     string
	Subtype     string
	// Shares are the messages this one forwards or shares, in order; Text
	// is then only the sender's own note.
	Shares []Share
}

// Posted is where chat.postMessage put a message.
type Posted struct{ TS, Channel string }

var defaultHTTP = &http.Client{Timeout: 15 * time.Second}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return defaultHTTP
}

func (c *Client) base() string {
	if c.APIBase != "" {
		return strings.TrimRight(c.APIBase, "/")
	}
	return defaultAPIBase
}

// call POSTs body (a form when jsonBody is url.Values, as Slack's read
// methods take their arguments; JSON when it is anything else; else an empty
// form) and decodes the answer into out after checking ok.
func (c *Client) call(ctx context.Context, method, token string, jsonBody any, out any) error {
	var body io.Reader = strings.NewReader("")
	contentType := "application/x-www-form-urlencoded"
	if form, ok := jsonBody.(url.Values); ok {
		body = strings.NewReader(form.Encode())
	} else if jsonBody != nil {
		b, err := json.Marshal(jsonBody)
		if err != nil {
			return fmt.Errorf("slack: %s: encode: %w", method, err)
		}
		body = bytes.NewReader(b)
		contentType = "application/json; charset=utf-8"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/"+method, body)
	if err != nil {
		return fmt.Errorf("slack: %s: %w", method, err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("slack: %s: request failed: %s", method, scrub(err.Error(), token))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("slack: %s: read: %w", method, err)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return &APIError{Method: method, Code: "ratelimited", RetryAfter: retryAfter(resp)}
	}
	if resp.StatusCode/100 != 2 {
		return &APIError{Method: method, Code: "http_" + strconv.Itoa(resp.StatusCode)}
	}
	var status struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
		Needed   string `json:"needed"`
		Provided string `json:"provided"`
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return fmt.Errorf("slack: %s: decode response: %w", method, err)
	}
	if !status.OK {
		code := status.Error
		if code == "" {
			code = "unknown_error"
		}
		e := &APIError{Method: method, Code: code, Needed: status.Needed, Provided: status.Provided}
		if code == "ratelimited" {
			e.RetryAfter = retryAfter(resp)
		}
		return e
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("slack: %s: decode response: %w", method, err)
		}
	}
	return nil
}

func retryAfter(resp *http.Response) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After")))
	if err != nil || n < 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// AuthTest asks auth.test who the bot token is.
func (c *Client) AuthTest(ctx context.Context) (Identity, error) {
	var out struct {
		UserID string `json:"user_id"`
		BotID  string `json:"bot_id"`
		TeamID string `json:"team_id"`
	}
	if err := c.call(ctx, "auth.test", c.BotToken, nil, &out); err != nil {
		return Identity{}, err
	}
	return Identity{UserID: out.UserID, BotID: out.BotID, TeamID: out.TeamID}, nil
}

// Open asks apps.connections.open for a Socket Mode URL. The URL is a secret.
func (c *Client) Open(ctx context.Context) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	if err := c.call(ctx, "apps.connections.open", c.AppToken, nil, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", errors.New("slack: apps.connections.open: no url in response")
	}
	return out.URL, nil
}

// PostMessage posts text to channel, in the thread threadTS when set. A rate
// limit with a Retry-After of at most 5 seconds is waited out and retried once.
func (c *Client) PostMessage(ctx context.Context, channel, text, threadTS string) (Posted, error) {
	if utf8.RuneCountInString(text) > MaxTextLength {
		return Posted{}, ErrTooLong
	}
	body := struct {
		Channel  string `json:"channel"`
		Text     string `json:"text"`
		ThreadTS string `json:"thread_ts,omitempty"`
	}{channel, text, threadTS}
	var out struct {
		Channel string `json:"channel"`
		TS      string `json:"ts"`
	}
	err := c.callRetrying(ctx, "chat.postMessage", body, &out)
	if err != nil {
		return Posted{}, err
	}
	return Posted{TS: out.TS, Channel: out.Channel}, nil
}

// callRetrying calls a bot-token method, waiting out a rate limit with a
// Retry-After of at most maxRetryWait and retrying once.
func (c *Client) callRetrying(ctx context.Context, method string, body, out any) error {
	err := c.call(ctx, method, c.BotToken, body, out)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Code == "ratelimited" && apiErr.RetryAfter <= maxRetryWait {
		t := time.NewTimer(apiErr.RetryAfter)
		select {
		case <-ctx.Done():
			t.Stop()
			return err
		case <-t.C:
		}
		err = c.call(ctx, method, c.BotToken, body, out)
	}
	return err
}

// AddReaction adds the emoji name (without colons) to the message ts in
// channel. A reaction the bot has already left is success.
func (c *Client) AddReaction(ctx context.Context, channel, ts, name string) error {
	return c.react(ctx, "reactions.add", "already_reacted", channel, ts, name)
}

// RemoveReaction removes the bot's emoji name from the message ts in channel.
// A reaction the bot has not left is success.
func (c *Client) RemoveReaction(ctx context.Context, channel, ts, name string) error {
	return c.react(ctx, "reactions.remove", "no_reaction", channel, ts, name)
}

func (c *Client) react(ctx context.Context, method, unchanged, channel, ts, name string) error {
	body := struct {
		Channel   string `json:"channel"`
		Timestamp string `json:"timestamp"`
		Name      string `json:"name"`
	}{channel, ts, name}
	err := c.callRetrying(ctx, method, body, nil)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Code == unchanged {
		return nil
	}
	return err
}

type envelope struct {
	Type       string          `json:"type"`
	EnvelopeID string          `json:"envelope_id"`
	Reason     string          `json:"reason"`
	Payload    json.RawMessage `json:"payload"`
}

type rawEvent struct {
	Type        string            `json:"type"`
	Subtype     string            `json:"subtype"`
	Channel     string            `json:"channel"`
	ChannelType string            `json:"channel_type"`
	User        string            `json:"user"`
	BotID       string            `json:"bot_id"`
	Text        string            `json:"text"`
	TS          string            `json:"ts"`
	ThreadTS    string            `json:"thread_ts"`
	EventTS     string            `json:"event_ts"`
	Attachments []json.RawMessage `json:"attachments"`
}

// Run opens one Socket Mode session: Open, dial, wait for hello (then call
// onConnected if non-nil), then read envelopes, acknowledging each before
// handing its event to onEvent, until ctx ends or the session drops.
//
// It returns ctx.Err() on cancellation (closing the socket normally),
// ErrReconnect when Slack asks for a new connection, ErrAuth for
// authentication failures and other errors otherwise.
func (c *Client) Run(ctx context.Context, onEvent func(Event), onConnected func()) error {
	wsURL, err := c.Open(ctx)
	if err != nil {
		return err
	}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: c.httpClient()})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("slack: socket dial %s: %s", redactURL(wsURL), scrubURL(err.Error(), wsURL))
	}
	defer conn.CloseNow()
	conn.SetReadLimit(readLimit)

	// Close normally when ctx ends; that also unblocks a pending Read.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "")
		case <-stop:
		}
	}()
	readCtx := context.WithoutCancel(ctx)

	for {
		_, data, err := conn.Read(readCtx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return fmt.Errorf("slack: socket read: %s", scrubURL(err.Error(), wsURL))
		}
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue // not something we understand; Slack does not expect a reply
		}
		if env.EnvelopeID != "" {
			ack, _ := json.Marshal(map[string]string{"envelope_id": env.EnvelopeID})
			if err := conn.Write(ctx, websocket.MessageText, ack); err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				return fmt.Errorf("slack: socket ack: %s", scrubURL(err.Error(), wsURL))
			}
		}
		switch env.Type {
		case "hello":
			if onConnected != nil {
				onConnected()
			}
		case "disconnect":
			_ = conn.Close(websocket.StatusNormalClosure, "")
			if env.Reason == "link_disabled" {
				return fmt.Errorf("%w: socket mode disabled for the app (link_disabled)", ErrAuth)
			}
			return fmt.Errorf("%w (%s)", ErrReconnect, env.Reason)
		case "events_api":
			if ev, ok := parseEvent(env.Payload); ok && onEvent != nil {
				onEvent(ev)
			}
		}
	}
}

func parseEvent(payload json.RawMessage) (Event, bool) {
	var p struct {
		Event rawEvent `json:"event"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return Event{}, false
	}
	e := p.Event
	var typ string
	switch e.Type {
	case "message":
		if !isPost(e.Subtype) {
			return Event{}, false
		}
		typ = "message"
	case "app_mention":
		typ = "mention"
	default:
		return Event{}, false
	}
	return Event{
		Type:        typ,
		Channel:     e.Channel,
		ChannelType: e.ChannelType,
		User:        e.User,
		BotID:       e.BotID,
		Text:        e.Text,
		TS:          e.TS,
		ThreadTS:    e.ThreadTS,
		EventTS:     e.EventTS,
		Subtype:     e.Subtype,
		Shares:      shares(e.Attachments),
	}, true
}

// isPost is a message subtype someone wrote, rather than a channel event
// (a join, a topic). bot_message is a post by an integration (an incoming
// webhook); the caller decides about bots' messages.
func isPost(subtype string) bool {
	switch subtype {
	case "", "thread_broadcast", "file_share", "bot_message":
		return true
	}
	return false
}

// redactURL is scheme://host/path of a URL, without its query (the ticket).
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<socket url>"
	}
	return u.Scheme + "://" + u.Host + u.Path
}

// scrubURL removes the URL's query string from s, in every form the
// websocket library might print it (it rewrites ws to http).
func scrubURL(s, raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return scrub(s, raw)
	}
	if u.RawQuery != "" {
		s = strings.ReplaceAll(s, "?"+u.RawQuery, "")
		s = strings.ReplaceAll(s, u.RawQuery, "[redacted]")
	}
	return scrub(s, raw)
}

func scrub(s string, secrets ...string) string {
	for _, sec := range secrets {
		if sec != "" {
			s = strings.ReplaceAll(s, sec, "[redacted]")
		}
	}
	return s
}
