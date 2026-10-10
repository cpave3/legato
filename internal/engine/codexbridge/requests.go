package codexbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// request is something the Codex app server asked its clients to decide: an
// approval, or a question for the person. The terminal UI shows it too;
// whichever client answers first settles it for both.
type request struct {
	requestID string
	// rpcID is the id the app server gave the request on this connection.
	rpcID json.RawMessage
	// itemID is the turn item the request is about.
	itemID string

	tool    string
	command string
	reason  string
	// allow lists the ways the request can be allowed, the first being what
	// a plain allow takes. deny is the decision a denial sends, and
	// denyEndsTurn says that decision stops the turn rather than the action.
	allow        []permissionOption
	deny         json.RawMessage
	denyEndsTurn bool

	// questions are set when the request asks the person something rather
	// than for leave to act: it is then answered, not allowed or denied.
	questions []question
}

// permissionOption is one way a request can be allowed.
type permissionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// decision is what the app server is told when this option is taken.
	decision json.RawMessage
}

// question is one thing Codex asks the person, in the native protocol's own
// shape: the panel draws it without knowing which agent asked.
type question struct {
	// ID names the question in the answers sent back.
	ID          string           `json:"id"`
	Header      string           `json:"header,omitempty"`
	Question    string           `json:"question"`
	Options     []questionOption `json:"options"`
	MultiSelect bool             `json:"multiSelect"`
	// AllowOther lets the person type an answer of their own.
	AllowOther bool `json:"allowOther"`
}

type questionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type snapshotPermission struct {
	RequestID string `json:"requestId"`
	Tool      string `json:"tool"`
	Target    string `json:"target"`
	Command   string `json:"command"`
	Reason    string `json:"reason,omitempty"`
	CWD       string `json:"cwd"`
	// Options are the ways the request can be allowed, when there are several.
	Options []permissionOption `json:"options,omitempty"`
}

type snapshotQuestion struct {
	RequestID string     `json:"requestId"`
	Questions []question `json:"questions"`
}

// permissionTarget is the only target the native panel models: the machine
// the agent runs on.
const permissionTarget = "host"

var (
	errNoSuchRequest = errors.New("no such pending request")
	errInvalidAnswer = errors.New("invalid answer")
)

// options are what the panel offers: several ways to allow, or none when a
// plain allow says it all.
func (r request) options() []permissionOption {
	if len(r.allow) < 2 {
		return nil
	}
	return r.allow
}

// timeoutEvent says the request can no longer be answered in Legato.
func (r request) timeoutEvent() nativeEvent {
	kind := "permission_timeout"
	if r.questions != nil {
		kind = "question_timeout"
	}
	return nativeEvent{"type": kind, "requestId": r.requestID}
}

func (r request) raisedEvent() nativeEvent {
	if r.questions != nil {
		return nativeEvent{"type": "question_request", "requestId": r.requestID, "questions": r.questions}
	}
	event := nativeEvent{
		"type": "permission_request", "requestId": r.requestID, "tool": r.tool,
		"target": permissionTarget, "command": r.command, "reason": r.reason,
	}
	if options := r.options(); options != nil {
		event["options"] = options
	}
	return event
}

func decisionNamed(name string) json.RawMessage {
	encoded, _ := json.Marshal(name)
	return encoded
}

