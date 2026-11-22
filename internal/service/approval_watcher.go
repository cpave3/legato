package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// Sources of approvals raised by agents themselves.
const (
	ApprovalSourceAgentPermission = "agent_permission"
	ApprovalSourceAgentQuestion   = "agent_question"
)

// How many times in a row an endpoint may refuse its event stream before the
// watcher stops asking until the next reconcile.
const approvalWatchAttempts = 3

// NativeEventStream reads a native agent endpoint: its snapshot and its
// event stream.
type NativeEventStream interface {
	Snapshot(ctx context.Context, endpoint, sessionID string) (*chimera.SessionSnapshot, error)
	Subscribe(ctx context.Context, endpoint, sessionID, since string, onEvent func(chimera.EventEnvelope)) error
}

// AgentRequestResolver answers an agent's permission and question requests.
// AgentService implements it.
type AgentRequestResolver interface {
	ResolvePermission(ctx context.Context, taskID, requestID string, action PermissionAction) error
	AnswerQuestion(ctx context.Context, taskID, requestID string, answer QuestionAnswer) error
}

// ApprovalWatcherOptions configures an ApprovalWatcher.
type ApprovalWatcherOptions struct {
	// Sessions lists agent sessions; the running ones with a native endpoint
	// are watched.
	Sessions func(ctx context.Context) ([]store.AgentSession, error)
	// TaskTitle names a task for the approval's details. Optional.
	TaskTitle func(ctx context.Context, taskID string) string
	Stream    NativeEventStream
	// Interval between reconciles started by Start. Default 15s.
	Interval   time.Duration
	RetryDelay time.Duration
}

// ApprovalWatcher follows the event stream of every running native agent,
// turns its permission and question requests into approvals, and closes
// them when the agent settles them itself. Holding the stream is also what
// makes a Claude Code bridge raise its requests in Legato.
type ApprovalWatcher struct {
	approvals *ApprovalService
	agents    AgentRequestResolver
	opts      ApprovalWatcherOptions

	mu        sync.Mutex
	watches   map[string]*approvalWatch
	listeners []func(agent WatchedAgent, event chimera.EventEnvelope)
	stop      context.CancelFunc
}

// approvalWatch follows one agent's stream. Watches are keyed by
// execution, so the slots of one task (its main agent and its reviewer)
// are followed apart.
type approvalWatch struct {
	WatchedAgent
	endpoint, sessionID string
	cancel              context.CancelFunc
}

// watchKey is the key of a session's watch: its execution, else its slot
// of its task.
func watchKey(session store.AgentSession) string {
	if session.ExecutionID != "" {
		return session.ExecutionID
	}
	return "slot:" + AgentSlotKey(session.TaskID, session.Slot)
}

func NewApprovalWatcher(approvals *ApprovalService, agents AgentRequestResolver, opts ApprovalWatcherOptions) *ApprovalWatcher {
	if opts.Interval <= 0 {
		opts.Interval = 15 * time.Second
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = time.Second
	}
	if opts.Stream == nil {
		opts.Stream = chimeraEventStream{}
	}
	w := &ApprovalWatcher{approvals: approvals, agents: agents, opts: opts, watches: map[string]*approvalWatch{}}
	resolver := agentApprovalResolver{agents: agents}
	approvals.RegisterResolver(ApprovalSourceAgentPermission, resolver)
	approvals.RegisterResolver(ApprovalSourceAgentQuestion, resolver)
	return w
}

// AddListener passes every event of every watched agent to fn, with the
// agent (its task, slot and execution), from the goroutine that reads that
// agent's stream.
func (w *ApprovalWatcher) AddListener(fn func(agent WatchedAgent, event chimera.EventEnvelope)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.listeners = append(w.listeners, fn)
}

