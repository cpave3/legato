package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	gh "github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/store"
)

// PRWaitStepType is the step that waits for a pull request to merge, or
// close (LEG-540).
const PRWaitStepType = "github.pr.wait"

// A pull request's states, as the wait step reads them.
const (
	PRStateOpen   = "open"
	PRStateClosed = "closed"
	PRStateMerged = "merged"
)

// errPRClosedUnmerged is a pull request a wait for its merge can never see
// merge.
var errPRClosedUnmerged = errors.New("closed without merging")

// PRState is where a pull request stands.
type PRState struct {
	// State is PRStateOpen, PRStateClosed or PRStateMerged.
	State    string
	MergedAt string
	URL      string
	// HeadBranch is the branch it merges from, BaseBranch the one it
	// merges into.
	HeadBranch string
	BaseBranch string
	// Conflicts says whether it conflicts with its base: nil while GitHub
	// works it out, when the last answer stands.
	Conflicts *bool
	// CheckStatus is its head's CI: pass, fail, pending, or "" with none.
	CheckStatus string
}

// PRStateReader reads a pull request's state from GitHub.
type PRStateReader interface {
	PullRequestState(ctx context.Context, repo string, number int) (PRState, error)
}

// GitHubPRStates reads pull requests' states with Legato's read-only GitHub
// client, one request each.
type GitHubPRStates struct{ Client *gh.Client }

func (g GitHubPRStates) PullRequestState(_ context.Context, repo string, number int) (PRState, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return PRState{}, fmt.Errorf("repository %q is not owner/name", repo)
	}
	status, err := g.Client.FetchPRWaitState(owner, name, number)
	if err != nil {
		return PRState{}, err
	}
	state := PRState{
		State: strings.ToLower(status.State), MergedAt: status.MergedAt, URL: status.URL,
		HeadBranch: status.HeadBranch, BaseBranch: status.BaseBranch, CheckStatus: status.CheckStatus,
	}
	switch {
	case status.MergeableState == "dirty":
		state.Conflicts = boolPtr(true)
	case status.Mergeable != nil:
		state.Conflicts = boolPtr(!*status.Mergeable)
	}
	return state, nil
}

func boolPtr(v bool) *bool { return &v }

// PRWaitExecutor is github.pr.wait: it reads the pull request when the step
// starts, and waits on WorkflowService.CheckPRWaits when it is still open.
// It keeps the last state it read of each waiting attempt's pull request,
// in memory: each host process reads GitHub for itself, at once when it
// starts, so nothing needs it across a restart.
type PRWaitExecutor struct {
	reader PRStateReader
	mu     sync.Mutex
	seen   map[string]PRState
}

// NewPRWaitExecutor waits on the pull requests reader reads.
func NewPRWaitExecutor(reader PRStateReader) *PRWaitExecutor {
	return &PRWaitExecutor{reader: reader, seen: map[string]PRState{}}
}

// remember keeps what a read of an attempt's pull request said, and says
// whether that changes what its needs-you item says: a first read, a
// conflict that starts or ends, CI that changes. A read that leaves
// mergeability unknown keeps the last answer.
func (e *PRWaitExecutor) remember(attemptID string, state PRState) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	old, ok := e.seen[attemptID]
	if state.Conflicts == nil && ok {
		state.Conflicts = old.Conflicts
	}
	e.seen[attemptID] = state
	return !ok || conflicted(old) != conflicted(state) || old.CheckStatus != state.CheckStatus
}

func conflicted(state PRState) bool { return state.Conflicts != nil && *state.Conflicts }

// last is what the latest read of an attempt's pull request said.
func (e *PRWaitExecutor) last(attemptID string) (PRState, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	state, ok := e.seen[attemptID]
	return state, ok
}

func (e *PRWaitExecutor) forget(attemptID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.seen, attemptID)
}

func (e *PRWaitExecutor) Type() string { return PRWaitStepType }

