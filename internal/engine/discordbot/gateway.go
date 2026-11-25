package discordbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"runtime"
	"time"

	"nhooyr.io/websocket"
)

// Gateway opcodes.
const (
	opDispatch       = 0
	opHeartbeat      = 1
	opIdentify       = 2
	opResume         = 6
	opReconnect      = 7
	opInvalidSession = 9
	opHello          = 10
	opHeartbeatACK   = 11
)

const (
	// readLimit is the largest gateway message read (GUILD_CREATE can be big).
	readLimit = 16 << 20
	// writeTimeout bounds one gateway write.
	writeTimeout = 10 * time.Second
	// closeWait is how long Run waits for the close handshake before returning.
	closeWait = time.Second
	// closeReconnect is the code Run closes with when the session should be
	// resumed: anything but 1000 and 1001, which end the session.
	closeReconnect websocket.StatusCode = 4000
)

type gatewayPayload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s"`
	T  string          `json:"t"`
}

type readResult struct {
	data []byte
	err  error
}

// Run runs one gateway session and returns when it ends. With a session kept
// from an earlier run it RESUMEs at that session's resume address; otherwise it
// IDENTIFYs. onReady gets the bot's user ID on READY and on RESUMED. onMessage
// gets every MESSAGE_CREATE, the bot's own included (compare with SelfID).
// Both are called on Run's goroutine.
//
// Run returns ErrReconnect (wrapped) when it should be called again at once,
// ErrAuth, ErrIntents or ErrConfig when retrying will not help, ctx.Err() when
// ctx ends (the session is closed with 1000 and forgotten), and any other
// error when the caller should back off and call Run again, which resumes.
func (c *Client) Run(ctx context.Context, onMessage func(Message), onReady func(selfID string)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if onMessage == nil {
		onMessage = func(Message) {}
	}
	if onReady == nil {
		onReady = func(string) {}
	}

	c.mu.Lock()
	resume := c.sessionID != "" && c.resumeURL != ""
	base := c.resumeURL
	c.mu.Unlock()
	if !resume {
		base = c.GatewayURL
		if base == "" {
			u, err := c.fetchGatewayURL(ctx)
			if err != nil {
				return err
			}
			base = u
		}
	}
	addr, err := gatewayAddress(base)
	if err != nil {
		return err
	}

	conn, _, err := websocket.Dial(ctx, addr, &websocket.DialOptions{HTTPClient: &http.Client{Timeout: 30 * time.Second}})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("discord: connecting to the gateway: %w", err)
	}
	conn.SetReadLimit(readLimit)
	defer func() { _ = conn.CloseNow() }()

	// The reader goroutine only passes frames on; all state is Run's.
	reads := make(chan readResult)
	quit := make(chan struct{})
	defer close(quit)
	go func() {
		for {
			_, data, err := conn.Read(context.Background())
			select {
			case reads <- readResult{data, err}:
			case <-quit:
				return
			}
			if err != nil {
				return
			}
		}
	}()

	s := &session{c: c, ctx: ctx, conn: conn, resume: resume, onMessage: onMessage, onReady: onReady, acked: true, reads: reads}
	return s.loop(reads)
}

type session struct {
	c         *Client
	ctx       context.Context
	conn      *websocket.Conn
	resume    bool
	onMessage func(Message)
	onReady   func(string)
	// reads is the reader goroutine's channel, which a failed write asks
	// for the close the server may have sent.
	reads <-chan readResult

	hello    bool
	interval time.Duration
	timer    *time.Timer
	acked    bool
}

