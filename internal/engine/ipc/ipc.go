package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// Message is the IPC protocol unit between CLI clients and the TUI server.
type Message struct {
	Type    string `json:"type"`
	TaskID  string `json:"task_id"`
	Status  string `json:"status,omitempty"`
	Content string `json:"content,omitempty"`
}

// SocketDir returns the directory for Legato IPC sockets.
// Uses $XDG_RUNTIME_DIR/legato/, falling back to /tmp/legato-<uid>/.
func SocketDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "legato")
	}
	return fmt.Sprintf("/tmp/legato-%d", os.Getuid())
}

// SocketPath returns a unique socket path for this process.
func SocketPath() string {
	return filepath.Join(SocketDir(), fmt.Sprintf("legato-%d.sock", os.Getpid()))
}

// Server listens on a Unix domain socket for IPC messages.
type Server struct {
	listener   net.Listener
	callback   func(Message)
	socketPath string
	wg         sync.WaitGroup
	done       chan struct{}
}

// NewServer creates and starts an IPC server on the given socket path.
// The callback is invoked for each valid message received.
// Stale socket files are cleaned up automatically.
func NewServer(socketPath string, callback func(Message)) (*Server, error) {
	// Clean up stale socket if present.
	if _, err := os.Stat(socketPath); err == nil {
		conn, dialErr := net.Dial("unix", socketPath)
		if dialErr != nil {
			// No one listening — stale file.
			os.Remove(socketPath)
		} else {
			conn.Close()
			return nil, fmt.Errorf("another instance is already listening on %s", socketPath)
		}
	}

	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, fmt.Errorf("creating socket directory: %w", err)
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listening on %s: %w", socketPath, err)
	}

	s := &Server{
		listener:   ln,
		callback:   callback,
		socketPath: socketPath,
		done:       make(chan struct{}),
	}

	s.wg.Add(1)
	go s.accept()

	return s, nil
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				continue
			}
		}
		s.wg.Add(1)
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue // skip malformed messages
		}
		s.callback(msg)
	}
}

// Close stops the server and removes the socket file.
func (s *Server) Close() error {
	close(s.done)
	err := s.listener.Close()
	s.wg.Wait()
	os.Remove(s.socketPath)
	return err
}

// Send connects to a single IPC socket, sends a message, and disconnects.
// Returns nil silently if the socket doesn't exist or the connection is refused.
func Send(socketPath string, msg Message) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil
	}
	defer conn.Close()

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshalling message: %w", err)
	}
	data = append(data, '\n')

	_, err = conn.Write(data)
	return err
}

// Broadcast sends a message to all Legato IPC sockets in the socket directory.
// Best-effort: silently skips sockets that can't be reached.
func Broadcast(msg Message) {
	dir := SocketDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sock" {
			continue
		}
		Send(filepath.Join(dir, e.Name()), msg)
	}
}
