package server

import (
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/prompt"
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
	piping  bool // true once pipe-pane has been started
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

func newStreamManager(tmux service.TmuxManager) *streamManager {
	return &streamManager{
		streams: make(map[string]*agentStream),
		tmux:    tmux,
	}
}

// subscribe adds a client to an agent's stream. The pipe-pane is not started
// until the first resize message arrives, ensuring the pane is sized correctly
// before any output flows.
func (sm *streamManager) subscribe(agentID string, client *wsClient) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	s, ok := sm.streams[agentID]
	if ok {
		// Stream already exists — just add the client.
		s.mu.Lock()
		s.clients[client] = struct{}{}
		s.mu.Unlock()
		return
	}

	s = &agentStream{
		clients: map[*wsClient]struct{}{client: {}},
		sizes:   make(map[*wsClient]*clientSize),
		agentID: agentID,
		cancel:  make(chan struct{}),
	}
	sm.streams[agentID] = s
}

// startPipe starts the pipe-pane for a stream if not already running.
// Called after the first resize so the pane is correctly sized before output flows.
func (sm *streamManager) startPipe(s *agentStream) {
	s.mu.Lock()
	if s.piping {
		s.mu.Unlock()
		return
	}
	s.piping = true
	agentID := s.agentID
	s.mu.Unlock()

	sessionName := "legato-" + agentID

	// Wait briefly for the application to redraw after the resize SIGWINCH,
	// then capture the pane at the correct width as backfill.
	time.Sleep(150 * time.Millisecond)

	// Abort if the stream was torn down during the sleep.
	select {
	case <-s.cancel:
		s.mu.Lock()
		s.piping = false
		s.mu.Unlock()
		return
	default:
	}

	if snapshot, err := sm.tmux.CaptureWithEscapes(sessionName); err == nil && snapshot != "" {
		// capture-pane uses \n but xterm.js needs \r\n so each line
		// starts at column 0 instead of staircase-ing.
		snapshot = strings.ReplaceAll(snapshot, "\n", "\r\n")
		msg := WSMessage{
			Type:    MsgAgentOutput,
			AgentID: agentID,
			Content: snapshot,
		}
		s.mu.Lock()
		for c := range s.clients {
			go c.send(msg)
		}
		s.mu.Unlock()
	}

	// Now start the live pipe.
	reader, cleanup, err := sm.tmux.PipeOutput(sessionName)
	if err != nil {
		log.Printf("pipe-pane %s: %v", sessionName, err)
		s.mu.Lock()
		s.piping = false
		s.mu.Unlock()
		return
	}

	// Check again — if torn down while PipeOutput was running,
	// clean up immediately to avoid an orphaned pipe-pane.
	select {
	case <-s.cancel:
		cleanup()
		return
	default:
	}

	s.mu.Lock()
	s.cleanup = cleanup
	s.mu.Unlock()

	go sm.readLoop(s, reader)
}

