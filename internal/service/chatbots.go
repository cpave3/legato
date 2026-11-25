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
	"unicode/utf8"
)

// Chat bots (LEG-624): a Slack bot over Socket Mode and a Discord bot over
// the Gateway, each holding one outbound connection, so Legato needs no
// public address. Workflows start on their messages (chat.message,
// chat.mention), post as them (chat.message.send), react as them
// (chat.reaction.add, chat.reaction.remove) and read the thread a message
// is in (chat.thread.read).

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
	// Forwarded is a message that forwards or shares others. Text is then
	// only the sender's own words, and ForwardedText the forwarded
	// messages quoted, each under a "> Forwarded from …:" line; triggers
	// see the two together. ForwardedURL is the first forwarded message's
	// link that is known. Whether a message mentions the bot is decided
	// from Text alone, never from what it forwards.
	Forwarded     bool
	ForwardedURL  string
	ForwardedText string
	// Click is set for a click on a button the bot posted, which is no
	// message: MessageID is then the message with the buttons, ThreadID
	// what replies to it, User who clicked, and Text empty.
	Click *ChatClick
}

// ChatButton is a button under a message a bot sends.
type ChatButton struct {
	Label, Value string
	// Style is "primary", "danger", or "" for the platform's default.
	Style string
}

// The limits a button keeps on every platform (Slack's label, Discord's
// custom_id, which is the value).
const (
	MaxChatButtons     = 5
	MaxChatButtonLabel = 75
	MaxChatButtonValue = 100
)

// ChatClick is a click on a button: its value and label, and the
// interaction's ID, unique to the click.
type ChatClick struct {
	Value, Label, InteractionID string
	// MessageText is the text of the message with the buttons, as the
	// platform gave it with the click.
	MessageText string
	// settle rewrites the message to say the click was taken, its buttons
	// gone or disabled. It holds the platform's handle on the click (a
	// Discord interaction token), so it is never stored.
	settle func(ctx context.Context, line string) error
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
// ErrChatAuth when trying again cannot help. Send posts a message, and
// React adds the bot's reaction to one (or removes it), over the platform's
// HTTP API; neither needs a session. A reaction already there, or one already
// gone, is success. ReadThread reads the thread a threadID names (as
// ChatEvent.ThreadID gives it), at most max messages: the parent first, then
// the rest oldest first; a message in no thread is a thread of one.
type ChatConnection interface {
	Name() string
	Platform() string
	Run(ctx context.Context, emit func(ChatEvent), connected func()) error
	Send(ctx context.Context, channel, text, threadID string, buttons ...ChatButton) (SentMessage, error)
	React(ctx context.Context, channel, messageID, emoji string, remove bool) error
	ReadThread(ctx context.Context, channel, threadID string, max int) ([]ChatThreadMessage, error)
}

// ChatThreadMessage is one message of a thread a bot read.
type ChatThreadMessage struct {
	ID string
	// AuthorName is "" when the platform would not say (a Slack bot
	// without users:read).
	AuthorID, AuthorName string
	At                   time.Time
	// Text is the author's own words; forwarded messages are in
	// ForwardedText, quoted as on a ChatEvent.
	Text string
	// Parent is the message that starts the thread: a Slack thread's
	// parent, a Discord thread's starter, the root of a reply chain.
	Parent, FromBot bool
	Forwarded       bool
	ForwardedURL    string
	ForwardedText   string
}

// withForwards sets m's forward fields as ChatEvent.withForwards does.
func (m ChatThreadMessage) withForwards(forwards []chatForward) ChatThreadMessage {
	e := ChatEvent{}.withForwards(forwards)
	m.Forwarded, m.ForwardedURL, m.ForwardedText = e.Forwarded, e.ForwardedURL, e.ForwardedText
	return m
}

// ChatThreadReadMax is the most messages one thread read reads: on Slack
// the parent and the first 999 replies, on Discord the newest 1000.
const ChatThreadReadMax = 1000

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
	Send(ctx context.Context, bot, channel, text, threadID string, buttons ...ChatButton) (SentMessage, error)
}

