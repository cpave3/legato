package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Notifier sends push notifications. Implementations are provider-specific
// (ntfy, Pushover, etc.). The interface is intentionally small so callers
// don't depend on provider details.
type Notifier interface {
	// Notify sends a notification with the given title and message.
	// Implementations must be safe for concurrent use.
	Notify(title, message string) error
	// Configured returns true when the notifier has enough config to send.
	Configured() bool
	// CanNotify checks whether enough time has passed since the last
	// notification for the given task ID. Must be called before Notify
	// to avoid rate-limit violations.
	CanNotify(taskID string) bool
}

// ntfyNotifier publishes to an ntfy.sh topic via HTTP POST.
type ntfyNotifier struct {
	url    string
	topic  string
	token  string
	client *http.Client

	// per-agent rate limiting: last notification time by task ID.
	lastMu      sync.Mutex
	lastSent    map[string]time.Time
	minInterval time.Duration
}

// NewNtfyNotifier creates a notifier that posts to ntfy. Empty topic
// returns a no-op notifier (Configured() == false).
func NewNtfyNotifier(url, topic, token string) Notifier {
	if url == "" || topic == "" {
		return &noopNotifier{}
	}
	return &ntfyNotifier{
		url:         url,
		topic:       topic,
		token:       token,
		client:      &http.Client{Timeout: 5 * time.Second},
		lastSent:    make(map[string]time.Time),
		minInterval: 5 * time.Minute,
	}
}

func (n *ntfyNotifier) Configured() bool { return true }

// Notify sends the notification synchronously.
// Callers that need fire-and-forget should wrap the call in a goroutine.
func (n *ntfyNotifier) Notify(title, message string) error {
	n.doNotify(title, message)
	return nil
}

func (n *ntfyNotifier) doNotify(title, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	endpoint := fmt.Sprintf("%s/%s", n.url, n.topic)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte(message)))
	if err != nil {
		return
	}
	req.Header.Set("Title", title)
	req.Header.Set("Priority", "default")
	if n.token != "" {
		req.Header.Set("Authorization", "Bearer "+n.token)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
}

// CanNotify checks whether enough time has passed since the last
// notification for the given task ID.
func (n *ntfyNotifier) CanNotify(taskID string) bool {
	n.lastMu.Lock()
	defer n.lastMu.Unlock()
	if last, ok := n.lastSent[taskID]; ok && time.Since(last) < n.minInterval {
		return false
	}
	n.lastSent[taskID] = time.Now()
	return true
}

type noopNotifier struct{}

func (n *noopNotifier) Notify(_, _ string) error { return nil }
func (n *noopNotifier) Configured() bool         { return false }
func (n *noopNotifier) CanNotify(_ string) bool  { return false }
