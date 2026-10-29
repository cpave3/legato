package claudebridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// send writes one command for the mod as a JSON line. The mod reads the
// bridge's stdout piece by piece, so a command is always a whole line.
func (b *Bridge) send(command map[string]any) error {
	line, err := json.Marshal(command)
	if err != nil {
		return err
	}
	b.commandMu.Lock()
	defer b.commandMu.Unlock()
	if _, err := b.commands.Write(append(line, '\n')); err != nil {
		b.disconnect()
		return fmt.Errorf("writing command for the Claude Code mod: %w", err)
	}
	return nil
}

// Ready tells the mod where to post its events. It is the first command a
// bridge writes, once it is listening.
func (b *Bridge) Ready(endpointURL string) error {
	return b.send(map[string]any{"type": "ready", "url": endpointURL})
}

func (b *Bridge) sendOrFail(w http.ResponseWriter, command map[string]any, status int) {
	if err := b.send(command); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(status)
}

// handleMessage delivers a message from Legato. To an idle session it is a
// prompt of its own. During a turn it is queued into that turn, which reads
// it at its next step: Claude Code can fold a message in, so the bridge never
// answers busy the way a Chimera session does. An explicit append adds
// context for the next request without starting a turn.
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
	kind := "submit"
	if r.URL.Query().Get("append") == "true" {
		kind = "append"
	} else {
		b.mu.Lock()
		if b.session.busy {
			kind = "queue"
		}
		b.mu.Unlock()
	}
	if err := b.send(map[string]any{"type": kind, "text": body.Content}); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if kind == "append" {
		b.mu.Lock()
		b.session.showAppended(body.Content)
		b.mu.Unlock()
	}
	w.WriteHeader(http.StatusAccepted)
}

func (b *Bridge) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	b.sendOrFail(w, map[string]any{"type": "interrupt"}, http.StatusNoContent)
}

// permissionAnswer is what the mod's tool.check hook receives: allow or deny
// from Legato, or ask to hand the prompt back to Claude Code's own dialog.
type permissionAnswer struct {
	Decision string `json:"decision"`
	Feedback string `json:"feedback,omitempty"`
	// Message, when set, is the text the model reads for a deny.
	Message string `json:"message,omitempty"`
	// Answers are a question's answers, which the mod hands back to Claude
	// Code inside the tool's own input.
	Answers map[string]string `json:"answers,omitempty"`
	// ReturnInput has the mod hand the tool's input back with the allow.
	// Claude Code accepts a plan no other way.
	ReturnInput bool `json:"returnInput,omitempty"`
	// UpdatedPermissions are permission changes the allow applies, in the
	// engine's own form.
	UpdatedPermissions []json.RawMessage `json:"updatedPermissions,omitempty"`
}

var askInTerminal = permissionAnswer{Decision: "ask"}