// commandDecisions reads the decisions a command approval offers into the
// ways to allow it and the one a denial sends. An older server names none.
func commandDecisions(available []json.RawMessage) (allow []permissionOption, deny json.RawMessage, denyEndsTurn bool) {
	var canDecline, canCancel bool
	for _, raw := range available {
		var name string
		if json.Unmarshal(raw, &name) == nil {
			switch name {
			case "accept":
				allow = append(allow, permissionOption{ID: name, Label: "Allow once", decision: raw})
			case "acceptForSession":
				allow = append(allow, permissionOption{ID: name, Label: "Allow for this session", decision: raw})
			case "decline":
				canDecline = true
			case "cancel":
				canCancel = true
			}
			continue
		}
		var keyed map[string]json.RawMessage
		if json.Unmarshal(raw, &keyed) != nil || len(keyed) != 1 {
			continue
		}
		for kind, detail := range keyed {
			switch kind {
			case "acceptWithExecpolicyAmendment":
				var amendment struct {
					Prefix []string `json:"execpolicy_amendment"`
				}
				label := "Allow, and don't ask again for commands like this"
				if json.Unmarshal(detail, &amendment) == nil && len(amendment.Prefix) > 0 {
					label = "Allow, and don't ask again for commands that start with `" + strings.Join(amendment.Prefix, " ") + "`"
				}
				allow = append(allow, permissionOption{ID: kind, Label: label, decision: raw})
			case "applyNetworkPolicyAmendment":
				allow = append(allow, permissionOption{ID: kind, Label: "Allow, and remember this network rule", decision: raw})
			}
		}
	}
	if len(allow) == 0 {
		allow = []permissionOption{{ID: "accept", Label: "Allow once", decision: decisionNamed("accept")}}
	}
	// Declining stops the action and lets the turn go on; a request that
	// cannot be declined can only be cancelled, which ends the turn.
	if canCancel && !canDecline {
		return allow, decisionNamed("cancel"), true
	}
	return allow, decisionNamed("decline"), false
}

// describeChanges renders a file change as the text a person approves.
func describeChanges(args map[string]any) string {
	changes, _ := args["changes"].([]map[string]any)
	var out strings.Builder
	for index, change := range changes {
		if index > 0 {
			out.WriteString("\n")
		}
		path, _ := change["path"].(string)
		diff, _ := change["diff"].(string)
		switch change["kind"] {
		case "add":
			out.WriteString("Add " + path + "\n" + prefixLines("+", diff))
		case "delete":
			out.WriteString("Delete " + path + "\n" + prefixLines("-", diff))
		default:
			out.WriteString("Update " + path + "\n" + strings.TrimRight(truncate(diff, 4000), "\n"))
		}
	}
	return out.String()
}