func (s *session) loop(reads <-chan readResult) error {
	var beat <-chan time.Time
	defer func() {
		if s.timer != nil {
			s.timer.Stop()
		}
	}()
	for {
		select {
		case <-s.ctx.Done():
			s.close(websocket.StatusNormalClosure, "")
			s.c.clearSession()
			return s.ctx.Err()

		case <-beat:
			if !s.acked {
				s.close(closeReconnect, "heartbeat not acknowledged")
				return fmt.Errorf("%w: heartbeat not acknowledged", ErrReconnect)
			}
			if err := s.heartbeat(); err != nil {
				return err
			}
			s.acked = false
			s.timer.Reset(s.interval)

		case r := <-reads:
			if r.err != nil {
				return s.readError(r.err)
			}
			var p gatewayPayload
			if err := json.Unmarshal(r.data, &p); err != nil {
				continue
			}
			switch p.Op {
			case opHello:
				if s.hello {
					continue
				}
				var d struct {
					HeartbeatInterval float64 `json:"heartbeat_interval"`
				}
				if err := json.Unmarshal(p.D, &d); err != nil || d.HeartbeatInterval <= 0 {
					s.close(closeReconnect, "bad hello")
					return errors.New("discord: gateway HELLO without a heartbeat interval")
				}
				s.hello = true
				s.interval = time.Duration(d.HeartbeatInterval * float64(time.Millisecond))
				jitter := rand.Float64
				if s.c.Jitter != nil {
					jitter = s.c.Jitter
				}
				s.timer = time.NewTimer(time.Duration(float64(s.interval) * jitter()))
				beat = s.timer.C
				if err := s.greet(); err != nil {
					return err
				}

			case opHeartbeatACK:
				s.acked = true

			case opHeartbeat:
				if err := s.heartbeat(); err != nil {
					return err
				}

			case opReconnect:
				s.close(closeReconnect, "reconnect")
				return fmt.Errorf("%w: asked by Discord", ErrReconnect)

			case opInvalidSession:
				var resumable bool
				_ = json.Unmarshal(p.D, &resumable)
				s.close(closeReconnect, "invalid session")
				if resumable {
					return fmt.Errorf("%w: invalid session, resumable", ErrReconnect)
				}
				s.c.clearSession()
				if err := sleep(s.ctx, s.c.invalidSessionWait()); err != nil {
					return err
				}
				return fmt.Errorf("%w: invalid session", ErrReconnect)

			case opDispatch:
				if p.S != nil {
					s.c.mu.Lock()
					s.c.seq, s.c.hasSeq = *p.S, true
					s.c.mu.Unlock()
				}
				s.dispatch(p.T, p.D)
			}
		}
	}
}

// greet sends IDENTIFY or RESUME.
func (s *session) greet() error {
	c := s.c
	if s.resume {
		c.mu.Lock()
		d := map[string]any{"token": c.Token, "session_id": c.sessionID, "seq": c.seq}
		c.mu.Unlock()
		return s.send(opResume, d)
	}
	return s.send(opIdentify, map[string]any{
		"token":   c.Token,
		"intents": c.intents(),
		"properties": map[string]string{
			"os":      runtime.GOOS,
			"browser": "legato",
			"device":  "legato",
		},
	})
}

func (s *session) heartbeat() error {
	s.c.mu.Lock()
	var d any
	if s.c.hasSeq {
		d = s.c.seq
	}
	s.c.mu.Unlock()
	return s.send(opHeartbeat, d)
}

