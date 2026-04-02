package server

import (
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// clientSize tracks a client's terminal dimensions and when they last reported.
type clientSize struct {
	cols    int
	rows    int
	lastSeen time.Time
}

// agentStream manages a single pipe-pane per agent, fanning out to multiple clients.
type agentStream struct {
	mu      sync.Mutex
	clients map[*wsClient]struct{}
	sizes   map[*wsClient]*clientSize
	cleanup func()
	agentID string
	cancel  chan struct{}
	// Current applied pane size.
	appliedCols int
	appliedRows int
}

// streamManager tracks active streams per agent, ensuring only one pipe-pane per agent.
type streamManager struct {
	mu      sync.Mutex
	streams map[string]*agentStream // keyed by agentID
	tmux    service.TmuxManager
}

const sizeTTL = 10 * time.Second

func newStreamManager(tmux service.TmuxManager) *streamManager {
	return &streamManager{
		streams: make(map[string]*agentStream),
		tmux:    tmux,
	}
}

// subscribe adds a client to an agent's stream. Starts the pipe if this is the first subscriber.
func (sm *streamManager) subscribe(agentID string, client *wsClient) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	s, ok := sm.streams[agentID]
	if ok {
		// Stream already running — just add the client.
		s.mu.Lock()
		s.clients[client] = struct{}{}
		s.mu.Unlock()
		return nil
	}

	// Start a new stream.
	sessionName := "legato-" + agentID
	reader, cleanup, err := sm.tmux.PipeOutput(sessionName)
	if err != nil {
		return err
	}

	s = &agentStream{
		clients: map[*wsClient]struct{}{client: {}},
		sizes:   make(map[*wsClient]*clientSize),
		cleanup: cleanup,
		agentID: agentID,
		cancel:  make(chan struct{}),
	}
	sm.streams[agentID] = s

	go sm.readLoop(s, reader)

	return nil
}

// unsubscribe removes a client from an agent's stream. Stops the pipe if no subscribers remain.
func (sm *streamManager) unsubscribe(agentID string, client *wsClient) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	s, ok := sm.streams[agentID]
	if !ok {
		return
	}

	s.mu.Lock()
	delete(s.clients, client)
	delete(s.sizes, client)
	remaining := len(s.clients)
	s.mu.Unlock()

	if remaining == 0 {
		close(s.cancel)
		s.cleanup()
		delete(sm.streams, agentID)
	} else {
		// Recalculate size without this client.
		sm.resizePane(s)
	}
}

// unsubscribeAll removes a client from all streams (called on disconnect).
func (sm *streamManager) unsubscribeAll(client *wsClient) {
	sm.mu.Lock()
	agentIDs := make([]string, 0)
	for id, s := range sm.streams {
		s.mu.Lock()
		if _, ok := s.clients[client]; ok {
			agentIDs = append(agentIDs, id)
		}
		s.mu.Unlock()
	}
	sm.mu.Unlock()

	for _, id := range agentIDs {
		sm.unsubscribe(id, client)
	}
}

// updateClientSize records a client's terminal size and resizes the pane if needed.
func (sm *streamManager) updateClientSize(agentID string, client *wsClient, cols, rows int) {
	sm.mu.Lock()
	s, ok := sm.streams[agentID]
	sm.mu.Unlock()
	if !ok {
		return
	}

	s.mu.Lock()
	s.sizes[client] = &clientSize{cols: cols, rows: rows, lastSeen: time.Now()}
	s.mu.Unlock()

	sm.resizePane(s)
}

// resizePane computes the minimum size across all active clients and resizes the tmux pane.
func (sm *streamManager) resizePane(s *agentStream) {
	if sm.tmux == nil {
		return
	}

	s.mu.Lock()
	minCols, minRows := 0, 0
	now := time.Now()

	for client, size := range s.sizes {
		// Skip stale clients.
		if now.Sub(size.lastSeen) > sizeTTL {
			delete(s.sizes, client)
			continue
		}
		if minCols == 0 || size.cols < minCols {
			minCols = size.cols
		}
		if minRows == 0 || size.rows < minRows {
			minRows = size.rows
		}
	}

	// Don't resize if no valid sizes, or if unchanged.
	if minCols == 0 || minRows == 0 || (minCols == s.appliedCols && minRows == s.appliedRows) {
		s.mu.Unlock()
		return
	}

	s.appliedCols = minCols
	s.appliedRows = minRows
	agentID := s.agentID
	s.mu.Unlock()

	sessionName := "legato-" + agentID
	sm.tmux.SetOption(sessionName, "window-size", "manual")

	// Use resize-window to set exact dimensions.
	// SetOption doesn't cover this — need a direct tmux call.
	// We'll use the SendKey path (no --) which just execs tmux.
	// Actually, we need a resize-window command. For now, use the approach of
	// setting aggressive-resize and forcing dimensions.
	if mgr, ok := sm.tmux.(interface {
		ResizeWindow(name string, cols, rows int) error
	}); ok {
		mgr.ResizeWindow(sessionName, minCols, minRows)
	} else {
		// Fallback: use tmux command directly via exec if we can find the binary.
		// The TmuxManager interface doesn't have ResizeWindow, so we exec directly.
		path, err := exec.LookPath("tmux")
		if err == nil {
			exec.Command(path, "resize-window", "-t", sessionName,
				"-x", fmt.Sprintf("%d", minCols),
				"-y", fmt.Sprintf("%d", minRows)).Run()
		}
	}
}

func (sm *streamManager) readLoop(s *agentStream, reader io.Reader) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-s.cancel:
			return
		default:
		}

		n, err := reader.Read(buf)
		if n > 0 {
			msg := WSMessage{
				Type:    MsgAgentOutput,
				AgentID: s.agentID,
				Content: string(buf[:n]),
			}
			s.mu.Lock()
			for c := range s.clients {
				go c.send(msg)
			}
			s.mu.Unlock()
		}
		if err != nil {
			log.Printf("stream %s ended: %v", s.agentID, err)
			return
		}
	}
}
