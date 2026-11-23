package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/store"
)

// AgentCompactStepType is the step that compacts a task's agent with its
// tool's own compaction (LEG-536).
const AgentCompactStepType = "agent.compact"

// CompactStepLimit is how long an agent.compact step waits, from its start,
// for the agent's turn to end and its compaction to finish.
const CompactStepLimit = 15 * time.Minute

// CompactCheckEvery is how often the waiting agent.compact steps ask their
// working agents again and look at their deadlines.
const CompactCheckEvery = 5 * time.Second

// AgentCompactExecutor is agent.compact. It asks the task's agent to compact
// when the step starts, and waits (WorkflowStepWaiting): an agent at work is
// asked again by WorkflowService.CheckCompactions until its turn ends, and
// the agent's compaction events, which WorkflowService.ObserveCompaction is
// told, end the step.
//
// A step is ended only by the process whose ask the agent took, and only by
// an end that came after that ask. The agent takes an ask only while no
// compaction runs, so that end is the asked compaction's: another
// process's copy of an earlier compaction's end, however late, never ends
// a later step, and the start of the compaction need not have been heard.
// Asks and events are put in one order by a counter, in the goroutine that
// reads the agent's stream, so an end that comes before the ask returns or
// the step is stored as waiting still ends it.
type AgentCompactExecutor struct {
	compactor AgentCompactor
	now       func() time.Time

	mu  sync.Mutex
	seq uint64
	// attempts are the attempts this process asked for. Another process,
	// or this one after a restart, does not know of an attempt: it asks
	// again, and the agent refuses while it compacts.
	attempts map[string]*compactAttempt
	// ends are the last compaction end each task's agent reported.
	ends map[string]*compactEnd
}

// compactAttempt is what this process knows of a compact step's attempt.
type compactAttempt struct {
	// asked is set when the agent took an ask of this process, made at
	// askedAt; working while it refused the last one at work.
	asked, working bool
	askedAt        uint64
}

// compactEnd is an agent's compaction_finished or compaction_failed.
type compactEnd struct {
	at            uint64
	failed        bool
	reason        string
	before, after *int
}

// NewAgentCompactExecutor compacts agents through compactor.
func NewAgentCompactExecutor(compactor AgentCompactor) *AgentCompactExecutor {
	return &AgentCompactExecutor{compactor: compactor, now: time.Now, attempts: map[string]*compactAttempt{}, ends: map[string]*compactEnd{}}
}

// SetClock replaces the clock the step's deadline is read by.
func (e *AgentCompactExecutor) SetClock(now func() time.Time) { e.now = now }

func (e *AgentCompactExecutor) Type() string { return AgentCompactStepType }

func (e *AgentCompactExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	target, err := compactTarget(step.With)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if err := e.ask(ctx, step.AttemptID, target); err != nil {
		e.forget(step.AttemptID)
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
}

func (e *AgentCompactExecutor) Cancel(_ context.Context, attempt store.WorkflowStepAttempt) error {
	e.forget(attempt.ID)
	return nil
}

// compactWhat is the agent a step compacts, and what its summary keeps.
type compactWhat struct{ task, instructions string }

func compactTarget(with map[string]any) (compactWhat, error) {
	task, err := interviewTaskID(with["task"])
	if err != nil {
		return compactWhat{}, err
	}
	instructions, err := workflowString(with, "instructions", false)
	if err != nil {
		return compactWhat{}, err
	}
	return compactWhat{task: strings.TrimSpace(task), instructions: strings.TrimSpace(instructions)}, nil
}

// ask asks the agent to compact. An agent at work is no error: it is asked
// again. Any other refusal is the step's failure.
func (e *AgentCompactExecutor) ask(ctx context.Context, attemptID string, target compactWhat) error {
	if e.compactor == nil {
		return errors.New("this Legato cannot compact agents")
	}
	// Numbered before the ask, so the events it causes come after.
	e.mu.Lock()
	attempt := e.attempts[attemptID]
	if attempt == nil {
		attempt = &compactAttempt{}
		e.attempts[attemptID] = attempt
	}
	e.seq++
	askedAt := e.seq
	e.mu.Unlock()
	err := e.compactor.CompactAgent(ctx, target.task, target.instructions)
	e.mu.Lock()
	defer e.mu.Unlock()
	switch {
	case err == nil:
		attempt.asked, attempt.working, attempt.askedAt = true, false, askedAt
		return nil
	case errors.Is(err, ErrAgentWorking):
		attempt.working = true
		return nil
	case errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("task %s has no agent running to compact", target.task)
	default:
		return err
	}
}

// note records an agent's compaction end, in the order it came.
func (e *AgentCompactExecutor) note(taskID string, event chimera.EventEnvelope) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq++
	var data struct {
		Before *int   `json:"tokensBefore"`
		After  *int   `json:"tokensAfter"`
		Error  string `json:"error"`
	}
	_ = json.Unmarshal(event.Data, &data)
	e.ends[taskID] = &compactEnd{at: e.seq, failed: event.Type == "compaction_failed", reason: strings.TrimSpace(data.Error), before: data.Before, after: data.After}
}