// Start reconciles now, when agents start, end or bind an endpoint, and
// every Interval, until the returned function is called.
func (w *ApprovalWatcher) Start(ctx context.Context, bus *events.Bus) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	w.mu.Lock()
	w.stop = cancel
	w.mu.Unlock()
	var subs []<-chan events.Event
	if bus != nil {
		for _, t := range []events.EventType{events.EventAgentStarted, events.EventAgentChimeraDiscovered, events.EventAgentDied} {
			subs = append(subs, bus.Subscribe(t))
		}
	}
	signals := make(chan events.Event, 16)
	for _, sub := range subs {
		go func(sub <-chan events.Event) {
			for {
				select {
				case <-ctx.Done():
					return
				case event, ok := <-sub:
					if !ok {
						return
					}
					select {
					case signals <- event:
					default:
					}
				}
			}
		}(sub)
	}
	go func() {
		ticker := time.NewTicker(w.opts.Interval)
		defer ticker.Stop()
		w.Reconcile(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-signals:
				if event.Type == events.EventAgentDied {
					if payload, ok := event.Payload.(events.AgentDiedPayload); ok && payload.Slot == "" {
						w.AgentEnded(ctx, payload.TaskID)
					}
				}
				w.Reconcile(ctx)
			case <-ticker.C:
				w.Reconcile(ctx)
			}
		}
	}()
	return func() {
		cancel()
		if bus != nil {
			for _, sub := range subs {
				bus.Unsubscribe(sub)
			}
		}
		w.Stop()
	}
}

// Stop ends every watch.
func (w *ApprovalWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stop != nil {
		w.stop()
	}
	for key, watch := range w.watches {
		watch.cancel()
		delete(w.watches, key)
	}
}

// Reconcile watches exactly the running agents that have a native endpoint,
// and expires the pending agent approvals of executions no longer running.
func (w *ApprovalWatcher) Reconcile(ctx context.Context) {
	sessions, err := w.opts.Sessions(ctx)
	if err != nil {
		log.Printf("approvals: listing agents: %v", err)
		return
	}
	wanted := map[string]store.AgentSession{}
	running := map[string]bool{}
	for _, session := range sessions {
		if session.Status != "running" {
			continue
		}
		running[session.ExecutionID] = true
		if session.ChimeraEndpoint == nil || session.ChimeraSessionID == nil || *session.ChimeraEndpoint == "" {
			continue
		}
		wanted[watchKey(session)] = session
	}

	w.mu.Lock()
	for key, watch := range w.watches {
		session, ok := wanted[key]
		if !ok || watch.endpoint != *session.ChimeraEndpoint || watch.sessionID != *session.ChimeraSessionID || watch.WatchedAgent != watchedAgentOf(session) {
			watch.cancel()
			delete(w.watches, key)
		}
	}
	for key, session := range wanted {
		if _, ok := w.watches[key]; ok {
			continue
		}
		watchCtx, cancel := context.WithCancel(context.Background())
		watch := &approvalWatch{
			WatchedAgent: watchedAgentOf(session),
			endpoint:     *session.ChimeraEndpoint, sessionID: *session.ChimeraSessionID, cancel: cancel,
		}
		w.watches[key] = watch
		go w.follow(watchCtx, watch)
	}
	w.mu.Unlock()

	for _, source := range []string{ApprovalSourceAgentPermission, ApprovalSourceAgentQuestion} {
		pending, err := w.approvals.List(ctx, store.ApprovalFilter{Status: store.ApprovalPending, Source: source})
		if err != nil {
			continue
		}
		for _, approval := range pending {
			if !running[approval.ExecutionID] {
				_ = w.approvals.closeQuietly(ctx, approval.ID, store.ApprovalExpired, "the agent ended")
			}
		}
	}
}

// AgentEnded expires the pending approvals of an agent that ended.
func (w *ApprovalWatcher) AgentEnded(ctx context.Context, taskID string) {
	if err := w.approvals.ExpireTask(ctx, taskID); err != nil {
		log.Printf("approvals: expiring requests of %s: %v", taskID, err)
	}
}

