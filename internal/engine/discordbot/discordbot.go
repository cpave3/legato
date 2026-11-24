// Package discordbot is a thin Discord bot client: the Gateway websocket (API
// v10, JSON encoding) for receiving messages, and the one REST call Legato
// makes, POST /channels/{id}/messages.
//
// It follows Discord's gateway rules:
//
//   - The gateway address comes from GET /gateway/bot unless given, with
//     ?v=10&encoding=json added.
//   - After HELLO (op 10) the first heartbeat (op 1, the last sequence number or
//     null) is sent after heartbeat_interval × a random fraction, then every
//     interval. A heartbeat that falls due before the previous one was
//     acknowledged (op 11) means a zombie connection: it is closed with a code
//     other than 1000/1001, so the session stays resumable. A heartbeat the
//     server asks for (op 1) is sent at once.
//   - A new session IDENTIFYs (op 2); a known one RESUMEs (op 6) at READY's
//     resume_gateway_url with its session_id and last sequence number.
//   - RECONNECT (op 7) and INVALID_SESSION (op 9, d true) resume; INVALID_SESSION
//     with d false, close codes 4007 and 4009, and our own close with 1000 (on
//     cancellation) end the session, so the next run identifies.
//     Close codes 4004, 4010–4014 are not retryable and map to ErrAuth,
//     ErrConfig and ErrIntents.
//
// Run runs one connection and returns; the caller loops, backing off unless
// the error is ErrReconnect. The bot token never appears in an error.
package discordbot

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	// ErrAuth is a refused token: gateway close 4004 or REST 401.
	ErrAuth = errors.New("discord: authentication failed")
	// ErrIntents is gateway close 4013 or 4014.
	ErrIntents = errors.New("discord: gateway intents refused: enable the privileged Message Content intent for the bot in the Discord Developer Portal (Bot > Privileged Gateway Intents)")
	// ErrConfig is gateway close 4010, 4011 or 4012.
	ErrConfig = errors.New("discord: gateway configuration refused")
	// ErrReconnect asks the caller to call Run again at once.
	ErrReconnect = errors.New("discord: reconnect")
	// ErrTooLong is content over MaxContentLength characters.
	ErrTooLong = errors.New("discord: message too long")
)

// MaxContentLength is the most characters a message's content may have.
const MaxContentLength = 2000

// Gateway intents.
const (
	IntentGuilds         = 1 << 0
	IntentGuildMessages  = 1 << 9
	IntentDirectMessages = 1 << 12
	IntentMessageContent = 1 << 15
)

// DefaultIntents are the intents a Client asks for when Intents is 0.
const DefaultIntents = IntentGuilds | IntentGuildMessages | IntentDirectMessages | IntentMessageContent

// IntentByName maps configuration names to intent bits.
var IntentByName = map[string]int{
	"guilds":          IntentGuilds,
	"guild_messages":  IntentGuildMessages,
	"direct_messages": IntentDirectMessages,
	"message_content": IntentMessageContent,
}

// IntentsFromNames ORs the named intents (case ignored). No names give 0,
// which a Client reads as DefaultIntents.
func IntentsFromNames(names []string) (int, error) {
	bits := 0
	for _, n := range names {
		b, ok := IntentByName[strings.ToLower(strings.TrimSpace(n))]
		if !ok {
			return 0, fmt.Errorf("discord: unknown intent %q", n)
		}
		bits |= b
	}
	return bits, nil
}

// APIError is a REST call Discord refused.
type APIError struct {
	Status     int
	Code       int
	Message    string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	s := fmt.Sprintf("discord: HTTP %d", e.Status)
	if e.Message != "" {
		s += ": " + e.Message
	}
	if e.Code != 0 {
		s += fmt.Sprintf(" (code %d)", e.Code)
	}
	if e.RetryAfter > 0 {
		s += fmt.Sprintf(" (retry after %s)", e.RetryAfter)
	}
	return s
}

// Message is a MESSAGE_CREATE.
type Message struct {
	ID, ChannelID, GuildID string // GuildID is "" for a DM
	AuthorID               string
	AuthorIsBot            bool
	Content                string
	Mentions               []string // user IDs mentioned
	ReferenceID            string   // the message this one replies to
	InThread               bool     // ChannelID is a thread
	Timestamp              time.Time
}

// Client is a Discord bot. Its session state is kept across Run calls, so a
// Client must not run two sessions at once.
type Client struct {
	Token      string
	Intents    int          // 0 = DefaultIntents
	HTTP       *http.Client // nil = a client with a 15s timeout
	APIBase    string       // "" = https://discord.com/api/v10
	GatewayURL string       // "" = ask GET {APIBase}/gateway/bot

	// Jitter gives the fraction of the heartbeat interval [0,1) before the
	// first heartbeat; nil = random.
	Jitter func() float64
	// InvalidSessionWait is how long to wait after a non-resumable
	// INVALID_SESSION; nil = random 1–5s.
	InvalidSessionWait func() time.Duration

	mu        sync.Mutex
	sessionID string
	resumeURL string
	seq       int64
	hasSeq    bool
	selfID    string
	threads   map[string]bool
}

const defaultAPIBase = "https://discord.com/api/v10"

func (c *Client) apiBase() string {
	if c.APIBase != "" {
		return strings.TrimRight(c.APIBase, "/")
	}
	return defaultAPIBase
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (c *Client) intents() int {
	if c.Intents != 0 {
		return c.Intents
	}
	return DefaultIntents
}

// SelfID is the bot's own user ID, known once a session is READY.
func (c *Client) SelfID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.selfID
}
