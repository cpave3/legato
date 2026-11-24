package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Chat bots (LEG-624): a Slack bot over Socket Mode and a Discord bot over
// the Gateway, each holding one outbound connection, so Legato needs no
// public address. Workflows start on their messages (chat.message,
// chat.mention) and post as them (chat.message.send).

// ChatEvent is a message a chat bot received, the same for every platform.
type ChatEvent struct {
	Bot, Platform string
	Channel, User string
	Text          string
	MessageID     string
	// ThreadID is what Send's threadID takes to reply to this message: on
	// Slack the thread's ts (the message's own when it starts none), on
	// Discord the thread channel's ID when the message is in a thread, else
	// the message's ID (a reply).
	ThreadID string
	At       time.Time
	// Mention is a message that mentions the bot, or is sent to it
	// directly.
	Mention bool
	// FromBot is a message another bot posted. The bot's own messages
	// never become events.
	FromBot bool
}

// SentMessage is a message a bot sent.
type SentMessage struct {
	MessageID, Channel string
	// ThreadID is what Send's threadID takes to reply in the same thread.
	ThreadID string
}

// ChatConnection is one bot on one platform. Run holds one session: it
// connects, calls connected once it is, hands each message to emit, and
// returns when the session ends (ctx.Err() when ctx ends). Its errors wrap
// ErrChatReconnect when the platform asked for a new session at once, and
// ErrChatAuth when trying again cannot help. Send posts a message over the
// platform's HTTP API and needs no session.
type ChatConnection interface {
	Name() string
	Platform() string
	Run(ctx context.Context, emit func(ChatEvent), connected func()) error
	Send(ctx context.Context, channel, text, threadID string) (SentMessage, error)
}

var (
	// ErrChatReconnect is a session's end that wants a new one at once.
	ErrChatReconnect = errors.New("the platform asked for a new connection")
	// ErrChatAuth is a refusal of the bot's tokens or settings.
	ErrChatAuth = errors.New("the platform refused the bot")
	// ErrChatBotUnknown names a bot no Legato setting describes.
	ErrChatBotUnknown = errors.New("no such chat bot")
	// ErrChatBotNotConnected is a bot that cannot send: switched off, or
	// refused by its platform.
	ErrChatBotNotConnected = errors.New("chat bot is not connected")
)

// The states of a chat bot's connection.
const (
	ChatBotDisabled   = "disabled"
	ChatBotConnecting = "connecting"
	ChatBotConnected  = "connected"
	ChatBotBackingOff = "backing_off"
	ChatBotAuthFailed = "auth_failed"
	// ChatBotElsewhere is a bot another Legato process on the same data
	// directory keeps connected.
	ChatBotElsewhere = "elsewhere"
)

// ChatBotSpec is one bot as the settings describe it.
type ChatBotSpec struct {
	Name, Platform string
	// Configured is a bot whose tokens are set; Enabled one switched on.
	Configured, Enabled bool
	DefaultChannel      string
	Conn                ChatConnection
	// Problem, when set, is why the bot cannot connect, found before trying
	// (a missing token, one of the wrong kind): the bot is auth_failed.
	Problem string
	// Secrets are the bot's tokens, scrubbed from any error it shows.
	Secrets []string
}

// ChatBotStatus is what Settings shows of a bot. It never holds a token.
type ChatBotStatus struct {
	Name           string     `json:"name"`
	Platform       string     `json:"platform"`
	Enabled        bool       `json:"enabled"`
	Configured     bool       `json:"configured"`
	DefaultChannel string     `json:"default_channel,omitempty"`
	State          string     `json:"state"`
	LastError      string     `json:"last_error,omitempty"`
	Since          time.Time  `json:"since"`
	NextAttempt    *time.Time `json:"next_attempt,omitempty"`
}

// ChatSender sends as a bot, for chat.message.send.
type ChatSender interface {
	Send(ctx context.Context, bot, channel, text, threadID string) (SentMessage, error)
}

// ChatBotDirectory says whether a bot of a name is set up, for workflow
// checks.
type ChatBotDirectory interface {
	Known(name string) (configured, enabled bool)
}

// ChatBots is the supervisor: one connection loop per enabled bot, with
// backoff, and the status Settings shows. Only one process on a data
// directory runs the loops (the others are SetElsewhere), since two Slack
// Socket Mode connections split the events between them and two Discord
// sessions both receive every one.
type ChatBots struct {
	// BaseDelay and MaxDelay bound the backoff; StableAfter is how long a
	// session must last for the next failure to start the backoff over.
	BaseDelay, MaxDelay, StableAfter time.Duration
	// SendTimeout bounds one Send; StopWait how long Stop waits.
	SendTimeout, StopWait time.Duration

	onEvent func(context.Context, ChatEvent)
	rand    func() float64
	after   func(time.Duration) <-chan time.Time
	clock   func() time.Time

	mu        sync.Mutex
	bots      map[string]*chatBot
	order     []string
	elsewhere bool
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	onChange  []func()
	stopped   bool
}