// outcome is the end of the compaction an attempt asked for, when this
// process asked for it and heard it end; nil otherwise.
func (e *AgentCompactExecutor) outcome(attemptID, taskID string) *compactEnd {
	e.mu.Lock()
	defer e.mu.Unlock()
	attempt, end := e.attempts[attemptID], e.ends[taskID]
	if attempt == nil || !attempt.asked || end == nil || end.at < attempt.askedAt {
		return nil
	}
	return end
}

func (e *AgentCompactExecutor) hasAsked(attemptID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	attempt := e.attempts[attemptID]
	return attempt != nil && attempt.asked
}

func (e *AgentCompactExecutor) forget(attemptID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.attempts, attemptID)
}

// overdue is why an attempt has waited too long, or "" while it may wait.
func (e *AgentCompactExecutor) overdue(attempt store.WorkflowStepAttempt) string {
	started, ok := parseStoreTime(attempt.StartedAt)
	if !ok || e.now().Sub(started) < CompactStepLimit {
		return ""
	}
	minutes := int(CompactStepLimit / time.Minute)
	e.mu.Lock()
	known := e.attempts[attempt.ID]
	working := known != nil && known.working && !known.asked
	e.mu.Unlock()
	if working {
		return fmt.Sprintf("The agent was still working %d minutes after the step started, so it was not compacted.", minutes)
	}
	return fmt.Sprintf("Legato saw no end to the compaction within %d minutes of the step starting. Retry the step to compact the agent again.", minutes)
}

// parseStoreTime reads a time the store wrote with datetime('now'), in UTC.
func parseStoreTime(value string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339Nano} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// compactingAttempts are the waiting agent.compact attempts, with what each
// compacts, and the executor; nil without one.
func (s *WorkflowService) compactingAttempts(ctx context.Context) (*AgentCompactExecutor, []compactingAttempt, error) {
	executor, ok := s.executors[AgentCompactStepType].(*AgentCompactExecutor)
	if !ok {
		return nil, nil, nil
	}
	attempts, err := s.WaitingAttempts(ctx, AgentCompactStepType)
	if err != nil {
		return nil, nil, err
	}
	var found []compactingAttempt
	var errs []error
	for _, attempt := range attempts {
		with, err := s.attemptWith(ctx, attempt)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		target, err := compactTarget(with)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		found = append(found, compactingAttempt{attempt: attempt, target: target})
	}
	return executor, found, errors.Join(errs...)
}

type compactingAttempt struct {
	attempt store.WorkflowStepAttempt
	target  compactWhat
}

// isCompactionEvent says whether an agent's event concerns a compact step:
// a main-slot agent's compaction_finished or compaction_failed.
func isCompactionEvent(agent WatchedAgent, event chimera.EventEnvelope) bool {
	switch event.Type {
	case "compaction_finished", "compaction_failed":
		return namedSlot(agent.Slot) == ""
	}
	return false
}

// noteCompaction records an agent's compaction event in the order it came.
func (s *WorkflowService) noteCompaction(agent WatchedAgent, event chimera.EventEnvelope) bool {
	executor, ok := s.executors[AgentCompactStepType].(*AgentCompactExecutor)
	if !ok || !isCompactionEvent(agent, event) {
		return false
	}
	executor.note(agent.TaskID, event)
	return true
}

// ObserveCompaction is told an agent's event. A compaction's end ends the
// waiting agent.compact step that asked for it (AgentCompactExecutor.outcome):
// compaction_finished completes it with its sizes, compaction_failed fails
// it.
func (s *WorkflowService) ObserveCompaction(ctx context.Context, agent WatchedAgent, event chimera.EventEnvelope) {
	if s.noteCompaction(agent, event) {
		s.settleCompactions(ctx)
	}
}