func prefixLines(prefix, text string) string {
	lines := strings.Split(strings.TrimRight(truncate(text, 4000), "\n"), "\n")
	for index, line := range lines {
		lines[index] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// raise puts a request from the app server to the person in Legato. Requests
// the bridge has no card for (an MCP server's form, a wider permission
// profile) and requests about other threads are left to the terminal, which
// shows every one of them.
func (b *Bridge) raise(incoming frame) {
	var about struct {
		ThreadID string `json:"threadId"`
		ItemID   string `json:"itemId"`
	}
	if json.Unmarshal(incoming.Params, &about) != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if about.ThreadID == "" || about.ThreadID != b.session.threadID {
		return
	}
	raised := request{requestID: "request-" + strings.Trim(string(incoming.ID), `"`), rpcID: incoming.ID, itemID: about.ItemID}
	if b.session.pendingIndex(raised.requestID) >= 0 {
		return
	}
	switch incoming.Method {
	case "item/commandExecution/requestApproval":
		var params struct {
			Command            string            `json:"command"`
			Reason             *string           `json:"reason"`
			AvailableDecisions []json.RawMessage `json:"availableDecisions"`
		}
		if json.Unmarshal(incoming.Params, &params) != nil {
			return
		}
		raised.tool = nativeBash
		raised.command = shellCommand(params.Command)
		if params.Reason != nil {
			raised.reason = *params.Reason
		}
		raised.allow, raised.deny, raised.denyEndsTurn = commandDecisions(params.AvailableDecisions)
	case "item/fileChange/requestApproval":
		var params struct {
			Reason *string `json:"reason"`
		}
		_ = json.Unmarshal(incoming.Params, &params)
		raised.tool = nativePatch
		raised.command = "A file change"
		if index, known := b.session.byID[about.ItemID]; known && b.session.items[index].tool != nil {
			if described := describeChanges(b.session.items[index].tool.args); described != "" {
				raised.command = described
			}
		}
		if params.Reason != nil {
			raised.reason = *params.Reason
		}
		raised.allow = []permissionOption{
			{ID: "accept", Label: "Allow once", decision: decisionNamed("accept")},
			{ID: "acceptForSession", Label: "Allow for this session", decision: decisionNamed("acceptForSession")},
		}
		raised.deny = decisionNamed("decline")
	case "item/tool/requestUserInput":
		var params struct {
			Questions []struct {
				ID       string           `json:"id"`
				Header   string           `json:"header"`
				Question string           `json:"question"`
				IsOther  bool             `json:"isOther"`
				Options  []questionOption `json:"options"`
			} `json:"questions"`
		}
		if json.Unmarshal(incoming.Params, &params) != nil || len(params.Questions) == 0 {
			return
		}
		for _, asked := range params.Questions {
			options := asked.Options
			if options == nil {
				options = []questionOption{}
			}
			raised.questions = append(raised.questions, question{
				ID: asked.ID, Header: asked.Header, Question: asked.Question, Options: options,
				// With nothing to pick from, the answer is the person's own words.
				AllowOther: asked.IsOther || len(options) == 0,
			})
		}
	default:
		return
	}
	b.session.pending = append(b.session.pending, raised)
	b.publish([]nativeEvent{raised.raisedEvent()})
}

func (s *session) pendingIndex(requestID string) int {
	for index, pending := range s.pending {
		if pending.requestID == requestID {
			return index
		}
	}
	return -1
}

func (s *session) take(index int) request {
	taken := s.pending[index]
	s.pending = append(s.pending[:index], s.pending[index+1:]...)
	return taken
}

func (s *session) pendingSnapshot() ([]snapshotPermission, []snapshotQuestion) {
	permissions, questions := []snapshotPermission{}, []snapshotQuestion{}
	for _, pending := range s.pending {
		if pending.questions != nil {
			questions = append(questions, snapshotQuestion{RequestID: pending.requestID, Questions: pending.questions})
			continue
		}
		permissions = append(permissions, snapshotPermission{
			RequestID: pending.requestID, Tool: pending.tool, Target: permissionTarget,
			Command: pending.command, Reason: pending.reason, CWD: s.cwd, Options: pending.options(),
		})
	}
	return permissions, questions
}

// expireRequests closes every request nobody answered in Legato.
func (s *session) expireRequests() []nativeEvent {
	var out []nativeEvent
	for _, pending := range s.pending {
		out = append(out, pending.timeoutEvent())
	}
	s.pending = nil
	return out
}

// expireRequestsFor closes the requests open on one item: its work is over,
// so it was answered somewhere.
func (s *session) expireRequestsFor(itemID string) []nativeEvent {
	return s.expireWhere(func(pending request) bool { return pending.itemID == itemID })
}

// expireRequestAnswered closes the request the app server says is settled:
// another client, the terminal, answered it.
func (s *session) expireRequestAnswered(rpcID json.RawMessage) []nativeEvent {
	return s.expireWhere(func(pending request) bool { return string(pending.rpcID) == string(rpcID) })
}

func (s *session) expireWhere(matches func(request) bool) []nativeEvent {
	var out []nativeEvent
	kept := s.pending[:0]
	for _, pending := range s.pending {
		if matches(pending) {
			out = append(out, pending.timeoutEvent())
			continue
		}
		kept = append(kept, pending)
	}
	s.pending = kept
	return out
}

// handlePermission applies Legato's decision on an approval.
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
	index := b.session.pendingIndex(requestID)
	if index < 0 || b.session.pending[index].questions != nil {
		b.mu.Unlock()
		http.Error(w, "no pending permission request "+requestID, http.StatusNotFound)
		return
	}
	pending := b.session.pending[index]
	decided, option, err := pending.decide(body.Decision, body.Option)
	if err != nil {
		b.mu.Unlock()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	b.session.take(index)
	event := nativeEvent{"type": "permission_resolved", "requestId": requestID, "decision": body.Decision}
	if option != "" {
		event["option"] = option
	}
	b.publish([]nativeEvent{event})
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := b.client.respond(ctx, pending.rpcID, map[string]any{"decision": decided}); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if feedback := strings.TrimSpace(body.Feedback); body.Decision == "deny" && feedback != "" {
		go b.deliverFeedback(feedback, pending.denyEndsTurn)
	}
	w.WriteHeader(http.StatusNoContent)
}

// decide turns Legato's decision into the one the app server is told, and
// names the option an allow took when the request listed several.
func (r request) decide(decision, optionID string) (json.RawMessage, string, error) {
	if decision == "deny" {
		if optionID != "" {
			return nil, "", fmt.Errorf("%w: an option is a way to allow, not to deny", errInvalidAnswer)
		}
		return r.deny, "", nil
	}
	if optionID == "" {
		return r.allow[0].decision, "", nil
	}
	for _, option := range r.allow {
		if option.ID == optionID {
			return option.decision, option.ID, nil
		}
	}
	return nil, "", fmt.Errorf("%w: %s is not one of this request's options", errInvalidAnswer, optionID)
}

// feedbackWait bounds how long feedback waits for a cancelled turn to end.
const feedbackWait = 10 * time.Second

// deliverFeedback sends what the person wants done instead of what they
// denied. Where the denial ended the turn, it waits for the turn to be over
// and opens the next one; otherwise it joins the turn still running.
func (b *Bridge) deliverFeedback(feedback string, afterTurn bool) {
	if afterTurn {
		deadline := time.After(feedbackWait)
		for busy := true; busy; {
			b.mu.Lock()
			busy = b.session.busy
			b.mu.Unlock()
			if !busy {
				break
			}
			select {
			case <-b.closed:
				return
			case <-deadline:
				busy = false
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	_ = b.deliver(ctx, feedback)
}

// handleQuestion applies the answers given in Legato, or the person's choice
// not to answer.
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
	index := b.session.pendingIndex(requestID)
	if index < 0 || b.session.pending[index].questions == nil {
		b.mu.Unlock()
		http.Error(w, "no pending question "+requestID, http.StatusNotFound)
		return
	}
	pending := b.session.pending[index]
	answers := map[string]any{}
	event := nativeEvent{"type": "question_resolved", "requestId": requestID, "dismissed": true}
	if !body.Dismissed {
		checked, err := answersFor(pending.questions, body.Answers)
		if err != nil {
			b.mu.Unlock()
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for id, given := range checked {
			answers[id] = map[string]any{"answers": given}
		}
		event = nativeEvent{"type": "question_resolved", "requestId": requestID, "answers": checked}
	}
	b.session.take(index)
	b.publish([]nativeEvent{event})
	b.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := b.client.respond(ctx, pending.rpcID, map[string]any{"answers": answers}); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// answersFor checks the answers given in Legato against the questions asked.
func answersFor(questions []question, given map[string][]string) (map[string][]string, error) {
	known := make(map[string]bool, len(questions))
	out := make(map[string][]string, len(questions))
	for _, asked := range questions {
		known[asked.ID] = true
		var chosen []string
		for _, answer := range given[asked.ID] {
			if answer = strings.TrimSpace(answer); answer != "" {
				chosen = append(chosen, answer)
			}
		}
		switch {
		case len(chosen) == 0:
			return nil, fmt.Errorf("%w: question %s has no answer", errInvalidAnswer, asked.ID)
		case len(chosen) > 1 && !asked.MultiSelect:
			return nil, fmt.Errorf("%w: question %s takes one answer", errInvalidAnswer, asked.ID)
		}
		out[asked.ID] = chosen
	}
	for id := range given {
		if !known[id] {
			return nil, fmt.Errorf("%w: %s is not one of the questions asked", errInvalidAnswer, id)
		}
	}
	return out, nil
}
