package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/service"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// A worker's Pull and Events for the main instances paired with it: GET
// /api/integrations/handoff/pull, and the WebSocket at
// /api/integrations/handoff/events, opened by a main with its handoff token
// as a bearer header. See docs/ai/integrations.md § Live away state.

// HandoffFeed is how a worker tells a main of its handoffs: a pull, and the
// rows of the Events connection. The worker's handoff service implements
// it beside TaskHandoffs.
type HandoffFeed interface {
	Pull(ctx context.Context, mainInstanceID string) (service.HandoffPull, error)
	Stream(ctx context.Context, mainInstanceID string, send func(service.HandoffStreamMessage) error) error
}

// SetHandoffReauthEvery sets how often an Events connection checks that its
// main's token still works (5 seconds by default): once the worker unpairs
// the main, the connection closes with service.HandoffStreamRefused.
func (s *Server) SetHandoffReauthEvery(d time.Duration) { s.instances.reauthEvery = d }

// handoffStreams are a server's open Events connections.
type handoffStreams struct {
	mu       sync.Mutex
	open     map[*websocket.Conn]context.CancelFunc
	shutdown sync.Once
}

// add keeps an open connection until remove, and has the server close it
// when it shuts down.
func (s *Server) addHandoffStream(conn *websocket.Conn, cancel context.CancelFunc) {
	st := &s.instances.streams
	st.shutdown.Do(func() { s.server.RegisterOnShutdown(s.closeHandoffStreams) })
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.open == nil {
		st.open = map[*websocket.Conn]context.CancelFunc{}
	}
	st.open[conn] = cancel
}

func (s *Server) removeHandoffStream(conn *websocket.Conn) {
	st := &s.instances.streams
	st.mu.Lock()
	defer st.mu.Unlock()
	delete(st.open, conn)
}

// closeHandoffStreams closes every open Events connection: the worker is
// going away, and its mains reconnect.
func (s *Server) closeHandoffStreams() {
	st := &s.instances.streams
	st.mu.Lock()
	defer st.mu.Unlock()
	for conn, cancel := range st.open {
		conn.Close(websocket.StatusGoingAway, "the worker is stopping")
		cancel()
	}
	st.open = nil
}

func (s *Server) handoffFeed(w http.ResponseWriter) (HandoffFeed, bool) {
	feed, ok := s.instances.handoffs.(HandoffFeed)
	if !ok {
		writeIntegrationError(w, http.StatusServiceUnavailable, "unavailable", "this worker does not take handoffs")
	}
	return feed, ok
}

// handoffPullHandler is Pull: a row for each of the calling main's
// unfinished handoffs.
func (s *Server) handoffPullHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		main, ok := requireHandoffMain(w, r)
		if !ok {
			return
		}
		feed, ok := s.handoffFeed(w)
		if !ok {
			return
		}
		pull, err := feed.Pull(r.Context(), main.InstanceID)
		if err != nil {
			writeIntegrationError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSONValue(w, http.StatusOK, pull)
	}
}

// handoffEventsHandler is the Events connection. The main sends
// {"type":"pull"} once it has made the calls it owes; the worker answers
// with a pull, then rows as they change. Every pull asked for, and every
// check of the token, counts as the main's last pull here.
func (s *Server) handoffEventsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		main, ok := requireHandoffMain(w, r)
		if !ok {
			return
		}
		feed, ok := s.handoffFeed(w)
		if !ok {
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(64 << 10)
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		s.addHandoffStream(conn, cancel)
		defer s.removeHandoffStream(conn)

		// The token is checked again while the connection lasts: a main
		// unpaired here hears so, and reconnects to a 401.
		stillPaired := func() bool {
			if s.integrationAuth == nil {
				return true
			}
			_, err := s.integrationAuth.Authenticate(ctx, token)
			return !errors.Is(err, service.ErrIntegrationUnauthorized)
		}
		every := s.instances.reauthEvery
		if every <= 0 {
			every = 5 * time.Second
		}
		go func() {
			ticker := time.NewTicker(every)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if !stillPaired() {
						conn.Close(service.HandoffStreamRefused, "this worker no longer accepts this main; pair again")
						cancel()
						return
					}
				}
			}
		}()

		send := func(m service.HandoffStreamMessage) error {
			// A main unpaired here hears nothing more: not the handoffs
			// that unpairing closed, which would read as lost to it.
			if !stillPaired() {
				conn.Close(service.HandoffStreamRefused, "this worker no longer accepts this main; pair again")
				return errors.New("the main is no longer paired")
			}
			writeCtx, done := context.WithTimeout(ctx, 10*time.Second)
			defer done()
			return wsjson.Write(writeCtx, conn, m)
		}
		streaming := false
		for {
			var m service.HandoffStreamMessage
			if err := wsjson.Read(ctx, conn, &m); err != nil {
				return
			}
			if m.Type != service.HandoffStreamPull || streaming {
				continue
			}
			if !stillPaired() {
				conn.Close(service.HandoffStreamRefused, "this worker no longer accepts this main; pair again")
				return
			}
			streaming = true
			go func() {
				defer cancel()
				if err := feed.Stream(ctx, main.InstanceID, send); err != nil && ctx.Err() == nil {
					conn.Close(websocket.StatusInternalError, "the worker could not read its handoffs")
				}
			}()
		}
	}
}