// settleCompactions ends each waiting agent.compact step whose compaction
// this process heard end.
func (s *WorkflowService) settleCompactions(ctx context.Context) {
	executor, attempts, err := s.compactingAttempts(ctx)
	if err != nil {
		log.Printf("workflows: compactions: %v", err)
	}
	for _, waiting := range attempts {
		s.settleCompaction(ctx, executor, waiting)
	}
}

// settleCompaction ends one waiting step whose compaction ended, and says
// whether it did.
func (s *WorkflowService) settleCompaction(ctx context.Context, executor *AgentCompactExecutor, waiting compactingAttempt) bool {
	attempt := waiting.attempt
	end := executor.outcome(attempt.ID, waiting.target.task)
	if end == nil {
		return false
	}
	var err error
	if end.failed {
		reason := end.reason
		if reason == "" {
			reason = "the agent's tool gave no reason"
		}
		_, err = s.FailWaiting(ctx, attempt.RunID, attempt.Path, attempt.ID, "The compaction failed: "+reason)
	} else {
		outputs := map[string]any{}
		if end.before != nil {
			outputs["tokens_before"] = *end.before
		}
		if end.after != nil {
			outputs["tokens_after"] = *end.after
		}
		_, err = s.CompleteWaiting(ctx, attempt.RunID, attempt.Path, attempt.ID, outputs)
	}
	executor.forget(attempt.ID)
	if err != nil && !errors.Is(err, store.ErrWorkflowStateConflict) {
		log.Printf("workflows: %s: %v", attempt.Path, err)
	}
	return true
}

// CheckCompactions ends each waiting agent.compact step whose compaction
// this process heard end, fails each one past CompactStepLimit, and asks
// the agent of each other one that this process has not seen take an ask
// to compact again: one at work at the step's start is compacted once its
// turn ends. A refusal other than the agent being at work fails the step.
func (s *WorkflowService) CheckCompactions(ctx context.Context) error {
	executor, attempts, err := s.compactingAttempts(ctx)
	var errs []error
	if err != nil {
		errs = append(errs, err)
	}
	for _, waiting := range attempts {
		attempt := waiting.attempt
		if s.settleCompaction(ctx, executor, waiting) {
			continue
		}
		if reason := executor.overdue(attempt); reason != "" {
			s.failCompaction(ctx, executor, attempt, reason, &errs)
			continue
		}
		if executor.hasAsked(attempt.ID) {
			continue
		}
		if err := executor.ask(ctx, attempt.ID, waiting.target); err != nil {
			s.failCompaction(ctx, executor, attempt, err.Error(), &errs)
			continue
		}
		// An end may have come while it was asked.
		s.settleCompaction(ctx, executor, waiting)
	}
	return errors.Join(errs...)
}

// failCompaction fails a waiting agent.compact step with reason.
func (s *WorkflowService) failCompaction(ctx context.Context, executor *AgentCompactExecutor, attempt store.WorkflowStepAttempt, reason string, errs *[]error) {
	executor.forget(attempt.ID)
	if _, err := s.FailWaiting(ctx, attempt.RunID, attempt.Path, attempt.ID, reason); err != nil && !errors.Is(err, store.ErrWorkflowStateConflict) {
		*errs = append(*errs, err)
	}
}

// CompactionEvents is where the compact steps hear of their agents'
// compactions: the ApprovalWatcher, which follows every native agent.
type CompactionEvents interface {
	AddListener(fn func(agent WatchedAgent, event chimera.EventEnvelope))
}

// StartCompactWaits has the waiting agent.compact steps told of their
// agents' compactions through events, and checks them (CheckCompactions)
// every interval. It returns a function that stops both: the listener
// stays on events (listeners cannot be removed) but does nothing.
func (s *WorkflowService) StartCompactWaits(ctx context.Context, events CompactionEvents, every time.Duration) func() {
	ctx, cancel := context.WithCancel(ctx)
	if events != nil {
		events.AddListener(func(agent WatchedAgent, event chimera.EventEnvelope) {
			// Noted here, in the stream's order; settled off the stream,
			// as completing a step may start the next, which may stop
			// this agent.
			if ctx.Err() == nil && s.noteCompaction(agent, event) {
				go s.settleCompactions(ctx)
			}
		})
	}
	var once sync.Once
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.CheckCompactions(ctx); err != nil && ctx.Err() == nil {
					log.Printf("workflows: compactions: %v", err)
				}
			}
		}
	}()
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}