// handleModPermission holds the mod's request open until a person decides.
//
// The mod waits here rather than in its hook because a hook's own waiting is
// bounded to seconds, while time inside an engine call is not. With no native
// viewer attached there is nobody to answer in Legato, so the prompt goes
// straight back to the terminal.
func (b *Bridge) handleModPermission(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RequestID string         `json:"requestId"`
		Tool      string         `json:"tool"`
		Input     map[string]any `json:"input"`
		Reason    string         `json:"reason"`
		// Suggestions are the permission changes the engine would make for
		// its own "don't ask again".
		Suggestions []json.RawMessage `json:"suggestions"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&body); err != nil || body.RequestID == "" {
		http.Error(w, "requestId, tool and input are required", http.StatusBadRequest)
		return
	}
	b.mu.Lock()
	// The mod asks again for a request whose call timed out: wait on as before.
	if answer, ok := b.parked[body.RequestID]; ok {
		delete(b.parked, body.RequestID)
		b.mu.Unlock()
		writeJSON(w, http.StatusOK, answer)
		return
	}
	if waiter := b.waiters[body.RequestID]; waiter != nil {
		if timer := b.detached[body.RequestID]; timer != nil {
			timer.Stop()
			delete(b.detached, body.RequestID)
		}
		b.mu.Unlock()
		b.awaitPermission(w, r, body.RequestID, waiter)
		return
	}
	if len(b.subscribers) == 0 {
		b.mu.Unlock()
		writeJSON(w, http.StatusOK, askInTerminal)
		return
	}
	events := b.session.requestPermission(body.RequestID, body.Tool, body.Input, body.Reason, body.Suggestions)
	if events == nil {
		b.mu.Unlock()
		writeJSON(w, http.StatusOK, askInTerminal)
		return
	}
	waiter := make(chan permissionAnswer, 1)
	b.waiters[body.RequestID] = waiter
	b.publish(events)
	b.mu.Unlock()
	b.awaitPermission(w, r, body.RequestID, waiter)
}

// awaitPermission holds the mod's call until a decision comes. When the call
// ends first, the request stays open for b.grace, for the mod to ask again.
func (b *Bridge) awaitPermission(w http.ResponseWriter, r *http.Request, requestID string, waiter chan permissionAnswer) {
	hold := time.NewTimer(b.holdFor)
	defer hold.Stop()
	select {
	case answer := <-waiter:
		writeJSON(w, http.StatusOK, answer)
	case <-b.closed:
		writeJSON(w, http.StatusOK, askInTerminal)
	case <-hold.C:
		// Answer before Claude Code ends the call; the mod asks again.
		b.mu.Lock()
		b.detach(requestID, waiter)
		b.mu.Unlock()
		writeJSON(w, http.StatusOK, permissionAnswer{Decision: "wait"})
	case <-r.Context().Done():
		b.mu.Lock()
		defer b.mu.Unlock()
		b.detach(requestID, waiter)
	}
}

// detach keeps a held request open for b.grace after its call ended, for the
// mod to ask again. The caller holds b.mu.
func (b *Bridge) detach(requestID string, waiter chan permissionAnswer) {
	if b.waiters[requestID] != waiter {
		return
	}
	b.detached[requestID] = time.AfterFunc(b.grace, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.waiters[requestID] != waiter || b.detached[requestID] == nil {
			return
		}
		// The mod did not come back: the turn was interrupted and the
		// tool call dropped.
		delete(b.detached, requestID)
		delete(b.waiters, requestID)
		b.publish(b.session.expirePermission(requestID))
	})
}

// deliver sends a decision to the mod's held call, or keeps it for the mod's
// next call when the held one has ended. The caller holds b.mu.
func (b *Bridge) deliver(requestID string, waiter chan permissionAnswer, answer permissionAnswer) {
	delete(b.waiters, requestID)
	if timer := b.detached[requestID]; timer != nil {
		timer.Stop()
		delete(b.detached, requestID)
		b.parked[requestID] = answer
		time.AfterFunc(b.grace, func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			delete(b.parked, requestID)
		})
		return
	}
	waiter <- answer
}

// handlePermission applies Legato's decision and releases the waiting mod.
func (b *Bridge) handlePermission(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Decision string `json:"decision"`
		Feedback string `json:"feedback"`
		// Option names which of the request's options an allow takes.
		Option string `json:"option"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if body.Decision != "allow" && body.Decision != "deny" {
		http.Error(w, "decision must be allow or deny", http.StatusBadRequest)
		return
	}
	requestID := r.PathValue("requestId")
	b.mu.Lock()
	defer b.mu.Unlock()
	events, request, option, err := b.session.resolvePermission(requestID, body.Decision, body.Option)
	switch {
	case errors.Is(err, errNoSuchRequest):
		http.Error(w, "no pending permission request "+requestID, http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.publish(events)
	if waiter := b.waiters[requestID]; waiter != nil {
		answer := permissionAnswer{Decision: body.Decision, Feedback: body.Feedback}
		if body.Decision == "deny" {
			answer.Message = denialMessage(body.Feedback)
		} else {
			answer.ReturnInput = request.plan != ""
			answer.UpdatedPermissions = option.updates
		}
		b.deliver(requestID, waiter, answer)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleQuestion applies the answers given in Legato, or the person's choice
// not to answer, and releases the waiting mod.
func (b *Bridge) handleQuestion(w http.ResponseWriter, r *http.Request) {
	var body struct {
		// Answers holds, per question ID, the options chosen and any answer
		// the person typed.
		Answers   map[string][]string `json:"answers"`
		Dismissed bool                `json:"dismissed"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	requestID := r.PathValue("requestId")
	b.mu.Lock()
	defer b.mu.Unlock()
	var (
		events []nativeEvent
		answer permissionAnswer
		err    error
	)
	if body.Dismissed {
		events, err = b.session.dismissQuestion(requestID)
		answer = permissionAnswer{Decision: "deny", Message: questionDismissedMessage}
	} else {
		var answers map[string]string
		events, answers, err = b.session.answerQuestion(requestID, body.Answers)
		answer = permissionAnswer{Decision: "allow", Answers: answers}
	}
	switch {
	case errors.Is(err, errNoSuchRequest):
		http.Error(w, "no pending question "+requestID, http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.publish(events)
	if waiter := b.waiters[requestID]; waiter != nil {
		b.deliver(requestID, waiter, answer)
	}
	w.WriteHeader(http.StatusNoContent)
}

// releaseSettledWaiters hands back to the terminal every held request the
// session no longer lists as pending (its turn ended, the conversation was
// cleared). The caller holds b.mu.
func (b *Bridge) releaseSettledWaiters() {
	for requestID, waiter := range b.waiters {
		if b.session.pendingIndex(requestID) < 0 {
			b.deliver(requestID, waiter, askInTerminal)
		}
	}
}

// notifyViewers tells the mod how many native viewers are attached, so it
// knows whether a person is following the session from Legato.
func (b *Bridge) notifyViewers(count int) {
	_ = b.send(map[string]any{"type": "viewers", "count": count})
}
