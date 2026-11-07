package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"nhooyr.io/websocket"
)

// TerminalSession is a real terminal client attached to an agent's tmux
// session: output out of Read, keystrokes into Write.
type TerminalSession interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
}

// TerminalAttacher attaches a new client to the named tmux session.
type TerminalAttacher func(sessionName string, cols, rows int) (TerminalSession, error)

// SetTerminalAttacher enables GET /api/agents/{task_id}/terminal.
func (s *Server) SetTerminalAttacher(a TerminalAttacher) { s.terminal = a }

const maxTerminalDim = 1000

// terminalHandler bridges a browser's xterm.js to a tmux client on a pty:
// binary frames are the terminal's bytes both ways, unparsed; a text frame
// {"type":"resize","cols":N,"rows":N} resizes the client. Each connection is
// a client of its own, so tmux does the screen, the scrollback and the sizing.
func (s *Server) terminalHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("task_id")
		if id == "" || strings.ContainsAny(id, "/\\:.") {
			http.Error(w, `{"error":"bad agent id"}`, http.StatusBadRequest)
			return
		}
		cols, _ := strconv.Atoi(r.URL.Query().Get("cols"))
		rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
		if cols < 1 || rows < 1 || cols > maxTerminalDim || rows > maxTerminalDim {
			http.Error(w, `{"error":"cols and rows are required"}`, http.StatusBadRequest)
			return
		}
		if s.terminal == nil {
			http.Error(w, `{"error":"terminal not available"}`, http.StatusServiceUnavailable)
			return
		}
		term, err := s.terminal("legato-"+id, cols, rows)
		if err != nil {
			http.Error(w, `{"error":"no terminal for this agent"}`, http.StatusNotFound)
			return
		}
		defer term.Close()

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(1 << 20)

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()

		// terminal -> browser. Writes block while the browser is slow, which
		// stops reading the pty and so applies backpressure to tmux.
		go func() {
			defer cancel()
			buf := make([]byte, 32*1024)
			for {
				n, err := term.Read(buf)
				if n > 0 {
					if werr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
						return
					}
				}
				if err != nil {
					conn.Close(websocket.StatusNormalClosure, "session ended")
					return
				}
			}
		}()

		// browser -> terminal.
		go func() {
			<-ctx.Done()
			term.Close() // unblocks the reader above
		}()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				if _, err := term.Write(data); err != nil {
					return
				}
				continue
			}
			var msg struct {
				Type string `json:"type"`
				Cols int    `json:"cols"`
				Rows int    `json:"rows"`
			}
			if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" &&
				msg.Cols > 0 && msg.Rows > 0 && msg.Cols <= maxTerminalDim && msg.Rows <= maxTerminalDim {
				_ = term.Resize(msg.Cols, msg.Rows)
			}
		}
	}
}