func (w *ApprovalWatcher) follow(ctx context.Context, watch *approvalWatch) {
	failures := 0
	since := ""
	for ctx.Err() == nil {
		w.readSnapshot(ctx, watch)
		received := false
		err := w.opts.Stream.Subscribe(ctx, watch.endpoint, watch.sessionID, since, func(event chimera.EventEnvelope) {
			received = true
			if event.EventID != "" {
				since = event.EventID
			}
			w.handle(ctx, watch, event)
			w.mu.Lock()
			listeners := append([]func(WatchedAgent, chimera.EventEnvelope){}, w.listeners...)
			w.mu.Unlock()
			for _, listener := range listeners {
				listener(watch.WatchedAgent, event)
			}
		})
		if ctx.Err() != nil {
			return
		}
		if received {
			failures = 0
		} else if failures++; failures >= approvalWatchAttempts {
			if err != nil {
				log.Printf("approvals: following %s: %v", watch.AgentID(), err)
			}
			w.forget(watch)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.opts.RetryDelay):
		}
	}
}

func (w *ApprovalWatcher) forget(watch *approvalWatch) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for key, held := range w.watches {
		if held == watch {
			watch.cancel()
			delete(w.watches, key)
		}
	}
}

// readSnapshot submits the session's pending requests and withdraws the
// approvals of requests the session no longer has.
func (w *ApprovalWatcher) readSnapshot(ctx context.Context, watch *approvalWatch) {
	snapshot, err := w.opts.Stream.Snapshot(ctx, watch.endpoint, watch.sessionID)
	if err != nil || snapshot == nil {
		return
	}
	if snapshot.PendingPermissions != nil {
		keep := map[string]bool{}
		for _, p := range *snapshot.PendingPermissions {
			request := permissionRequest{RequestID: p.RequestID, Tool: p.Tool, Target: p.Target, Command: p.Command, Reason: p.Reason, CWD: p.CWD, Plan: p.Plan, Options: p.Options}
			keep[w.sourceKey(watch, p.RequestID)] = true
			w.submitPermission(ctx, watch, request)
		}
		_ = w.approvals.WithdrawExecution(ctx, ApprovalSourceAgentPermission, watch.ExecutionID, keep, "settled at the agent")
	}
	if len(snapshot.PendingQuestions) > 0 && string(snapshot.PendingQuestions) != "null" {
		var questions []questionRequest
		if json.Unmarshal(snapshot.PendingQuestions, &questions) == nil {
			keep := map[string]bool{}
			for _, q := range questions {
				keep[w.sourceKey(watch, q.RequestID)] = true
				w.submitQuestion(ctx, watch, q)
			}
			_ = w.approvals.WithdrawExecution(ctx, ApprovalSourceAgentQuestion, watch.ExecutionID, keep, "settled at the agent")
		}
	}
}

type permissionRequest struct {
	RequestID string          `json:"requestId"`
	Tool      string          `json:"tool"`
	Target    string          `json:"target"`
	Command   string          `json:"command"`
	Reason    string          `json:"reason,omitempty"`
	CWD       string          `json:"cwd,omitempty"`
	Plan      string          `json:"plan,omitempty"`
	Options   json.RawMessage `json:"options,omitempty"`
}

type questionRequest struct {
	RequestID string            `json:"requestId"`
	Questions []json.RawMessage `json:"questions"`
}

func (w *ApprovalWatcher) handle(ctx context.Context, watch *approvalWatch, event chimera.EventEnvelope) {
	switch event.Type {
	case "permission_request":
		var request permissionRequest
		if json.Unmarshal(event.Data, &request) == nil && request.RequestID != "" {
			w.submitPermission(ctx, watch, request)
		}
	case "question_request":
		var request questionRequest
		if json.Unmarshal(event.Data, &request) == nil && request.RequestID != "" {
			w.submitQuestion(ctx, watch, request)
		}
	case "permission_resolved", "permission_timeout":
		w.withdraw(ctx, watch, ApprovalSourceAgentPermission, event)
	case "question_resolved", "question_timeout":
		w.withdraw(ctx, watch, ApprovalSourceAgentQuestion, event)
	case "run_finished":
		for _, source := range []string{ApprovalSourceAgentPermission, ApprovalSourceAgentQuestion} {
			_ = w.approvals.WithdrawExecution(ctx, source, watch.ExecutionID, nil, "the agent's turn ended")
		}
	}
}

