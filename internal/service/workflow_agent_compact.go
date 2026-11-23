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
// the agent's compaction_finished or compaction_failed, which
// WorkflowService.ObserveCompaction is told, ends the step.
type AgentCompactExecutor struct {
	compactor AgentCompactor
	now       func() time.Time

	mu sync.Mutex
	// seen is what this process saw of each attempt's agent: compactAsked
	// once it took the ask, compactWorking while it refused it at work.
	// Another process, or this one after a restart, does not know of an
	// attempt: it asks again, and the agent refuses while it compacts.
	seen map[string]string
}

// What a process saw of a compact step's agent.
const (
	compactAsked   = "asked"
	compactWorking = "working"
)

// NewAgentCompactExecutor compacts agents through compactor.
func NewAgentCompactExecutor(compactor AgentCompactor) *AgentCompactExecutor {
	return &AgentCompactExecutor{compactor: compactor, now: time.Now, seen: map[string]string{}}
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
	err := e.compactor.CompactAgent(ctx, target.task, target.instructions)
	switch {
	case err == nil:
		e.see(attemptID, compactAsked)
		return nil
	case errors.Is(err, ErrAgentWorking):
		e.see(attemptID, compactWorking)
		return nil
	case errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("task %s has no agent running to compact", target.task)
	default:
		return err
	}
}

func (e *AgentCompactExecutor) see(attemptID, what string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seen[attemptID] = what
}

func (e *AgentCompactExecutor) hasAsked(attemptID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.seen[attemptID] == compactAsked
}

func (e *AgentCompactExecutor) forget(attemptID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.seen, attemptID)
}

// overdue is why an attempt has waited too long, or "" while it may wait.
func (e *AgentCompactExecutor) overdue(attempt store.WorkflowStepAttempt) string {
	started, ok := parseStoreTime(attempt.StartedAt)
	if !ok || e.now().Sub(started) < CompactStepLimit {
		return ""
	}
	minutes := int(CompactStepLimit / time.Minute)
	e.mu.Lock()
	seen := e.seen[attempt.ID]
	e.mu.Unlock()
	if seen == compactWorking {
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

// ObserveCompaction ends the waiting agent.compact steps of an agent that
// reports the end of a compaction: compaction_finished completes them with
// its sizes; compaction_failed fails those whose compaction this process
// asked for (another's failure leaves the step to ask for its own). It is an
// ApprovalWatcher listener.
func (s *WorkflowService) ObserveCompaction(ctx context.Context, agent WatchedAgent, event chimera.EventEnvelope) {
	if (event.Type != "compaction_finished" && event.Type != "compaction_failed") || namedSlot(agent.Slot) != "" {
		return
	}
	executor, attempts, err := s.compactingAttempts(ctx)
	if err != nil {
		log.Printf("workflows: compactions: %v", err)
	}
	var data struct {
		Before *int   `json:"tokensBefore"`
		After  *int   `json:"tokensAfter"`
		Error  string `json:"error"`
	}
	_ = json.Unmarshal(event.Data, &data)
	for _, waiting := range attempts {
		attempt := waiting.attempt
		if waiting.target.task != agent.TaskID {
			continue
		}
		if event.Type == "compaction_finished" {
			outputs := map[string]any{}
			if data.Before != nil {
				outputs["tokens_before"] = *data.Before
			}
			if data.After != nil {
				outputs["tokens_after"] = *data.After
			}
			_, err = s.CompleteWaiting(ctx, attempt.RunID, attempt.Path, attempt.ID, outputs)
		} else {
			if !executor.hasAsked(attempt.ID) {
				continue
			}
			reason := strings.TrimSpace(data.Error)
			if reason == "" {
				reason = "the agent's tool gave no reason"
			}
			_, err = s.FailWaiting(ctx, attempt.RunID, attempt.Path, attempt.ID, "The compaction failed: "+reason)
		}
		executor.forget(attempt.ID)
		if err != nil && !errors.Is(err, store.ErrWorkflowStateConflict) {
			log.Printf("workflows: %s: %v", attempt.Path, err)
		}
	}
}

// CheckCompactions fails each waiting agent.compact step past
// CompactStepLimit, and asks the agent of each other one that this process
// has not seen take a compaction to compact again: one at work at the
// step's start is compacted once its turn ends. A refusal other than the
// agent being at work fails the step.
func (s *WorkflowService) CheckCompactions(ctx context.Context) error {
	executor, attempts, err := s.compactingAttempts(ctx)
	var errs []error
	if err != nil {
		errs = append(errs, err)
	}
	for _, waiting := range attempts {
		attempt := waiting.attempt
		if reason := executor.overdue(attempt); reason != "" {
			s.failCompaction(ctx, executor, attempt, reason, &errs)
			continue
		}
		if executor.hasAsked(attempt.ID) {
			continue
		}
		if err := executor.ask(ctx, attempt.ID, waiting.target); err != nil {
			s.failCompaction(ctx, executor, attempt, err.Error(), &errs)
		}
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
			if ctx.Err() != nil || (event.Type != "compaction_finished" && event.Type != "compaction_failed") {
				return
			}
			// Off the agent's stream: completing the step may start the
			// next, which may stop this agent.
			go s.ObserveCompaction(ctx, agent, event)
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