func (e *PRWaitExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

func (e *PRWaitExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	target, err := prWaitTarget(step.With)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	state, outputs, done, err := e.check(ctx, target)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if done {
		return WorkflowExecutionResult{Status: "succeeded", Outputs: outputs}, nil
	}
	if step.AttemptID != "" {
		e.remember(step.AttemptID, state)
	}
	return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
}

// prWait is what a step waits for.
type prWait struct {
	repo   string
	number int
	until  string
}

func prWaitTarget(with map[string]any) (prWait, error) {
	repo, err := workflowString(with, "repo", true)
	if err != nil {
		return prWait{}, err
	}
	number, err := strconv.Atoi(fmt.Sprint(with["pr_number"]))
	if err != nil || number <= 0 {
		return prWait{}, fmt.Errorf("pr_number must be a pull request's number, not %v", with["pr_number"])
	}
	until, _ := workflowString(with, "until", false)
	switch until = strings.TrimSpace(until); until {
	case "":
		until = PRStateMerged
	case PRStateMerged, PRStateClosed:
	default:
		return prWait{}, fmt.Errorf("until must be merged or closed, not %q", until)
	}
	return prWait{repo: strings.TrimSpace(repo), number: number, until: until}, nil
}

// check reads the pull request: what it said, its outputs and true when
// the wait is over, an error when it can never be (closed without merging,
// waiting for the merge).
func (e *PRWaitExecutor) check(ctx context.Context, target prWait) (PRState, map[string]any, bool, error) {
	if e.reader == nil {
		return PRState{}, nil, false, errors.New("waiting for a pull request needs Legato's GitHub access")
	}
	state, err := e.reader.PullRequestState(ctx, target.repo, target.number)
	if err != nil {
		return PRState{}, nil, false, fmt.Errorf("reading %s#%d: %w", target.repo, target.number, err)
	}
	switch state.State {
	case PRStateMerged:
	case PRStateClosed:
		if target.until == PRStateMerged {
			return state, nil, false, fmt.Errorf("pull request %s#%d %w", target.repo, target.number, errPRClosedUnmerged)
		}
	default:
		return state, nil, false, nil
	}
	return state, map[string]any{
		"state": state.State, "merged": state.State == PRStateMerged, "merged_at": state.MergedAt, "url": state.URL,
	}, true, nil
}

// CheckPRWaits reads the pull request of every github.pr.wait step that
// waits, and completes (or fails) the steps whose wait is over, stopping
// the agent Resume agent started for the run (stopResumedAgent). A read
// that fails leaves the step waiting for the next check.
func (s *WorkflowService) CheckPRWaits(ctx context.Context) error {
	_, err := s.checkPRWaits(ctx)
	return err
}

// PRWaitChangedKind marks the EventWorkflowChanged of a waiting pull
// request whose state changed what its needs-you item says, or ended its
// wait (merged or closed), so Attention drops the item.
const PRWaitChangedKind = "pr_wait"

// checkPRWaits is CheckPRWaits, and the runs whose waiting pull request
// changed what their needs-you item says or whose wait ended. CompleteWaiting
// and FailWaiting publish the step's own change (workflow_events.go); a
// change to the item while the step still waits is only this.
func (s *WorkflowService) checkPRWaits(ctx context.Context) ([]string, error) {
	executor, ok := s.executors[PRWaitStepType].(*PRWaitExecutor)
	if !ok {
		return nil, nil
	}
	attempts, err := s.WaitingAttempts(ctx, PRWaitStepType)
	if err != nil {
		return nil, err
	}
	var errs []error
	var changed []string
	for _, attempt := range attempts {
		with, err := s.attemptWith(ctx, attempt)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		target, err := prWaitTarget(with)
		if err == nil {
			var state PRState
			var outputs map[string]any
			var done bool
			state, outputs, done, err = executor.check(ctx, target)
			if err == nil && !done {
				if executor.remember(attempt.ID, state) {
					changed = append(changed, attempt.RunID)
				}
				continue
			}
			if err == nil {
				_, err = s.CompleteWaiting(ctx, attempt.RunID, attempt.Path, attempt.ID, outputs)
				if errors.Is(err, store.ErrWorkflowStateConflict) {
					continue // another Legato completed it
				}
				var recorded *StepRecordedError
				if err == nil || errors.As(err, &recorded) {
					executor.forget(attempt.ID)
					s.stopResumedAgent(ctx, attempt)
					changed = append(changed, attempt.RunID)
				}
				errs = append(errs, err)
				continue
			}
			// A read that failed is tried again; a PR that can never
			// satisfy the wait fails the step.
			if !errors.Is(err, errPRClosedUnmerged) {
				log.Printf("workflows: %s: %v", attempt.Path, err)
				continue
			}
		}
		_, failErr := s.FailWaiting(ctx, attempt.RunID, attempt.Path, attempt.ID, err.Error())
		if failErr != nil && !errors.Is(failErr, store.ErrWorkflowStateConflict) {
			errs = append(errs, failErr)
		}
		if failErr == nil {
			executor.forget(attempt.ID)
			s.stopResumedAgent(ctx, attempt)
			changed = append(changed, attempt.RunID)
		}
	}
	return changed, errors.Join(errs...)
}

// attemptWith is the resolved with of an attempt's step.
func (s *WorkflowService) attemptWith(ctx context.Context, attempt store.WorkflowStepAttempt) (map[string]any, error) {
	view, err := s.store.GetWorkflowRun(ctx, attempt.RunID)
	if err != nil {
		return nil, err
	}
	for _, step := range view.Steps {
		if step.Path == attempt.Path {
			var with map[string]any
			if err := json.Unmarshal([]byte(step.ResolvedInputsJSON), &with); err != nil {
				return nil, fmt.Errorf("step %s: %w", step.Path, err)
			}
			return with, nil
		}
	}
	return nil, fmt.Errorf("run %s has no step %s", attempt.RunID, attempt.Path)
}

// prWaitEventGap is the least time between two checks a PR refresh starts:
// a sync that refreshes many cards publishes many events.
const prWaitEventGap = 30 * time.Second

// StartPRWaitLoop checks the waiting github.pr.wait steps at once, after
// each refresh of a card's pull requests (EventPRStatusUpdated, at most every
// prWaitEventGap), and every interval. It returns a function that stops it.
func (s *WorkflowService) StartPRWaitLoop(ctx context.Context, bus *events.Bus, every time.Duration) func() {
	ctx, cancel := context.WithCancel(ctx)
	var refreshed <-chan events.Event
	if bus != nil {
		refreshed = bus.Subscribe(events.EventPRStatusUpdated)
	}
	var once sync.Once
	done := make(chan struct{})
	check := func() {
		changed, err := s.checkPRWaits(ctx)
		if err != nil && ctx.Err() == nil {
			log.Printf("workflows: pull request waits: %v", err)
		}
		if bus == nil {
			return
		}
		for _, runID := range changed {
			bus.Publish(events.Event{Type: events.EventWorkflowChanged, At: time.Now(), Payload: events.WorkflowChangedPayload{RunID: runID, Kind: PRWaitChangedKind}})
		}
	}
	go func() {
		defer close(done)
		check()
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		var last time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				check()
				last = time.Now()
			case <-refreshed:
				if time.Since(last) >= prWaitEventGap {
					check()
					last = time.Now()
				}
			}
		}
	}()
	return func() {
		once.Do(func() {
			cancel()
			if bus != nil {
				bus.Unsubscribe(refreshed)
			}
			<-done
		})
	}
}