func (w *ApprovalWatcher) withdraw(ctx context.Context, watch *approvalWatch, source string, event chimera.EventEnvelope) {
	var data struct {
		RequestID string `json:"requestId"`
	}
	if json.Unmarshal(event.Data, &data) == nil && data.RequestID != "" {
		_ = w.approvals.Withdraw(ctx, source, w.sourceKey(watch, data.RequestID), "settled at the agent")
	}
}

func (w *ApprovalWatcher) sourceKey(watch *approvalWatch, requestID string) string {
	return watch.ExecutionID + "/" + watch.sessionID + "/" + requestID
}

// agentRequestDetails is what an agent approval's details hold.
type agentRequestDetails struct {
	RequestID string `json:"request_id"`
	// Slot is the task's slot of the agent that asked, "" for main.
	Slot      string            `json:"slot,omitempty"`
	AgentKind string            `json:"agent_kind,omitempty"`
	TaskTitle string            `json:"task_title,omitempty"`
	Tool      string            `json:"tool,omitempty"`
	Target    string            `json:"target,omitempty"`
	Command   string            `json:"command,omitempty"`
	Reason    string            `json:"reason,omitempty"`
	CWD       string            `json:"cwd,omitempty"`
	Plan      string            `json:"plan,omitempty"`
	Questions []json.RawMessage `json:"questions,omitempty"`
}

func (w *ApprovalWatcher) taskTitle(ctx context.Context, taskID string) string {
	if w.opts.TaskTitle == nil {
		return ""
	}
	return w.opts.TaskTitle(ctx, taskID)
}

func (w *ApprovalWatcher) submitPermission(ctx context.Context, watch *approvalWatch, request permissionRequest) {
	var offered []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	_ = json.Unmarshal(request.Options, &offered)
	options := make([]ApprovalOption, 0, len(offered)+1)
	for _, option := range offered {
		if option.ID != "" {
			options = append(options, ApprovalOption{ID: option.ID, Label: option.Label, Kind: ApprovalAllow})
		}
	}
	if len(options) == 0 {
		options = append(options, ApprovalOption{ID: "allow", Label: "Allow", Kind: ApprovalAllow})
	}
	options = append(options, ApprovalOption{ID: "deny", Label: "Deny", Kind: ApprovalDeny})

	title := "Use " + request.Tool
	switch {
	case request.Plan != "":
		title = "Approve a plan"
	case strings.EqualFold(request.Tool, "bash") || strings.EqualFold(request.Tool, "shell"):
		title = "Run a command"
	case request.Tool == "":
		title = "Allow an action"
	}
	_, _, err := w.approvals.Submit(ctx, ApprovalInput{
		Source: ApprovalSourceAgentPermission, SourceKey: w.sourceKey(watch, request.RequestID),
		TaskID: watch.TaskID, ExecutionID: watch.ExecutionID,
		Title: title, Summary: clip(request.Command, 500), Blocking: true,
		Details: agentRequestDetails{
			RequestID: request.RequestID, Slot: namedSlot(watch.Slot), AgentKind: watch.AgentKind, TaskTitle: w.taskTitle(ctx, watch.TaskID),
			Tool: request.Tool, Target: request.Target, Command: request.Command, Reason: request.Reason, CWD: request.CWD, Plan: request.Plan,
		},
		Options: options,
	})
	if err != nil {
		log.Printf("approvals: %s request %s: %v", watch.TaskID, request.RequestID, err)
	}
}

func (w *ApprovalWatcher) submitQuestion(ctx context.Context, watch *approvalWatch, request questionRequest) {
	title := "Answer a question"
	if len(request.Questions) > 0 {
		var first struct {
			Question string `json:"question"`
		}
		if json.Unmarshal(request.Questions[0], &first) == nil && first.Question != "" {
			title = clip(first.Question, 200)
		}
	}
	summary := ""
	if len(request.Questions) > 1 {
		summary = fmt.Sprintf("%d questions", len(request.Questions))
	}
	_, _, err := w.approvals.Submit(ctx, ApprovalInput{
		Source: ApprovalSourceAgentQuestion, SourceKey: w.sourceKey(watch, request.RequestID),
		TaskID: watch.TaskID, ExecutionID: watch.ExecutionID,
		Title: title, Summary: summary, Blocking: true,
		Details: agentRequestDetails{
			RequestID: request.RequestID, Slot: namedSlot(watch.Slot), AgentKind: watch.AgentKind, TaskTitle: w.taskTitle(ctx, watch.TaskID), Questions: request.Questions,
		},
	})
	if err != nil {
		log.Printf("approvals: %s question %s: %v", watch.TaskID, request.RequestID, err)
	}
}

