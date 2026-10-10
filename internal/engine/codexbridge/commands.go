package codexbridge

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// textInput is one piece of text in a turn's input.
func textInput(text string) map[string]any {
	return map[string]any{"type": "text", "text": text, "text_elements": []any{}}
}

// handleMessage delivers a message from Legato. To an idle thread it starts
// a turn. During a turn it joins that turn, which reads it at its next step:
// Codex can fold a message in, so the bridge never answers busy the way a
// Chimera session does. An explicit append holds context for the next message
// to carry, without starting a turn.
func (b *Bridge) handleMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if body.Content == "" {
		http.Error(w, "content required", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("append") == "true" {
		b.mu.Lock()
		b.session.appended = append(b.session.appended, body.Content)
		b.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		return
	}
	b.mu.Lock()
	if b.session.threadID == "" {
		// The terminal has not opened a thread yet: it is still starting, or
		// is showing a prompt of its own. The message is delivered when it
		// has one, however long the person takes over that prompt.
		b.publish(b.session.hold(body.Content))
		b.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		return
	}
	b.mu.Unlock()
	if err := b.deliver(r.Context(), body.Content); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// deliverHeld sends what was sent before there was a thread, as one turn.
func (b *Bridge) deliverHeld() {
	b.mu.Lock()
	held, shown := b.session.held, b.session.heldItems
	b.session.held, b.session.heldItems = nil, nil
	b.mu.Unlock()
	if len(held) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := b.send(ctx, held, shown[0]); err != nil {
		b.mu.Lock()
		b.publish([]nativeEvent{{"type": "error", "error": "A message sent before Codex was ready could not be delivered: " + err.Error()}})
		b.mu.Unlock()
	}
}

// deliver puts text to the thread as the person's message: into the turn in
// progress when there is one, as a new turn otherwise.
func (b *Bridge) deliver(ctx context.Context, text string) error {
	return b.send(ctx, []string{text}, -1)
}

// send puts messages to the thread as one input. shownAs is the item that
// already shows them, or -1 when Codex's report of the turn is what shows them.
func (b *Bridge) send(ctx context.Context, messages []string, shownAs int) error {
	b.mu.Lock()
	threadID, turnID, joined := b.session.threadID, b.session.turnID, b.session.joined
	busy := b.session.busy
	input := make([]any, 0, len(b.session.appended)+len(messages))
	// Codex reports a turn's input back as one message: everything sent,
	// run together. That is recognised, so the person is shown their own
	// words once, without the context that travelled with them.
	var reported strings.Builder
	for _, attached := range b.session.appended {
		input = append(input, textInput(attached))
		reported.WriteString(attached)
	}
	for _, message := range messages {
		input = append(input, textInput(message))
		reported.WriteString(message)
	}
	overrides := b.session.turnOverrides()
	if threadID != "" {
		b.session.echoes[reported.String()] = echo{display: strings.Join(messages, "\n\n"), shownAs: shownAs}
	}
	b.mu.Unlock()
	if threadID == "" {
		return errNoThread
	}

	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	steered := false
	if busy && turnID != "" {
		steer := map[string]any{"threadId": threadID, "expectedTurnId": turnID, "input": input}
		// A refusal means the turn ended meanwhile: the message opens the next.
		steered = b.client.call(ctx, "turn/steer", steer, nil) == nil
	}
	if !steered {
		start := map[string]any{"threadId": threadID, "input": input}
		for name, value := range overrides {
			start[name] = value
		}
		if err := b.client.call(ctx, "turn/start", start, nil); err != nil {
			b.mu.Lock()
			delete(b.session.echoes, reported.String())
			b.mu.Unlock()
			return err
		}
	}
	b.mu.Lock()
	// What was attached has been sent, unless the conversation changed.
	if b.session.threadID == threadID {
		b.session.appended = nil
	}
	b.mu.Unlock()
	if !joined {
		// The turn gives a new thread the history it needs to be joined.
		go b.join(threadID, rejoinAttempts)
	}
	return nil
}

// turnOverrides are the model and effort chosen in Legato, as a turn/start
// carries them. Codex keeps them for the turns that follow.
func (s *session) turnOverrides() map[string]any {
	overrides := map[string]any{}
	if s.modelOverride != "" && s.modelOverride != s.model {
		overrides["model"] = s.modelOverride
	}
	if s.effortOverride != "" && s.effortOverride != s.effort {
		overrides["effort"] = s.effortOverride
	}
	return overrides
}

func (b *Bridge) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	threadID, turnID := b.session.threadID, b.session.turnID
	b.mu.Unlock()
	if threadID == "" || turnID == "" {
		// Nothing the bridge knows of is running.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := b.client.call(ctx, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": turnID}, nil); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