type chatBot struct {
	spec      ChatBotSpec
	status    ChatBotStatus
	reconnect chan struct{}
}

// chatEventQueue is how many messages may wait for the trigger
// coordinator before a bot stops reading more.
const chatEventQueue = 256

// NewChatBots makes the supervisor; onEvent receives each message, one at
// a time per bot.
func NewChatBots(specs []ChatBotSpec, onEvent func(context.Context, ChatEvent)) *ChatBots {
	b := &ChatBots{
		BaseDelay: time.Second, MaxDelay: 3 * time.Minute, StableAfter: time.Minute,
		SendTimeout: 15 * time.Second, StopWait: 5 * time.Second,
		onEvent: onEvent, rand: rand.Float64, after: time.After, clock: time.Now,
		bots: map[string]*chatBot{},
	}
	for _, spec := range specs {
		state := ChatBotDisabled
		lastError := ""
		if spec.Enabled && spec.Problem != "" {
			state, lastError = ChatBotAuthFailed, spec.Problem
		} else if spec.Enabled {
			state = ChatBotConnecting
		}
		b.bots[spec.Name] = &chatBot{spec: spec, reconnect: make(chan struct{}, 1), status: ChatBotStatus{
			Name: spec.Name, Platform: spec.Platform, Enabled: spec.Enabled, Configured: spec.Configured,
			DefaultChannel: spec.DefaultChannel, State: state, LastError: lastError, Since: time.Now(),
		}}
		b.order = append(b.order, spec.Name)
	}
	sort.Strings(b.order)
	return b
}

// SetElsewhere marks the bots as connected by another process: Start runs
// no loops, and Send still works.
func (b *ChatBots) SetElsewhere() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.elsewhere = true
	for _, bot := range b.bots {
		if bot.spec.Enabled && bot.status.State == ChatBotConnecting {
			bot.status.State = ChatBotElsewhere
		}
	}
}

// SetEventHandler sets what receives each message; call it before Start.
func (b *ChatBots) SetEventHandler(fn func(context.Context, ChatEvent)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onEvent = fn
}

// OnChange adds a function called after a bot's status changes.
func (b *ChatBots) OnChange(fn func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onChange = append(b.onChange, fn)
}

// Start starts a loop for each enabled bot that has no problem.
func (b *ChatBots) Start(ctx context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cancel != nil || b.elsewhere || b.stopped {
		return
	}
	ctx, b.cancel = context.WithCancel(ctx)
	for _, name := range b.order {
		bot := b.bots[name]
		if !bot.spec.Enabled || bot.spec.Conn == nil {
			continue
		}
		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			b.loop(ctx, bot)
		}()
	}
}

// Stop ends every loop, waiting at most StopWait.
func (b *ChatBots) Stop() {
	b.mu.Lock()
	cancel := b.cancel
	b.stopped = true
	b.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(b.StopWait):
	}
}

// Reconnect has a bot that failed try again at once, as after its tokens
// were fixed.
func (b *ChatBots) Reconnect(name string) error {
	b.mu.Lock()
	bot, ok := b.bots[name]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrChatBotUnknown, name)
	}
	if !bot.spec.Enabled {
		return fmt.Errorf("%w: %s is switched off", ErrChatBotNotConnected, name)
	}
	select {
	case bot.reconnect <- struct{}{}:
	default:
	}
	return nil
}

// Statuses is every bot's status, by name.
func (b *ChatBots) Statuses() []ChatBotStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]ChatBotStatus, 0, len(b.order))
	for _, name := range b.order {
		out = append(out, b.bots[name].status)
	}
	return out
}

