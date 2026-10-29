// Package broker is the socket through which the agent-side processes of a
// sandboxed agent (hooks, `legato mcp serve`, the bridges) ask the host
// Legato to act for them, instead of opening its database. Each request
// carries the token of one execution; what a token may do is the host's
// matter (service.Broker). Requests and replies are newline-delimited JSON,
// one request and one reply per connection.
package broker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Timeout is how long a call waits for its reply when its context has no
// deadline of its own.
const Timeout = 2 * time.Second

// maxRequest bounds one request: an image sent inline (25 MiB) in base64.
const maxRequest = 40 << 20

// Request is one call.
type Request struct {
	ID    string          `json:"id"`
	Token string          `json:"token"`
	Op    string          `json:"op"`
	Args  json.RawMessage `json:"args,omitempty"`
}

// Error is a refusal the caller may read: a code and words for the person.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

type reply struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
}

// ErrUnreachable: the broker did not answer (no socket, or no reply in
// time). Callers that only report state carry on without it.
var ErrUnreachable = errors.New("Legato's broker is unreachable")

// Handler answers one request. An *Error is passed to the caller as it is;
// any other error becomes "internal", and its text stays in the host's log.
type Handler func(ctx context.Context, req Request) (any, error)

// Server serves a Handler on a Unix socket.
type Server struct {
	path    string
	ln      net.Listener
	handler Handler
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// Listen serves h on path, readable by this user only. A socket file left
// by a process that ended is replaced; a live one is not.
func Listen(path string, h Handler) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil {
		if conn, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
			_ = conn.Close()
			return nil, fmt.Errorf("a broker is already listening at %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{path: path, ln: ln, handler: h, ctx: ctx, cancel: cancel}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

// Path is where the server listens.
func (s *Server) Path() string { return s.path }

// Close stops listening, waits for calls in progress, and removes the socket.
func (s *Server) Close() error {
	s.cancel()
	err := s.ln.Close()
	s.wg.Wait()
	return err
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.serve(conn)
		}()
	}
}

func (s *Server) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64<<10), maxRequest)
	if !scanner.Scan() {
		return
	}
	var req Request
	out := reply{}
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		out.Error = &Error{Code: "bad_request", Message: "the request does not read"}
	} else {
		out.ID = req.ID
		result, err := s.handler(s.ctx, req)
		var be *Error
		switch {
		case errors.As(err, &be):
			out.Error = be
		case err != nil:
			log.Printf("broker: %s: %v", req.Op, err)
			out.Error = &Error{Code: "internal", Message: "Legato could not do this; see its log"}
		case result != nil:
			raw, err := json.Marshal(result)
			if err != nil {
				out.Error = &Error{Code: "internal", Message: "the result does not encode"}
			} else {
				out.Result = raw
			}
		}
	}
	line, _ := json.Marshal(out)
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, _ = conn.Write(append(line, '\n'))
}

// Call sends one request to the broker at socket and reads its reply into
// result (when not nil). It gives up after Timeout unless ctx says
// otherwise, with ErrUnreachable.
func Call(ctx context.Context, socket, token, op string, args, result any) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, Timeout)
		defer cancel()
	}
	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			return err
		}
		raw = b
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	line, err := json.Marshal(Request{ID: fmt.Sprintf("%d", time.Now().UnixNano()), Token: token, Op: op, Args: raw})
	if err != nil {
		return err
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 64<<10), maxRequest)
	if !scanner.Scan() {
		err := scanner.Err()
		if err == nil {
			err = errors.New("no reply")
		}
		return fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	var out reply
	if err := json.Unmarshal(scanner.Bytes(), &out); err != nil {
		return fmt.Errorf("the broker's reply does not read: %w", err)
	}
	if out.Error != nil {
		return out.Error
	}
	if result != nil && len(out.Result) > 0 {
		return json.Unmarshal(out.Result, result)
	}
	return nil
}