// namedSlot is slot when it is a named one, "" for the main slot.
func namedSlot(slot string) string {
	if IsNamedSlot(slot) {
		return slot
	}
	return ""
}

func clip(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// agentApprovalResolver applies decisions to the agent that asked.
type agentApprovalResolver struct {
	agents AgentRequestResolver
}

// dismissedFeedback is what an agent reads when its request was dismissed.
const dismissedFeedback = "Dismissed in Legato"

func (r agentApprovalResolver) Validate(approval Approval, decision ApprovalDecision) error {
	if approval.Source == ApprovalSourceAgentPermission && decision.Decision == ApprovalAnswer {
		return fmt.Errorf("a permission request is allowed or denied, not answered")
	}
	if approval.Source != ApprovalSourceAgentQuestion || decision.Decision != ApprovalAnswer {
		return nil
	}
	var details agentRequestDetails
	_ = json.Unmarshal(approval.Details, &details)
	asked := map[string]bool{}
	for _, raw := range details.Questions {
		var q struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &q) == nil {
			asked[q.ID] = true
		}
	}
	for id := range decision.Answers {
		if !asked[id] {
			return fmt.Errorf("the request asks no question %q", id)
		}
	}
	return nil
}

func (r agentApprovalResolver) Resolve(ctx context.Context, approval Approval, decision ApprovalDecision) error {
	var details agentRequestDetails
	if err := json.Unmarshal(approval.Details, &details); err != nil || details.RequestID == "" {
		return fmt.Errorf("approval %s does not name the agent's request", approval.ID)
	}
	if IsNamedSlot(details.Slot) {
		ctx = WithAgentSlot(ctx, details.Slot)
	}
	if approval.Source == ApprovalSourceAgentQuestion {
		switch decision.Decision {
		case ApprovalAnswer:
			return r.agents.AnswerQuestion(ctx, approval.TaskID, details.RequestID, QuestionAnswer{Answers: decision.Answers})
		default:
			return r.agents.AnswerQuestion(ctx, approval.TaskID, details.RequestID, QuestionAnswer{Dismissed: true})
		}
	}
	switch decision.Decision {
	case ApprovalAllow:
		option := decision.Option
		if option == "allow" {
			option = "" // the one way to allow; the agent offered no options
		}
		return r.agents.ResolvePermission(ctx, approval.TaskID, details.RequestID, PermissionAction{Decision: "allow", Option: option})
	case ApprovalDismiss:
		feedback := dismissedFeedback
		if decision.Feedback != "" {
			feedback += ": " + decision.Feedback
		}
		return r.agents.ResolvePermission(ctx, approval.TaskID, details.RequestID, PermissionAction{Decision: "deny", Feedback: feedback})
	default:
		return r.agents.ResolvePermission(ctx, approval.TaskID, details.RequestID, PermissionAction{Decision: "deny", Feedback: decision.Feedback})
	}
}

// chimeraEventStream reads endpoints with the native protocol client.
type chimeraEventStream struct{}

func (chimeraEventStream) Snapshot(ctx context.Context, endpoint, sessionID string) (*chimera.SessionSnapshot, error) {
	return chimera.NewClient(endpoint).GetSession(ctx, sessionID)
}

func (chimeraEventStream) Subscribe(ctx context.Context, endpoint, sessionID, since string, onEvent func(chimera.EventEnvelope)) error {
	return chimera.NewClient(endpoint).Subscribe(ctx, sessionID, since, onEvent)
}
