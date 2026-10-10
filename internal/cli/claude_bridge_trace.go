package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// bridgeTrace records the traffic between the Claude Code mod and its bridge.
// A nil trace records nothing, so callers use it unconditionally.
type bridgeTrace struct {
	mu   sync.Mutex
	file *os.File
}

func openBridgeTrace(path string) (*bridgeTrace, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening the bridge trace: %w", err)
	}
	return &bridgeTrace{file: file}, nil
}

func (t *bridgeTrace) Close() {
	if t != nil {
		t.file.Close()
	}
}

func (t *bridgeTrace) record(direction string, payload []byte) {
	if t == nil {
		return
	}
	entry := map[string]any{"at": time.Now().UTC().Format(time.RFC3339Nano), "direction": direction}
	if json.Valid(payload) {
		entry["payload"] = json.RawMessage(payload)
	} else {
		entry["payload"] = string(payload)
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, _ = t.file.Write(append(line, '\n'))
}

// commands returns a writer that records each command before it reaches the mod.
func (t *bridgeTrace) commands(out io.Writer) io.Writer {
	if t == nil {
		return out
	}
	return writerFunc(func(p []byte) (int, error) {
		t.record("command", bytes.TrimSpace(p))
		return out.Write(p)
	})
}

// handler records what the mod posts; Legato's own requests are not traced.
func (t *bridgeTrace) handler(next http.Handler) http.Handler {
	if t == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/mod/") {
			body, err := io.ReadAll(r.Body)
			if err == nil {
				t.record("mod", body)
				r.Body = io.NopCloser(bytes.NewReader(body))
			}
		}
		next.ServeHTTP(w, r)
	})
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
