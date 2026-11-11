package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cpave3/legato/internal/service"
	"nhooyr.io/websocket"
)

const (
	// voiceSessionPath is the voice session's WebSocket.
	voiceSessionPath = "/api/reeve/voice/session"
	// voiceHelloWait is how long a device has to say hello.
	voiceHelloWait = 10 * time.Second
	// voiceOutQueue is how many messages may wait for a slow device
	// before its session is closed.
	voiceOutQueue = 1024
)

// VoiceSessions opens voice sessions (service.ReeveVoice).
type VoiceSessions interface {
	Open(hello service.VoiceHello, out func(service.VoiceOut)) *service.VoiceSession
}

// reeveVoiceSessionHandler serves GET /api/reeve/voice/session: a
// WebSocket that carries one device's spoken conversation with the Reeve.
// Text frames are JSON messages, the first a hello; binary frames are
// audio, 16 kHz mono s16le PCM. See docs/ai/reeve.md § Hands-free voice.
func (s *Server) reeveVoiceSessionHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		voice, ok := s.reeveVoice.(VoiceSessions)
		if !ok || voice == nil {
			s.writeError(w, http.StatusServiceUnavailable, "this server has no voice for the Reeve")
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(1 << 20)
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		hello, ok := readVoiceHello(ctx, conn)
		if !ok {
			conn.Close(websocket.StatusPolicyViolation, "say hello first")
			return
		}

		queue := make(chan service.VoiceOut, voiceOutQueue)
		session := voice.Open(hello, func(out service.VoiceOut) {
			select {
			case queue <- out:
			default:
				// The device does not keep up: end its connection rather
				// than hold the session.
				cancel()
			}
		})
		defer session.Close()

		writerDone := make(chan struct{})
		go func() {
			defer close(writerDone)
			writeVoiceMessages(ctx, conn, queue, session.Done())
			cancel()
		}()

		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				break
			}
			if typ == websocket.MessageBinary {
				session.Audio(data)
				continue
			}
			var msg service.VoiceIn
			if json.Unmarshal(data, &msg) == nil && msg.Type != "" {
				session.Handle(msg)
			}
		}
		session.Close()
		cancel()
		<-writerDone
	}
}

func readVoiceHello(ctx context.Context, conn *websocket.Conn) (service.VoiceHello, bool) {
	ctx, cancel := context.WithTimeout(ctx, voiceHelloWait)
	defer cancel()
	typ, data, err := conn.Read(ctx)
	if err != nil || typ != websocket.MessageText {
		return service.VoiceHello{}, false
	}
	var msg service.VoiceIn
	if json.Unmarshal(data, &msg) != nil || msg.Type != "hello" {
		return service.VoiceHello{}, false
	}
	return service.VoiceHello{Device: msg.Device, Client: msg.Client, Play: msg.Play}, true
}

// writeVoiceMessages sends the session's messages until it ends, then
// what is left, then closes the connection.
func writeVoiceMessages(ctx context.Context, conn *websocket.Conn, queue <-chan service.VoiceOut, done <-chan struct{}) {
	write := func(out service.VoiceOut) bool {
		data, err := json.Marshal(out)
		if err != nil {
			return true
		}
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return conn.Write(wctx, websocket.MessageText, data) == nil
	}
	for {
		select {
		case <-ctx.Done():
			return
		case out := <-queue:
			if !write(out) {
				return
			}
		case <-done:
			for {
				select {
				case out := <-queue:
					if !write(out) {
						return
					}
				default:
					conn.Close(websocket.StatusNormalClosure, "")
					return
				}
			}
		}
	}
}