// Known says whether a bot of the name has tokens set, and is on.
func (b *ChatBots) Known(name string) (configured, enabled bool) {
	if b == nil {
		return false, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	bot, ok := b.bots[name]
	if !ok {
		return false, false
	}
	return bot.spec.Configured, bot.spec.Enabled
}

// Send posts as a bot: to its default channel when channel is empty. It
// fails at once for a bot that is unknown, off or refused, and gives up
// after SendTimeout; a bot still connecting can send, as sending needs no
// session.
func (b *ChatBots) Send(ctx context.Context, name, channel, text, threadID string) (SentMessage, error) {
	if b == nil {
		return SentMessage{}, fmt.Errorf("%w: chat bots are not available in this process", ErrChatBotNotConnected)
	}
	b.mu.Lock()
	bot, ok := b.bots[name]
	var status ChatBotStatus
	if ok {
		status = bot.status
	}
	b.mu.Unlock()
	switch {
	case !ok:
		return SentMessage{}, fmt.Errorf("%w named %q: set up Slack or Discord in Settings › Integrations", ErrChatBotUnknown, name)
	case !bot.spec.Enabled:
		return SentMessage{}, fmt.Errorf("bot %s: %w: it is switched off in Settings › Integrations", name, ErrChatBotNotConnected)
	case status.State == ChatBotAuthFailed:
		return SentMessage{}, fmt.Errorf("bot %s: %w: auth failed: %s; check its tokens in Settings › Integrations", name, ErrChatBotNotConnected, status.LastError)
	}
	if channel = strings.TrimSpace(channel); channel == "" {
		channel = bot.spec.DefaultChannel
	}
	if channel == "" {
		return SentMessage{}, fmt.Errorf("bot %s: no channel given, and the bot has no default channel", name)
	}
	if strings.TrimSpace(text) == "" {
		return SentMessage{}, fmt.Errorf("bot %s: the message is empty", name)
	}
	ctx, cancel := context.WithTimeout(ctx, b.SendTimeout)
	defer cancel()
	sent, err := bot.spec.Conn.Send(ctx, channel, text, strings.TrimSpace(threadID))
	if err != nil {
		return SentMessage{}, fmt.Errorf("bot %s: sending to %s: %s", name, channel, scrubChatError(err.Error(), bot.spec.Secrets))
	}
	return sent, nil
}

func (b *ChatBots) setStatus(bot *chatBot, state, lastError string, next *time.Time) {
	b.mu.Lock()
	changed := bot.status.State != state || bot.status.LastError != lastError
	if bot.status.State != state {
		bot.status.Since = b.clock()
	}
	bot.status.State, bot.status.NextAttempt = state, next
	if lastError != "" || state == ChatBotConnected {
		bot.status.LastError = lastError
	}
	listeners := append([]func(){}, b.onChange...)
	b.mu.Unlock()
	if changed {
		for _, fn := range listeners {
			fn()
		}
	}
}

// loop keeps one bot connected until ctx ends.
func (b *ChatBots) loop(ctx context.Context, bot *chatBot) {
	if bot.spec.Problem != "" {
		b.setStatus(bot, ChatBotAuthFailed, bot.spec.Problem, nil)
		select {
		case <-ctx.Done():
		case <-bot.reconnect:
			// Tokens are read at start: a problem with them stays until
			// Legato restarts.
			b.setStatus(bot, ChatBotAuthFailed, bot.spec.Problem+" (restart Legato after fixing it)", nil)
		}
		return
	}
	events := make(chan ChatEvent, chatEventQueue)
	var deliver sync.WaitGroup
	deliver.Add(1)
	go func() {
		defer deliver.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-events:
				if b.onEvent != nil {
					b.onEvent(ctx, e)
				}
			}
		}
	}()
	defer deliver.Wait()
	attempt := 0
	for ctx.Err() == nil {
		b.setStatus(bot, ChatBotConnecting, "", nil)
		started := b.clock()
		connectedAt := time.Time{}
		var connectedOnce sync.Once
		err := bot.spec.Conn.Run(ctx,
			func(e ChatEvent) {
				select {
				case events <- e:
				case <-ctx.Done():
				}
			},
			func() {
				connectedOnce.Do(func() {
					connectedAt = b.clock()
					attempt = 0
					b.setStatus(bot, ChatBotConnected, "", nil)
				})
			})
		if ctx.Err() != nil {
			return
		}
		message := "connection ended"
		if err != nil {
			message = scrubChatError(err.Error(), bot.spec.Secrets)
		}
		switch {
		case errors.Is(err, ErrChatAuth):
			b.setStatus(bot, ChatBotAuthFailed, message, nil)
			select {
			case <-ctx.Done():
				return
			case <-bot.reconnect:
				attempt = 0
				continue
			}
		case errors.Is(err, ErrChatReconnect) && !connectedAt.IsZero():
			continue
		}
		if !connectedAt.IsZero() && b.clock().Sub(started) >= b.StableAfter {
			attempt = 0
		}
		delay := b.backoff(attempt)
		attempt++
		next := b.clock().Add(delay)
		b.setStatus(bot, ChatBotBackingOff, message, &next)
		select {
		case <-ctx.Done():
			return
		case <-b.after(delay):
		case <-bot.reconnect:
		}
	}
}

// backoff is the wait before a retry: full jitter under an exponential
// ceiling.
func (b *ChatBots) backoff(attempt int) time.Duration {
	ceiling := b.BaseDelay
	for i := 0; i < attempt && ceiling < b.MaxDelay; i++ {
		ceiling *= 2
	}
	if ceiling > b.MaxDelay {
		ceiling = b.MaxDelay
	}
	return time.Duration(b.rand() * float64(ceiling))
}

var chatURLQuery = regexp.MustCompile(`((?:wss?|https?)://[^\s?"']*)\?[^\s"']*`)

// scrubChatError takes the bot's tokens, and the query of any address (a
// Slack socket address carries a ticket), out of an error's text.
func scrubChatError(text string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[redacted]")
		}
	}
	return chatURLQuery.ReplaceAllString(text, "$1")
}