// unsubscribe removes a client from an agent's stream. Stops the pipe if no subscribers remain.
func (sm *streamManager) unsubscribe(agentID string, client *wsClient) {
	sm.mu.Lock()

	s, ok := sm.streams[agentID]
	if !ok {
		sm.mu.Unlock()
		return
	}

	s.mu.Lock()
	delete(s.clients, client)
	delete(s.sizes, client)
	remaining := len(s.clients)
	s.mu.Unlock()

	if remaining == 0 {
		close(s.cancel)
		cleanupFn := s.cleanup
		delete(sm.streams, agentID)
		sessionName := "legato-" + agentID
		sm.mu.Unlock()

		// Run external commands without holding any locks.
		if cleanupFn != nil {
			cleanupFn()
		}
		// Revert to tmux auto-sizing so the terminal resumes its natural dimensions.
		sm.tmux.SetOption(sessionName, "window-size", "latest")
	} else {
		sm.mu.Unlock()
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

const sizeTTL = 10 * time.Second

// updateClientSize records a web client's terminal size, resizes the pane,
// and starts the pipe if this is the first size report.
func (sm *streamManager) updateClientSize(agentID string, client *wsClient, cols, rows int) {
	sm.mu.Lock()
	s, ok := sm.streams[agentID]
	sm.mu.Unlock()
	if !ok {
		return
	}

	s.mu.Lock()
	existing := s.sizes[client]
	sizeChanged := existing == nil || existing.cols != cols || existing.rows != rows
	s.sizes[client] = &clientSize{cols: cols, rows: rows, lastSeen: time.Now()}
	needsPipe := !s.piping
	s.mu.Unlock()

	// Only recalculate pane size if this client's dimensions actually changed.
	if sizeChanged {
		sm.resizePane(s)
	}

	// Start the pipe after the first resize so the pane is correctly sized.
	if needsPipe {
		sm.startPipe(s)
	}
}

// resizePane computes the minimum size across all web clients AND attached
// terminal clients, then resizes the tmux pane. This ensures the terminal
// (the golden path) is never cut off by a smaller web/mobile viewport.
func (sm *streamManager) resizePane(s *agentStream) {
	if sm.tmux == nil {
		return
	}

	s.mu.Lock()
	minCols, minRows := 0, 0
	now := time.Now()

	for client, size := range s.sizes {
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
	agentID := s.agentID
	s.mu.Unlock()

	// Include attached tmux terminal clients in the minimum calculation.
	sessionName := "legato-" + agentID
	tmuxPath, err := exec.LookPath("tmux")
	if err == nil {
		out, err := exec.Command(tmuxPath, "list-clients", "-t", sessionName,
			"-F", "#{client_width} #{client_height}").CombinedOutput()
		if err == nil {
			for _, line := range splitLines(string(out)) {
				var cols, rows int
				if _, err := fmt.Sscanf(line, "%d %d", &cols, &rows); err == nil && cols > 0 && rows > 0 {
					if minCols == 0 || cols < minCols {
						minCols = cols
					}
					if minRows == 0 || rows < minRows {
						minRows = rows
					}
				}
			}
		}
	}

	s.mu.Lock()
	if minCols == 0 || minRows == 0 || (minCols == s.appliedCols && minRows == s.appliedRows) {
		s.mu.Unlock()
		return
	}
	s.appliedCols = minCols
	s.appliedRows = minRows
	s.mu.Unlock()

	sm.tmux.SetOption(sessionName, "window-size", "manual")

	if tmuxPath != "" {
		exec.Command(tmuxPath, "resize-window", "-t", sessionName,
			"-x", fmt.Sprintf("%d", minCols),
			"-y", fmt.Sprintf("%d", minRows)).Run()
	}
}

// splitLines splits a string into non-empty trimmed lines.
func splitLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func (sm *streamManager) readLoop(s *agentStream, reader io.Reader) {
	buf := make([]byte, 4096)
	var detectTimer *time.Timer
	const detectDelay = 500 * time.Millisecond

	defer func() {
		if detectTimer != nil {
			detectTimer.Stop()
		}
	}()

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

			// Debounced prompt detection — run after output settles.
			if detectTimer != nil {
				detectTimer.Stop()
			}
			detectTimer = time.AfterFunc(detectDelay, func() {
				sm.detectAndBroadcastPrompt(s)
			})
		}
		if err != nil {
			log.Printf("stream %s ended: %v", s.agentID, err)
			return
		}
	}
}

// detectAndBroadcastPrompt captures the pane and broadcasts prompt state to all clients.
func (sm *streamManager) detectAndBroadcastPrompt(s *agentStream) {
	if sm.tmux == nil {
		return
	}

	sessionName := "legato-" + s.agentID
	output, err := sm.tmux.Capture(sessionName)
	if err != nil {
		return
	}

	state := prompt.Detect(output)
	msg := WSMessage{
		Type:    MsgPromptState,
		AgentID: s.agentID,
		Prompt:  &state,
	}

	s.mu.Lock()
	for c := range s.clients {
		go c.send(msg)
	}
	s.mu.Unlock()
}