// ChatReactor reacts as a bot, for chat.reaction.add and chat.reaction.remove.
type ChatReactor interface {
	React(ctx context.Context, bot, channel, messageID, emoji string, remove bool) error
}

// ChatThreadReader reads a thread as a bot, for chat.thread.read.
type ChatThreadReader interface {
	ReadThread(ctx context.Context, bot, channel, threadID string, limit int, includeBots bool) (ChatThread, error)
}

// ChatThread is what ChatBots.ReadThread kept of a thread: the parent first.
// LeftOut is how many other messages the limit left out (bots' messages
// left out by choice are not counted).
type ChatThread struct {
	Messages []ChatThreadMessage
	LeftOut  int
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
	// SendTimeout bounds one Send; ReadTimeout one ReadThread, which may
	// take several pages and a name for each author; StopWait how long
	// Stop waits.
	SendTimeout, ReadTimeout, StopWait time.Duration

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
		SendTimeout: 15 * time.Second, ReadTimeout: 30 * time.Second, StopWait: 5 * time.Second,
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

// Send posts as a bot: to its default channel when channel is empty, with
// buttons under it when there are any. It fails at once for a bot that is
// unknown, off or refused, or buttons no platform takes, and gives up
// after SendTimeout; a bot still connecting can send, as sending needs no
// session.
func (b *ChatBots) Send(ctx context.Context, name, channel, text, threadID string, buttons ...ChatButton) (SentMessage, error) {
	bot, channel, err := b.ready(name, channel)
	if err != nil {
		return SentMessage{}, err
	}
	if strings.TrimSpace(text) == "" {
		return SentMessage{}, fmt.Errorf("bot %s: the message is empty", name)
	}
	if err := checkChatButtons(buttons); err != nil {
		return SentMessage{}, fmt.Errorf("bot %s: %w", name, err)
	}
	ctx, cancel := context.WithTimeout(ctx, b.SendTimeout)
	defer cancel()
	sent, err := bot.spec.Conn.Send(ctx, channel, text, strings.TrimSpace(threadID), buttons...)
	if err != nil {
		return SentMessage{}, fmt.Errorf("bot %s: sending to %s: %s", name, channel, scrubChatError(err.Error(), bot.spec.Secrets))
	}
	return sent, nil
}

// checkChatButtons checks buttons against the limits every platform keeps.
func checkChatButtons(buttons []ChatButton) error {
	if len(buttons) > MaxChatButtons {
		return fmt.Errorf("a message takes at most %d buttons, not %d", MaxChatButtons, len(buttons))
	}
	values := map[string]bool{}
	for i, button := range buttons {
		at := fmt.Sprintf("button %d", i+1)
		switch {
		case strings.TrimSpace(button.Label) == "":
			return fmt.Errorf("%s: no label", at)
		case utf8.RuneCountInString(button.Label) > MaxChatButtonLabel:
			return fmt.Errorf("%s: the label is longer than %d characters", at, MaxChatButtonLabel)
		case strings.TrimSpace(button.Value) == "":
			return fmt.Errorf("%s: no value", at)
		case utf8.RuneCountInString(button.Value) > MaxChatButtonValue:
			return fmt.Errorf("%s: the value is longer than %d characters", at, MaxChatButtonValue)
		case values[button.Value]:
			return fmt.Errorf("%s: the value %q is another button's too; each button needs its own", at, button.Value)
		}
		switch button.Style {
		case "", "primary", "danger":
		default:
			return fmt.Errorf("%s: style must be primary or danger, or empty, not %q", at, button.Style)
		}
		values[button.Value] = true
	}
	return nil
}

// SettleClick rewrites the message a click was on to say line, its
// buttons gone (Slack) or disabled (Discord), within SendTimeout.
func (b *ChatBots) SettleClick(ctx context.Context, e ChatEvent, line string) error {
	if e.Click == nil || e.Click.settle == nil {
		return fmt.Errorf("bot %s: not a click on a button", e.Bot)
	}
	var secrets []string
	if b != nil {
		b.mu.Lock()
		if bot, ok := b.bots[e.Bot]; ok {
			secrets = bot.spec.Secrets
		}
		b.mu.Unlock()
	}
	timeout := 15 * time.Second
	if b != nil && b.SendTimeout > 0 {
		timeout = b.SendTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := e.Click.settle(ctx, line); err != nil {
		return fmt.Errorf("bot %s: updating message %s: %s", e.Bot, e.MessageID, scrubChatError(err.Error(), secrets))
	}
	return nil
}

// React adds the bot's emoji to a message, or removes it, as Send sends: in
// the bot's default channel when channel is empty, refused at once for a bot
// that cannot send, and given up after SendTimeout.
func (b *ChatBots) React(ctx context.Context, name, channel, messageID, emoji string, remove bool) error {
	bot, channel, err := b.ready(name, channel)
	if err != nil {
		return err
	}
	if messageID = strings.TrimSpace(messageID); messageID == "" {
		return fmt.Errorf("bot %s: no message given to react to", name)
	}
	if strings.TrimSpace(emoji) == "" {
		return fmt.Errorf("bot %s: no emoji given", name)
	}
	ctx, cancel := context.WithTimeout(ctx, b.SendTimeout)
	defer cancel()
	verb := "reacting to"
	if remove {
		verb = "removing a reaction from"
	}
	if err := bot.spec.Conn.React(ctx, channel, messageID, emoji, remove); err != nil {
		return fmt.Errorf("bot %s: %s %s in %s: %s", name, verb, messageID, channel, scrubChatError(err.Error(), bot.spec.Secrets))
	}
	return nil
}

// ReadThread reads the thread threadID names, as Send sends: in the bot's
// default channel when channel is empty, refused at once for a bot that
// cannot call its platform, given up after ReadTimeout. It keeps the parent,
// then the newest limit-1 of the other messages, leaving out bots' messages
// unless includeBots (never the parent).
func (b *ChatBots) ReadThread(ctx context.Context, name, channel, threadID string, limit int, includeBots bool) (ChatThread, error) {
	bot, channel, err := b.ready(name, channel)
	if err != nil {
		return ChatThread{}, err
	}
	if threadID = strings.TrimSpace(threadID); threadID == "" {
		return ChatThread{}, fmt.Errorf("bot %s: no thread given to read", name)
	}
	ctx, cancel := context.WithTimeout(ctx, b.ReadTimeout)
	defer cancel()
	read, err := bot.spec.Conn.ReadThread(ctx, channel, threadID, ChatThreadReadMax)
	if err != nil {
		return ChatThread{}, fmt.Errorf("bot %s: reading thread %s in %s: %s", name, threadID, channel, scrubChatError(err.Error(), bot.spec.Secrets))
	}
	var thread ChatThread
	for i, m := range read {
		if i == 0 || includeBots || !m.FromBot {
			thread.Messages = append(thread.Messages, m)
		}
	}
	if limit > 0 && len(thread.Messages) > limit {
		thread.LeftOut = len(thread.Messages) - limit
		thread.Messages = append(thread.Messages[:1], thread.Messages[len(thread.Messages)-(limit-1):]...)
	}
	return thread, nil
}

// ready is the bot of a name if it can call its platform, and the channel to
// use: channel, else the bot's default channel.
func (b *ChatBots) ready(name, channel string) (*chatBot, string, error) {
	if b == nil {
		return nil, "", fmt.Errorf("%w: chat bots are not available in this process", ErrChatBotNotConnected)
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
		return nil, "", fmt.Errorf("%w named %q: set up Slack or Discord in Settings › Integrations", ErrChatBotUnknown, name)
	case !bot.spec.Enabled:
		return nil, "", fmt.Errorf("bot %s: %w: it is switched off in Settings › Integrations", name, ErrChatBotNotConnected)
	case status.State == ChatBotAuthFailed:
		return nil, "", fmt.Errorf("bot %s: %w: auth failed: %s; check its tokens in Settings › Integrations", name, ErrChatBotNotConnected, status.LastError)
	}
	if channel = strings.TrimSpace(channel); channel == "" {
		channel = bot.spec.DefaultChannel
	}
	if channel == "" {
		return nil, "", fmt.Errorf("bot %s: no channel given, and the bot has no default channel", name)
	}
	return bot, channel, nil
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