func (s *session) send(op int, d any) error {
	b, err := json.Marshal(map[string]any{"op": op, "d": d})
	if err != nil {
		return fmt.Errorf("discord: encoding op %d: %w", op, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	if err := s.conn.Write(ctx, websocket.MessageText, b); err != nil {
		if s.ctx.Err() != nil {
			s.c.clearSession()
			return s.ctx.Err()
		}
		return s.writeError(err)
	}
	return nil
}

// writeError is a failed write's error. A write fails when the server has
// just closed the connection, often before the reader has passed its close
// frame on; the close code the server sent says why, so it wins.
func (s *session) writeError(err error) error {
	deadline := time.NewTimer(closeWait)
	defer deadline.Stop()
	for {
		select {
		case r := <-s.reads:
			if r.err == nil {
				continue // a frame that came before the close
			}
			if websocket.CloseStatus(r.err) != -1 {
				return s.readError(r.err)
			}
			return fmt.Errorf("discord: gateway write: %w", err)
		case <-deadline.C:
			return fmt.Errorf("discord: gateway write: %w", err)
		case <-s.ctx.Done():
			s.c.clearSession()
			return s.ctx.Err()
		}
	}
}

// close starts the close handshake and waits for it at most closeWait.
func (s *session) close(code websocket.StatusCode, reason string) {
	done := make(chan struct{})
	go func() {
		_ = s.conn.Close(code, reason)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(closeWait):
	}
}

func (s *session) readError(err error) error {
	if s.ctx.Err() != nil {
		s.c.clearSession()
		return s.ctx.Err()
	}
	code := websocket.CloseStatus(err)
	switch code {
	case 4004:
		return ErrAuth
	case 4013, 4014:
		return ErrIntents
	case 4010, 4011, 4012:
		return fmt.Errorf("%w (close %d)", ErrConfig, int(code))
	case 4007, 4009:
		s.c.clearSession()
		return fmt.Errorf("%w: session ended (close %d)", ErrReconnect, int(code))
	case -1:
		return fmt.Errorf("discord: gateway connection lost: %w", err)
	default:
		return fmt.Errorf("discord: gateway closed: %w", err)
	}
}

func (s *session) dispatch(typ string, raw json.RawMessage) {
	c := s.c
	switch typ {
	case "READY":
		var d struct {
			SessionID        string `json:"session_id"`
			ResumeGatewayURL string `json:"resume_gateway_url"`
			User             struct {
				ID string `json:"id"`
			} `json:"user"`
		}
		if json.Unmarshal(raw, &d) != nil {
			return
		}
		c.mu.Lock()
		c.sessionID, c.resumeURL, c.selfID = d.SessionID, d.ResumeGatewayURL, d.User.ID
		c.threads = nil
		c.mu.Unlock()
		s.onReady(d.User.ID)

	case "RESUMED":
		s.onReady(c.SelfID())

	case "GUILD_CREATE", "THREAD_LIST_SYNC":
		var d struct {
			Threads []channel `json:"threads"`
		}
		if json.Unmarshal(raw, &d) != nil {
			return
		}
		for _, t := range d.Threads {
			c.markThread(t, true)
		}

	case "THREAD_CREATE", "THREAD_UPDATE":
		var t channel
		if json.Unmarshal(raw, &t) == nil {
			c.markThread(t, true)
		}

	case "THREAD_DELETE":
		var t channel
		if json.Unmarshal(raw, &t) == nil {
			c.markThread(t, false)
		}

	case "MESSAGE_CREATE":
		var d struct {
			ID        string `json:"id"`
			ChannelID string `json:"channel_id"`
			GuildID   string `json:"guild_id"`
			Author    struct {
				ID  string `json:"id"`
				Bot bool   `json:"bot"`
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
		if json.Unmarshal(raw, &d) != nil {
			return
		}
		m := Message{
			ID: d.ID, ChannelID: d.ChannelID, GuildID: d.GuildID,
			AuthorID: d.Author.ID, AuthorIsBot: d.Author.Bot,
			Content: d.Content,
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
		c.mu.Lock()
		m.InThread = d.Position != nil || c.threads[d.ChannelID]
		c.mu.Unlock()
		s.onMessage(m)
	}
}

type channel struct {
	ID   string `json:"id"`
	Type int    `json:"type"`
}

// isThreadType: announcement, public and private threads.
func isThreadType(t int) bool { return t == 10 || t == 11 || t == 12 }

func (c *Client) markThread(ch channel, present bool) {
	if ch.ID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !present {
		delete(c.threads, ch.ID)
		return
	}
	if !isThreadType(ch.Type) {
		return
	}
	if c.threads == nil {
		c.threads = map[string]bool{}
	}
	c.threads[ch.ID] = true
}

// clearSession forgets the session, so the next Run identifies.
func (c *Client) clearSession() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID, c.resumeURL = "", ""
	c.seq, c.hasSeq = 0, false
}

func (c *Client) invalidSessionWait() time.Duration {
	if c.InvalidSessionWait != nil {
		return c.InvalidSessionWait()
	}
	return time.Second + rand.N(4*time.Second)
}

func (c *Client) fetchGatewayURL(ctx context.Context) (string, error) {
	var out struct {
		URL string `json:"url"`
	}
	if err := c.do(ctx, http.MethodGet, c.apiBase()+"/gateway/bot", nil, &out); err != nil {
		if errors.Is(err, ErrAuth) || ctx.Err() != nil {
			return "", err
		}
		return "", fmt.Errorf("discord: asking for the gateway address: %w", err)
	}
	if out.URL == "" {
		return "", errors.New("discord: no gateway address from /gateway/bot")
	}
	return out.URL, nil
}

// gatewayAddress adds the version and encoding to a gateway address.
func gatewayAddress(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return "", errors.New("discord: bad gateway address")
	}
	q := u.Query()
	q.Set("v", "10")
	q.Set("encoding", "json")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
