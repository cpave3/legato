package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// The review loop steps (spec LEG-559 § The five steps; LEG-585): catalog
// steps over the engine's operations on one loop record, which
// github.pr.review.open opens and hands to the run's later steps by its ID.
// Every one is a service waiting step: starting it only checks its inputs
// and writes the step, and the review engine's tick, in the Legato that
// serves the broker, does the GitHub and agent work and ends it (D9). So a
// process without the engine never fails one: it waits for the holder.

// The review loop step types.
// The review loop step types (workflowengine.ReviewOpenStepType and the
// rest; TestEveryWorkflowExecutorHasACatalogEntry holds them to the
// catalog).
const (
	ReviewOpenStepType    = "github.pr.review.open"
	ReviewPassStepType    = "github.pr.review.pass"
	ReviewWaitStepType    = "github.pr.review.wait"
	ReviewConfirmStepType = "github.pr.review.confirm"
	ReviewCloseStepType   = "github.pr.review.close"
	ReviewReadyStepType   = "github.pr.ready"
)

// Refusal codes of the review loop steps (error_code), beside
// review_loop_active and author_loop_active (review_loop_lifecycle.go) and
// loop_not_found and loop_closed (review_loop_ops.go).
const (
	ReviewRefusalLoopNotOwned            = "loop_not_owned"
	ReviewRefusalPassesOff               = "review_passes_off"
	ReviewRefusalAppMissing              = "reviewer_app_missing"
	ReviewRefusalPRNotOpen               = "pr_not_open"
	ReviewRefusalInvalidSubject          = "invalid_subject"
	ReviewRefusalNoAuthor                = "no_author"
	ReviewRefusalReviewerNotAdvertised   = "reviewer_not_advertised"
	ReviewRefusalSkillNotAdvertised      = "skill_not_advertised"
	ReviewRefusalStewardNeedsAgentAuthor = "steward_needs_agent_author"
	ReviewRefusalHandoffInput            = "handoff_input_refused"
	// ReviewRefusalMaxReentriesRaised: max_reentries above this Legato's
	// review_passes.max_reentries, which a workflow may lower, never raise.
	ReviewRefusalMaxReentriesRaised = "max_reentries_raised"
	// ReviewRefusalInvalidSettings: a max_passes or max_reentries below 0.
	ReviewRefusalInvalidSettings = "invalid_settings"
	// ReviewRefusalNotApprovedAtHeads: github.pr.ready before the loop is
	// approved, or a layer without legato-reviews' APPROVE at its head on
	// GitHub (moved since, withheld from an outsider, or held).
	ReviewRefusalNotApprovedAtHeads = "not_approved_at_heads"
	// ReviewRefusalReadyFailed: a layer GitHub would not mark ready.
	ReviewRefusalReadyFailed = "ready_failed"
)

// ReviewOutcomeEnded is a loop its workflow closed before it was approved,
// stuck or closed by GitHub.
const ReviewOutcomeEnded = "ended"

// reviewRefused is a step refusal with its code.
func reviewRefused(code, format string, args ...any) error {
	return &WorkflowStepError{Code: code, Message: code + ": " + fmt.Sprintf(format, args...)}
}

// reviewStepError gives an error of the engine its code and link as a
// step's failure: another loop's refusal (with that loop's link), or a
// loop by ID refused. Any other error is as it was.
func reviewStepError(err error) error {
	var typed *WorkflowStepError
	if err == nil || errors.As(err, &typed) {
		return err
	}
	var active *ReviewLoopActiveError
	if errors.As(err, &active) {
		return &WorkflowStepError{Code: active.Code(), Link: active.Link, Message: err.Error(), Err: err}
	}
	var author *AuthorLoopActiveError
	if errors.As(err, &author) {
		return &WorkflowStepError{Code: author.Code(), Link: author.Link, Message: err.Error(), Err: err}
	}
	var byID *ReviewLoopError
	if errors.As(err, &byID) {
		return &WorkflowStepError{Code: byID.Code(), Message: err.Error(), Err: err}
	}
	return err
}

// reviewOpenInput is what an open step asks for, as its inputs resolved.
type reviewOpenInput struct {
	Repo   string
	Number int
	// PRs are the pull requests of a list subject, bottom first; nil for
	// a pull request (Number) and its scope.
	PRs          []int
	Scope        string
	AgentAuthor  bool
	Reviewer     string
	Skill        string
	MaxPasses    int
	MaxReentries int
	Ready        bool
	Steward      bool
}

// numbers are the pull requests the subject names before its stack is
// found.
func (in reviewOpenInput) numbers() []int {
	if len(in.PRs) > 0 {
		return in.PRs
	}
	return []int{in.Number}
}

// present says whether a resolved input names something: an input that
// resolved to nothing, an empty text or list, or 0 does not.
func present(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case []any:
		return len(v) > 0
	case float64:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	}
	return true
}

// inputBool reads a resolved boolean input: a boolean, or its text.
func inputBool(with map[string]any, key string) (bool, error) {
	switch v := with[key].(type) {
	case nil:
		return false, nil
	case bool:
		return v, nil
	case string:
		switch strings.TrimSpace(strings.ToLower(v)) {
		case "", "false":
			return false, nil
		case "true":
			return true, nil
		}
	}
	return false, fmt.Errorf("%s must be true or false, not %v", key, with[key])
}

// reviewPRList reads prs: a list of pull request numbers (or its JSON text).
func reviewPRList(value any) ([]int, error) {
	if text, ok := value.(string); ok {
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			return nil, errors.New("prs must be a list of pull request numbers")
		}
		value = decoded
	}
	list, ok := value.([]any)
	if !ok {
		return nil, errors.New("prs must be a list of pull request numbers")
	}
	seen := map[int]bool{}
	var out []int
	for _, item := range list {
		n, err := workflowInt(item)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("prs must be a list of pull request numbers, not %v", item)
		}
		if seen[n] {
			return nil, fmt.Errorf("prs names #%d twice", n)
		}
		seen[n] = true
		out = append(out, n)
	}
	return out, nil
}

// parseReviewOpen reads an open step's inputs and refuses what a workflow
// may not ask for, with its code: what any process can say of the step
// without GitHub. instanceReentries is this Legato's
// review_passes.max_reentries (0: not known here).
func parseReviewOpen(with map[string]any, instanceReentries int) (reviewOpenInput, error) {
	if _, ok := with["handoff"]; ok {
		return reviewOpenInput{}, reviewRefused(ReviewRefusalHandoffInput, "the handoff binding is set by Legato's handoff service, never by a workflow, a launcher or the API")
	}
	var in reviewOpenInput
	repo, _ := with["repo"].(string)
	in.Repo = strings.TrimSpace(repo)
	if !strings.Contains(in.Repo, "/") {
		return in, reviewRefused(ReviewRefusalInvalidSubject, "a review loop needs repo (owner/name)")
	}
	hasNumber, hasList := present(with["pr_number"]), present(with["prs"])
	switch {
	case hasNumber && hasList:
		return in, reviewRefused(ReviewRefusalInvalidSubject, "give pr_number or prs, not both")
	case !hasNumber && !hasList:
		return in, reviewRefused(ReviewRefusalInvalidSubject, "give pr_number or prs")
	case hasNumber:
		n, err := workflowInt(with["pr_number"])
		if err != nil || n <= 0 {
			return in, reviewRefused(ReviewRefusalInvalidSubject, "pr_number must be a pull request's number, not %v", with["pr_number"])
		}
		in.Number = n
		scope, err := reviewScope(with)
		if err != nil {
			return in, reviewRefused(ReviewRefusalInvalidSubject, "%v", err)
		}
		in.Scope = scope
	default:
		prs, err := reviewPRList(with["prs"])
		if err != nil {
			return in, reviewRefused(ReviewRefusalInvalidSubject, "%v", err)
		}
		if len(prs) > workflowengine.MaxReviewLoopPRs {
			return in, reviewRefused(ReviewRefusalInvalidSubject, "a review loop takes at most %d pull requests, not %d", workflowengine.MaxReviewLoopPRs, len(prs))
		}
		if present(with["scope"]) {
			return in, reviewRefused(ReviewRefusalInvalidSubject, "scope goes with pr_number: prs names every pull request the loop reviews")
		}
		in.PRs, in.Scope = prs, ReviewScopePR
	}
	var err error
	for key, target := range map[string]*bool{"agent_author": &in.AgentAuthor, "ready": &in.Ready, "steward": &in.Steward} {
		if *target, err = inputBool(with, key); err != nil {
			return in, reviewRefused(ReviewRefusalInvalidSettings, "%v", err)
		}
	}
	if in.Steward && !in.AgentAuthor {
		return in, reviewRefused(ReviewRefusalStewardNeedsAgentAuthor, "steward needs agent_author: Legato looks after a pull request only for the agent that wrote it")
	}
	for key, target := range map[string]*int{"max_passes": &in.MaxPasses, "max_reentries": &in.MaxReentries} {
		if !present(with[key]) {
			continue
		}
		n, err := workflowInt(with[key])
		if err != nil || n < 0 {
			return in, reviewRefused(ReviewRefusalInvalidSettings, "%s must be 0 (this Legato's setting) or more, not %v", key, with[key])
		}
		*target = n
	}
	if instanceReentries > 0 && in.MaxReentries > instanceReentries {
		return in, reviewRefused(ReviewRefusalMaxReentriesRaised, "max_reentries %d is more than this Legato's review_passes.max_reentries (%d): a workflow may lower it, never raise it", in.MaxReentries, instanceReentries)
	}
	in.Reviewer, _ = with["reviewer"].(string)
	in.Reviewer = strings.TrimSpace(in.Reviewer)
	in.Skill, _ = with["skill"].(string)
	in.Skill = strings.TrimSpace(in.Skill)
	return in, nil
}

// ReviewLoopSteps are the review loop step executors every workflow service
// registers (Executors). Starting one only checks its inputs and writes the
// step; the review engine's tick, in the Legato that serves the broker,
// moves it on through the run caller (Attach). With no caller attached,
// every step that checks out waits for the holder.
type ReviewLoopSteps struct {
	mu           sync.RWMutex
	caller       *ReviewRunCaller
	enabled      func() bool
	maxReentries func() int
}

// NewReviewLoopSteps are the steps with no caller yet.
func NewReviewLoopSteps() *ReviewLoopSteps { return &ReviewLoopSteps{} }

// SetEnabled gives the steps review_passes.enabled as this process reads
// it: an open while it is off is refused (review_passes_off).
func (s *ReviewLoopSteps) SetEnabled(enabled func() bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = enabled
}

// SetMaxReentries gives the steps review_passes.max_reentries, which a
// workflow may lower but never raise.
func (s *ReviewLoopSteps) SetMaxReentries(max func() int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxReentries = max
}

// Attach runs the steps on caller.
func (s *ReviewLoopSteps) Attach(caller *ReviewRunCaller) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caller = caller
	if caller != nil {
		caller.maxReentries = s.maxReentries
	}
}

func (s *ReviewLoopSteps) current() *ReviewRunCaller {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.caller
}

func (s *ReviewLoopSteps) instanceReentries() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.maxReentries == nil {
		return 0
	}
	return s.maxReentries()
}

// Executors are the step executors, one per step type.
func (s *ReviewLoopSteps) Executors() []WorkflowExecutor {
	return []WorkflowExecutor{reviewOpenExecutor{s}, reviewPassExecutor{reviewObserver{s, ReviewPassStepType}},
		reviewWaitExecutor{reviewObserver{s, ReviewWaitStepType}}, reviewConfirmExecutor{reviewObserver{s, ReviewConfirmStepType}}, reviewCloseExecutor{s},
		reviewReadyExecutor{s}}
}

// reviewOpenExecutor is github.pr.review.open.
type reviewOpenExecutor struct{ steps *ReviewLoopSteps }

// ReviewLoopStepsOf is the step set a review loop step's executor belongs
// to, nil for any other executor: the runtime attaches the run caller to
// it.
func ReviewLoopStepsOf(executor WorkflowExecutor) *ReviewLoopSteps {
	if member, ok := executor.(interface{ loopSteps() *ReviewLoopSteps }); ok {
		return member.loopSteps()
	}
	return nil
}

func (e reviewOpenExecutor) loopSteps() *ReviewLoopSteps  { return e.steps }
func (e reviewCloseExecutor) loopSteps() *ReviewLoopSteps { return e.steps }
func (e reviewObserver) loopSteps() *ReviewLoopSteps      { return e.steps }
func (e reviewReadyExecutor) loopSteps() *ReviewLoopSteps { return e.steps }

func (e reviewOpenExecutor) Type() string { return ReviewOpenStepType }

// Start checks the open's inputs and the pull requests no loop holds, and
// waits: the holder's tick opens the loop (OpenWaiting) and completes the
// step with its outputs.
func (e reviewOpenExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	e.steps.mu.RLock()
	enabled := e.steps.enabled
	e.steps.mu.RUnlock()
	in, err := parseReviewOpen(step.With, e.steps.instanceReentries())
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if enabled != nil && !enabled() {
		return WorkflowExecutionResult{}, reviewRefused(ReviewRefusalPassesOff, "review passes are off on this Legato: turn on review_passes, with a reviewer App credential, to run a review loop")
	}
	if caller := e.steps.current(); caller != nil {
		for _, n := range in.numbers() {
			if active := caller.engine.activeLoopOn(ctx, in.Repo, n, step.TaskID); active != nil {
				return WorkflowExecutionResult{}, reviewStepError(active)
			}
		}
	}
	return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
}

// Cancel leaves nothing to undo: a loop the step opened belongs to its run,
// whose end closes it.
func (e reviewOpenExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

// Details says the step waits for the review engine.
func (e reviewOpenExecutor) Details(_ context.Context, attempt store.WorkflowStepAttempt) any {
	if attempt.Status != WorkflowStepWaiting {
		return nil
	}
	return ReviewRunDetails{Phase: ReviewPhaseOpening, Summary: reviewOpeningSummary, Reviews: []ReviewRunReview{}}
}

// reviewCloseExecutor is github.pr.review.close.
type reviewCloseExecutor struct{ steps *ReviewLoopSteps }

func (e reviewCloseExecutor) Type() string { return ReviewCloseStepType }

// Start checks the step's loop is its run's, and waits: the holder's tick
// closes it and completes the step with its outputs (SettleSteps).
// Closing a closed loop changes nothing.
func (e reviewCloseExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	db := e.steps.store()
	if db == nil {
		return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
	}
	if _, err := ownedStepLoop(ctx, db, step, true); err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
}

func (e reviewCloseExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

// store is the store the steps read, from the attached caller; nil without
// one.
func (s *ReviewLoopSteps) store() *store.Store {
	if caller := s.current(); caller != nil {
		return caller.engine.db
	}
	return nil
}

// ownedStepLoop is the loop a step's loop input names, refused with its
// code when there is none (loop_not_found), another run owns it
// (loop_not_owned), or it is closed (loop_closed) and closed is false.
func ownedStepLoop(ctx context.Context, db *store.Store, step ResolvedWorkflowStep, closed bool) (*store.ReviewLoop, error) {
	id, _ := step.With["loop"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, reviewRefused(ReviewRefusalLoopNotFound, "the step needs loop: ${{ steps.<open step>.outputs.loop_id }}")
	}
	rec, err := db.GetReviewLoop(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, reviewRefused(ReviewRefusalLoopNotFound, "there is no review loop %s", id)
	}
	if err != nil {
		return nil, err
	}
	if rec.OwnerKind != store.ReviewLoopOwnerRun || rec.RunID != step.RunID {
		name, _ := reviewLoopName(rec)
		return nil, reviewRefused(ReviewRefusalLoopNotOwned, "review loop %s belongs to %s, not to this run: a step acts only on its own run's loop", id, name)
	}
	if !closed && rec.State != store.ReviewLoopOpenState {
		return nil, reviewRefused(ReviewRefusalLoopClosed, "review loop %s is closed", id)
	}
	return rec, nil
}

// stepOwned says the loop a run task hosts was opened by an open step and
// belongs to its run (not to a waiting inline step).
func (c *ReviewRunCaller) stepOwned(ctx context.Context, taskID string) bool {
	l, err := c.engine.db.CurrentReviewLoop(ctx, taskID)
	return err == nil && l != nil && l.OwnerKind == store.ReviewLoopOwnerRun && c.engine.db.ReviewLoopOpenedByStep(ctx, l.AttemptID)
}

// runRunning says a run still runs, so its loop moves on; a failed run
// keeps its loop for Retry and moves nothing (D1).
func (c *ReviewRunCaller) runRunning(ctx context.Context, runID string) bool {
	view, err := c.engine.db.GetWorkflowRun(ctx, runID)
	if err != nil {
		return false
	}
	return view.Run.Status == "running" || view.Run.Status == "waiting"
}

// stepLoops are the open loops open steps handed to runs that still run,
// which the engine moves on as it moves an inline step's.
func (c *ReviewRunCaller) stepLoops(ctx context.Context) []ReviewLoop {
	open, err := c.engine.db.ListOpenReviewLoops(ctx)
	if err != nil {
		return nil
	}
	var out []ReviewLoop
	for _, rec := range open {
		if rec.OwnerKind != store.ReviewLoopOwnerRun || !c.engine.db.ReviewLoopOpenedByStep(ctx, rec.AttemptID) || !c.runRunning(ctx, rec.RunID) {
			continue
		}
		if l, ok := c.Loop(ctx, rec.HostTask); ok {
			out = append(out, l)
		}
	}
	return out
}

// openSteps opens the open steps that wait with no loop yet, and completes
// each with its loop's outputs; a step it cannot open fails with its code.
// Only the engine's tick calls it, with c.opening held.
func (c *ReviewRunCaller) openSteps(ctx context.Context) {
	attempts, err := c.workflows.WaitingAttempts(ctx, ReviewOpenStepType)
	if err != nil {
		log.Printf("review passes: the open steps waiting: %v", err)
		return
	}
	for _, attempt := range attempts {
		step, err := c.waitingStep(ctx, attempt)
		var rec *store.ReviewLoop
		var warnings []string
		if err == nil {
			rec, warnings, err = c.openStep(ctx, step)
		}
		if err != nil {
			if _, failErr := c.workflows.FailWaitingError(ctx, attempt.RunID, attempt.Path, attempt.ID, reviewStepError(err)); failErr != nil && !errors.Is(failErr, store.ErrWorkflowStateConflict) {
				log.Printf("review passes: failing %s: %v", attempt.RunID, failErr)
			}
			continue
		}
		outputs, err := c.openOutputs(ctx, rec, warnings)
		if err != nil {
			log.Printf("review passes: the outputs of %s's open: %v", attempt.RunID, err)
			continue
		}
		if _, err := c.workflows.CompleteWaiting(ctx, attempt.RunID, attempt.Path, attempt.ID, outputs); err != nil && !errors.Is(err, store.ErrWorkflowStateConflict) {
			log.Printf("review passes: completing %s's open: %v", attempt.RunID, err)
		}
	}
}

// openStep opens an open step's loop: the one it opened already (a holder
// that stopped before completing it), else a new one (open).
func (c *ReviewRunCaller) openStep(ctx context.Context, step ResolvedWorkflowStep) (*store.ReviewLoop, []string, error) {
	if rec, err := c.engine.db.ReviewLoopForStep(ctx, step.RunID, step.StepID); err != nil {
		return nil, nil, err
	} else if rec != nil {
		return rec, nil, nil
	}
	reentries := 0
	if c.maxReentries != nil {
		reentries = c.maxReentries()
	}
	in, err := parseReviewOpen(step.With, reentries)
	if err != nil {
		return nil, nil, err
	}
	if in.MaxReentries == 0 {
		in.MaxReentries = reentries
	}
	warnings, err := c.open(ctx, step, in)
	if err != nil {
		return nil, nil, err
	}
	rec, err := c.engine.db.ReviewLoopForStep(ctx, step.RunID, step.StepID)
	if err == nil && rec == nil {
		err = fmt.Errorf("run %s's step %s opened no review loop", step.RunID, step.StepID)
	}
	return rec, warnings, err
}

// openOutputs are an open step's outputs: the loop's ID, its linked task
// and its pull requests.
func (c *ReviewRunCaller) openOutputs(ctx context.Context, rec *store.ReviewLoop, warnings []string) (map[string]any, error) {
	layers, err := c.engine.db.ListReviewLoopPRs(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	prs := []map[string]any{}
	for _, layer := range layers {
		prs = append(prs, map[string]any{"number": layer.Number, "url": layer.URL, "branch": layer.Branch, "base": layer.Base, "author_task": layer.AuthorTask})
	}
	if warnings == nil {
		warnings = []string{}
	}
	return map[string]any{"loop_id": rec.ID, "linked_task": rec.LinkedTask, "prs": prs, "warnings": warnings}, nil
}

// reviewSettler is a caller whose waiting steps the engine's tick settles
// once its loops have moved on, outside the engine's lock.
type reviewSettler interface {
	SettleSteps(ctx context.Context)
}

// settleSteps settles every caller's waiting steps.
func (s *ReviewPassService) settleSteps(ctx context.Context) {
	s.mu.Lock()
	callers := append([]ReviewCaller(nil), s.callers...)
	s.mu.Unlock()
	for _, caller := range callers {
		if settler, ok := caller.(reviewSettler); ok {
			settler.SettleSteps(ctx)
		}
	}
}

// SettleSteps ends the review loop steps whose loop has got where they
// wait for: a close step closes its loop and completes with the contract's
// outputs. Only the engine's tick calls it, outside the engine's lock.
func (c *ReviewRunCaller) SettleSteps(ctx context.Context) {
	if c.workflows == nil {
		return
	}
	for _, kind := range []string{ReviewPassStepType, ReviewWaitStepType, ReviewConfirmStepType} {
		c.settleObservers(ctx, kind)
	}
	c.settleReady(ctx)
	attempts, err := c.workflows.WaitingAttempts(ctx, ReviewCloseStepType)
	if err != nil {
		log.Printf("review passes: the close steps waiting: %v", err)
		return
	}
	for _, attempt := range attempts {
		step, err := c.waitingStep(ctx, attempt)
		if err != nil {
			continue
		}
		rec, err := ownedStepLoop(ctx, c.engine.db, step, true)
		if err != nil {
			if _, failErr := c.workflows.FailWaitingError(ctx, attempt.RunID, attempt.Path, attempt.ID, err); failErr != nil && !errors.Is(failErr, store.ErrWorkflowStateConflict) {
				log.Printf("review passes: failing %s: %v", attempt.RunID, failErr)
			}
			continue
		}
		outputs, err := c.closeStepLoop(ctx, rec)
		if err != nil {
			log.Printf("review passes: closing loop %s of %s, trying again: %v", rec.ID, rec.RunID, err)
			continue
		}
		if _, err := c.workflows.CompleteWaiting(ctx, attempt.RunID, attempt.Path, attempt.ID, outputs); err != nil && !errors.Is(err, store.ErrWorkflowStateConflict) {
			log.Printf("review passes: completing %s's close: %v", attempt.RunID, err)
		}
	}
}

// stepOutcome is the outcome a run's open loop closes with: approved once
// confirmed at GitHub's heads, stuck while it waits for the operator, else
// ended (its workflow closed it on the way).
func (c *ReviewRunCaller) stepOutcome(ctx context.Context, rec *store.ReviewLoop) string {
	loop, err := c.engine.db.GetHandoffReview(ctx, rec.HostTask)
	if err != nil || loop == nil || loop.LoopID != rec.ID {
		return ReviewOutcomeEnded
	}
	switch {
	case confirmed(loop):
		return ReviewOutcomeApproved
	case loop.Phase == ReviewPhaseStuck:
		return ReviewOutcomeStuck
	}
	return ReviewOutcomeEnded
}

// closeStepLoop closes a run's loop with its outcome (Close; a closed one
// keeps its own) and gives the close step's outputs.
func (c *ReviewRunCaller) closeStepLoop(ctx context.Context, rec *store.ReviewLoop) (map[string]any, error) {
	if rec.State == store.ReviewLoopOpenState {
		if _, err := c.engine.Close(ctx, rec.ID, c.stepOutcome(ctx, rec)); err != nil {
			return nil, err
		}
		fresh, err := c.engine.db.GetReviewLoop(ctx, rec.ID)
		if err != nil {
			return nil, err
		}
		rec = fresh
	}
	return c.closeOutputs(ctx, rec)
}

// closeOutputs are a closed loop's outputs (spec LEG-559
// github.pr.review.close): its outcome and passes, the clean pass and when
// GitHub confirmed it, the findings counted, each pull request's state and
// review, and the summary file of every pass.
func (c *ReviewRunCaller) closeOutputs(ctx context.Context, rec *store.ReviewLoop) (map[string]any, error) {
	findings, err := c.engine.db.ListReviewLoopFindings(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{"total": len(findings), "blocking": 0, "nits": 0, "open": 0}
	for _, f := range findings {
		switch f.Severity {
		case SeverityBlocking:
			counts["blocking"]++
			if f.Status == "open" {
				counts["open"]++
			}
		case SeverityNit:
			counts["nits"]++
		}
	}
	layers, err := c.engine.db.ListReviewLoopPRs(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	approved := rec.Outcome == ReviewOutcomeApproved
	events := map[int]string{}
	if approved && rec.CleanPass > 0 {
		if pins, err := c.engine.db.ListHandoffReviewPRs(ctx, rec.HostTask, rec.CleanPass); err == nil {
			for _, pin := range pins {
				events[pin.Number] = pin.Event
			}
		}
	}
	withheld := false
	prs := []map[string]any{}
	for _, layer := range layers {
		state, draft := layer.State, layer.Draft
		if c.opts.GitHub != nil {
			if pr, read, err := c.opts.GitHub.PullRequest(ctx, rec.Repo, layer.Number); err == nil {
				state, draft = read, pr.Draft
			}
		}
		head := layer.CleanHead
		if head == "" {
			head = layer.PinnedHead
		}
		// An approval a later pass kept (the layer did not change) stands.
		layerApproved := approved && (events[layer.Number] == ReviewApprove || events[layer.Number] == ReviewKept)
		if approved && events[layer.Number] == ReviewComment && !rec.AgentAuthor {
			withheld = true
		}
		prs = append(prs, map[string]any{"number": layer.Number, "url": layer.URL, "state": state, "draft": draft,
			"author_task": layer.AuthorTask, "approved": layerApproved, "head_sha": head, "review_url": layer.ReviewURL})
	}
	path, err := c.writeSummary(ctx, rec, counts)
	if err != nil {
		return nil, err
	}
	// The passes posted: one discarded at the close (or re-pinned when a
	// head moved) is not counted.
	posted := 0
	if passes, err := c.engine.db.ListReviewLoopPasses(ctx, rec.ID); err == nil {
		for _, p := range passes {
			if p.State == store.ReviewLoopPassPosted {
				posted++
			}
		}
	}
	return map[string]any{"outcome": rec.Outcome, "passes": posted, "clean_pass": rec.CleanPass, "confirmed_at": rec.ConfirmedAt,
		"findings": counts, "prs": prs, "approval_withheld": withheld, "summary_path": path, "linked_task": rec.LinkedTask, "loop_id": rec.ID}, nil
}

// writeSummary writes a loop's summary file: every pass with its verdict
// and reviews, and the findings. "" without a summary folder.
func (c *ReviewRunCaller) writeSummary(ctx context.Context, rec *store.ReviewLoop, counts map[string]int) (string, error) {
	if c.opts.SummaryDir == "" {
		return "", nil
	}
	var numbers []int
	if layers, err := c.engine.db.ListReviewLoopPRs(ctx, rec.ID); err == nil {
		for _, l := range layers {
			numbers = append(numbers, l.Number)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Review loop %s on %s: %s after %d %s.\n", rec.ID, reviewStackName(rec.Repo, numbers), rec.Outcome, rec.Pass, plural(rec.Pass, "pass", "passes"))
	passes, _ := c.engine.db.ListReviewLoopPasses(ctx, rec.ID)
	for _, p := range passes {
		fmt.Fprintf(&b, "\n## Pass %d: %s", p.Pass, p.State)
		if p.Verdict != "" {
			fmt.Fprintf(&b, ", %s", p.Verdict)
		}
		b.WriteString("\n")
		if pins, err := c.engine.db.ListHandoffReviewPRs(ctx, rec.HostTask, p.Pass); err == nil {
			for _, pin := range pins {
				if pin.ReviewURL != "" {
					fmt.Fprintf(&b, "\n- #%d at %s: %s %s", pin.Number, short(pin.HeadSHA), pin.Event, pin.ReviewURL)
				}
			}
		}
	}
	fmt.Fprintf(&b, "\n\n%d %s: %d blocking (%d still open), %d %s.\n", counts["total"], plural(counts["total"], "finding", "findings"),
		counts["blocking"], counts["open"], counts["nits"], plural(counts["nits"], "nit", "nits"))
	dir := filepath.Join(c.opts.SummaryDir, rec.RunID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "review-loop-"+rec.ID+".md")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// reviewObserver is github.pr.review.pass, .wait (mode author) or .confirm:
// a step that waits until its loop gets where it says, which the holder's
// tick moves the loop to (Tick: a pass pinned, reviewed and posted; the
// author's round; Confirm at GitHub's heads). Its outputs come from the
// record (SettleSteps), so it is tied to no session: a Retry, a restart or
// another process finds the same loop where it stands.
type reviewObserver struct {
	steps *ReviewLoopSteps
	kind  string
}

// The observer steps, each its own type so its Type names it.
type (
	reviewPassExecutor    struct{ reviewObserver }
	reviewWaitExecutor    struct{ reviewObserver }
	reviewConfirmExecutor struct{ reviewObserver }
)

func (reviewPassExecutor) Type() string    { return ReviewPassStepType }
func (reviewWaitExecutor) Type() string    { return ReviewWaitStepType }
func (reviewConfirmExecutor) Type() string { return ReviewConfirmStepType }

// Start checks the step's loop is its run's and open, and the wait's mode,
// and waits. A watch (the steward) needs a loop opened with steward, and
// may start on one that closed: it says how it ended.
func (e reviewObserver) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	watch := false
	if e.kind == ReviewWaitStepType {
		switch mode := waitMode(step); mode {
		case ReviewWaitModeAuthor:
		case ReviewWaitModeWatch:
			watch = true
		default:
			return WorkflowExecutionResult{}, reviewRefused(ReviewRefusalInvalidSettings, "a wait takes mode author or watch, not %q", mode)
		}
	}
	if db := e.steps.store(); db != nil {
		rec, err := ownedStepLoop(ctx, db, step, watch)
		if err != nil {
			return WorkflowExecutionResult{}, err
		}
		if watch && !rec.Steward {
			return WorkflowExecutionResult{}, reviewRefused(ReviewRefusalInvalidSettings, "review loop %s was not opened with steward: true, so nothing watches it after approval", rec.ID)
		}
	}
	return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
}

// waitMode is a wait step's mode, author when it names none.
func waitMode(step ResolvedWorkflowStep) string {
	mode, _ := step.With["mode"].(string)
	if mode = strings.TrimSpace(mode); mode == "" {
		return ReviewWaitModeAuthor
	}
	return mode
}

// Cancel leaves the loop to its run, whose end closes it.
func (e reviewObserver) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

// Details is where the step's loop stands, as the run page shows it.
func (e reviewObserver) Details(ctx context.Context, attempt store.WorkflowStepAttempt) any {
	if caller := e.steps.current(); caller != nil {
		return caller.loopDetails(ctx, attempt.TaskID)
	}
	return nil
}

// Attention is why the step's loop needs the operator: stuck (it offers
// another_pass, message and end), or waiting for an author that will not
// move it alone.
func (e reviewObserver) Attention(ctx context.Context, attempt store.WorkflowStepAttempt) (string, []string) {
	caller := e.steps.current()
	if caller == nil {
		return "", nil
	}
	return caller.loopAttention(ctx, attempt.TaskID)
}

// Act takes the operator's action on the step's loop by its ID: Another
// pass, Message the author, or End (outcome stuck).
func (e reviewObserver) Act(ctx context.Context, attempt store.WorkflowStepAttempt, action, text string) error {
	caller := e.steps.current()
	if caller == nil {
		return errors.New("review passes are off on this Legato")
	}
	step, err := caller.waitingStep(ctx, attempt)
	if err != nil {
		return err
	}
	rec, err := ownedStepLoop(ctx, caller.engine.db, step, false)
	if err != nil {
		return err
	}
	return caller.engine.ActOnLoop(ctx, rec.ID, action, text)
}

// The modes of github.pr.review.wait: the author's round, or the steward's
// watch after approval (review_steward.go).
const (
	ReviewWaitModeAuthor = workflowengine.ReviewWaitModeAuthor
	ReviewWaitModeWatch  = workflowengine.ReviewWaitModeWatch
)

// settleObservers ends the waiting steps of one observer kind whose loop
// got where they wait for; a loop no longer the run's fails its step.
func (c *ReviewRunCaller) settleObservers(ctx context.Context, kind string) {
	attempts, err := c.workflows.WaitingAttempts(ctx, kind)
	if err != nil {
		log.Printf("review passes: the %s steps waiting: %v", kind, err)
		return
	}
	for _, attempt := range attempts {
		step, err := c.waitingStep(ctx, attempt)
		if err != nil {
			continue
		}
		rec, err := ownedStepLoop(ctx, c.engine.db, step, true)
		var outputs map[string]any
		if err == nil {
			outputs, err = c.observe(ctx, kind, step, rec)
		}
		if err != nil {
			if _, failErr := c.workflows.FailWaitingError(ctx, attempt.RunID, attempt.Path, attempt.ID, reviewStepError(err)); failErr != nil && !errors.Is(failErr, store.ErrWorkflowStateConflict) {
				log.Printf("review passes: failing %s: %v", attempt.RunID, failErr)
			}
			continue
		}
		if outputs == nil {
			continue // not there yet
		}
		if _, err := c.workflows.CompleteWaiting(ctx, attempt.RunID, attempt.Path, attempt.ID, outputs); err != nil && !errors.Is(err, store.ErrWorkflowStateConflict) {
			log.Printf("review passes: completing %s's %s: %v", attempt.RunID, kind, err)
		}
	}
}

// observe is an observer step's outputs once its loop got where it waits
// for, nil while it has not.
func (c *ReviewRunCaller) observe(ctx context.Context, kind string, step ResolvedWorkflowStep, rec *store.ReviewLoop) (map[string]any, error) {
	switch kind {
	case ReviewPassStepType:
		return c.observePass(ctx, step, rec)
	case ReviewWaitStepType:
		if waitMode(step) == ReviewWaitModeWatch {
			return c.observeWatch(ctx, rec)
		}
		return c.observeWait(ctx, rec)
	case ReviewConfirmStepType:
		return c.observeConfirm(ctx, step, rec)
	}
	return nil, nil
}

// endedState is what a loop closed under a waiting step says to it: stuck
// (the operator ended it while stuck), closed (its pull requests closed),
// or, for a loop approved and closed, approved. A loop cancelled with its
// run says nothing: the run is ending.
func endedState(rec *store.ReviewLoop) string {
	switch rec.Outcome {
	case ReviewOutcomeStuck, ReviewOutcomeClosed, ReviewOutcomeApproved:
		return rec.Outcome
	}
	return ""
}

// consumedPass is the newest pass an earlier github.pr.review.pass step of
// the run already reported: a pass step reports only a later one, so a
// clean pass that checks sent back to its author is never read twice.
func (c *ReviewRunCaller) consumedPass(ctx context.Context, runID, loopID string) int {
	view, err := c.engine.db.GetWorkflowRun(ctx, runID)
	if err != nil {
		return 0
	}
	newest := 0
	for _, step := range view.Steps {
		if step.ExecutorType != ReviewPassStepType || step.Status != "succeeded" || step.OutputsJSON == "" {
			continue
		}
		var out struct {
			Pass int    `json:"pass"`
			Loop string `json:"loop_id"`
		}
		if json.Unmarshal([]byte(step.OutputsJSON), &out) == nil && out.Loop == loopID && out.Pass > newest {
			newest = out.Pass
		}
	}
	return newest
}

// observePass completes a pass step once a pass later than the run's last
// is posted (verdict clean or changes, state posted), or the loop ended
// (state stuck or closed). While the loop is stuck the step waits, with
// Another pass, Message the author and End.
func (c *ReviewRunCaller) observePass(ctx context.Context, step ResolvedWorkflowStep, rec *store.ReviewLoop) (map[string]any, error) {
	consumed := c.consumedPass(ctx, step.RunID, rec.ID)
	out := func(state, verdict string, pass int) map[string]any {
		return map[string]any{"state": state, "verdict": verdict, "pass": pass, "loop_id": rec.ID}
	}
	if rec.State != store.ReviewLoopOpenState {
		if state := endedState(rec); state != "" {
			return out(state, "", rec.Pass), nil
		}
		return nil, nil
	}
	result, err := c.engine.Pass(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	switch result.State {
	case ReviewPassStatePosted:
		if result.Pass <= consumed {
			return nil, nil // the run read this one already: the next one
		}
		return out(ReviewPassStatePosted, result.Verdict, result.Pass), nil
	case ReviewPassStateNone:
		return out(ReviewOutcomeClosed, "", result.Pass), nil
	}
	return nil, nil // pending, or stuck for the operator
}

// observeWait completes a wait (mode author) once the author is done (state
// ready), the pull requests closed (closed), or the operator ended the loop
// while stuck (stuck). It waits while the author works, and while the loop
// is stuck.
func (c *ReviewRunCaller) observeWait(ctx context.Context, rec *store.ReviewLoop) (map[string]any, error) {
	out := func(state string) map[string]any { return map[string]any{"state": state, "loop_id": rec.ID} }
	if rec.State != store.ReviewLoopOpenState {
		if state := endedState(rec); state != "" {
			if state == ReviewOutcomeApproved {
				state = ReviewWaitReady
			}
			return out(state), nil
		}
		return nil, nil
	}
	result, err := c.engine.Wait(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	switch result.State {
	case ReviewWaitReady, ReviewWaitClosed:
		return out(result.State), nil
	}
	return nil, nil
}

// observeWatch completes a watch (mode watch, the steward) with what woke
// it: pushed or disputed once the loop went back to review (a pass is to
// start), closed when a pull request closed unmerged, and merged, idle or
// ended as the loop closed so (the operator's End of a stuck loop is
// ended). It begins the steward's watch on a loop the review approved, and
// waits while the loop is approved and watched, waits for its author, or
// is stuck.
func (c *ReviewRunCaller) observeWatch(ctx context.Context, rec *store.ReviewLoop) (map[string]any, error) {
	out := func(woke string) map[string]any { return map[string]any{"woke": woke, "loop_id": rec.ID} }
	if rec.State != store.ReviewLoopOpenState {
		switch rec.Outcome {
		case ReviewOutcomeClosed, StewardWokeMerged, StewardWokeIdle, StewardWokeEnded:
			return out(rec.Outcome), nil
		case ReviewOutcomeStuck, ReviewOutcomeApproved:
			return out(StewardWokeEnded), nil
		}
		return nil, nil // cancelled with its run
	}
	loop, err := c.engine.db.GetHandoffReview(ctx, rec.HostTask)
	if err != nil || loop == nil || loop.LoopID != rec.ID {
		return nil, err
	}
	if !stewarding(rec) {
		if !confirmed(loop) {
			return nil, nil // not approved yet: the review's
		}
		if err := c.engine.beginWatch(ctx, rec); err != nil {
			return nil, err
		}
		return nil, nil
	}
	switch loop.Phase {
	case ReviewPhaseReady, ReviewPhaseWaitingForReviewer, ReviewPhaseReviewing:
		woke := rec.LastWake
		if woke != StewardWokeDisputed {
			woke = StewardWokePushed
		}
		return out(woke), nil
	case ReviewPhaseSkipped, ReviewPhaseEnded:
		return out(StewardWokeClosed), nil
	}
	return nil, nil
}

// observeConfirm completes a confirm once the holder's Confirm read
// GitHub's heads after the run's last clean pass: approved (confirmed at
// them), or moved (a head moved: the loop goes on to a new pass); a loop
// that closed says closed or stuck. While a counted check sent the clean
// pass back to its author, it waits.
func (c *ReviewRunCaller) observeConfirm(ctx context.Context, step ResolvedWorkflowStep, rec *store.ReviewLoop) (map[string]any, error) {
	out := func(state string) map[string]any {
		o := map[string]any{"state": state, "loop_id": rec.ID, "confirmed_at": "", "pass": rec.Pass}
		if state == ReviewConfirmApproved {
			o["confirmed_at"], o["pass"] = rec.ConfirmedAt, rec.CleanPass
		}
		return o
	}
	if rec.State != store.ReviewLoopOpenState {
		if state := endedState(rec); state != "" {
			return out(state), nil
		}
		return nil, nil
	}
	loop, err := c.engine.db.GetHandoffReview(ctx, rec.HostTask)
	if err != nil || loop == nil || loop.LoopID != rec.ID {
		return nil, err
	}
	consumed := c.consumedPass(ctx, step.RunID, rec.ID)
	switch {
	case confirmed(loop) && loop.Pass == consumed:
		return out(ReviewConfirmApproved), nil
	case loop.Phase == ReviewPhaseClean, loop.Phase == ReviewPhaseStuck:
		return nil, nil // GitHub's heads not read yet, or the operator decides
	case loop.Pass > consumed:
		return out(ReviewConfirmMoved), nil // a later pass already
	}
	switch loop.Phase {
	case ReviewPhaseReady, ReviewPhaseWaitingForReviewer, ReviewPhaseReviewing:
		// A head moved since the clean pass: a new pass reviews it.
		return out(ReviewConfirmMoved), nil
	}
	// The author handles a counted check that failed at the clean pass
	// (checksReopen): a reply settles it and the same pass is approved, a
	// push starts a new pass. Either way, wait.
	return nil, nil
}

// loopDetails is where the loop a run task hosts stands, for the run page:
// as the inline step's, once it has a loop.
func (c *ReviewRunCaller) loopDetails(ctx context.Context, taskID string) any {
	return c.details(ctx, taskID)
}

// loopAttention is the inline step's attention, for the loop a run task
// hosts.
func (c *ReviewRunCaller) loopAttention(ctx context.Context, taskID string) (string, []string) {
	return c.attention(ctx, taskID)
}

// reviewReadyExecutor is github.pr.ready (LEG-588): it marks a loop's
// approved stack ready, as the author App, in the holder's tick.
type reviewReadyExecutor struct{ steps *ReviewLoopSteps }

func (e reviewReadyExecutor) Type() string { return ReviewReadyStepType }

// Start checks the step's loop is its run's, and waits for the holder.
func (e reviewReadyExecutor) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	if db := e.steps.store(); db != nil {
		if _, err := ownedStepLoop(ctx, db, step, true); err != nil {
			return WorkflowExecutionResult{}, err
		}
	}
	return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
}

func (e reviewReadyExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }

// ReviewReadyGitHub marks a pull request ready for review as the author App
// (ReviewPublisher.MarkReady): false when it was not a draft.
type ReviewReadyGitHub interface {
	MarkReady(ctx context.Context, repo string, number int) (bool, error)
}

// settleReady runs the waiting ready steps: each completes with the layers
// it marked ready, or fails with not_approved_at_heads or ready_failed. A
// GitHub read that fails is tried again on the next tick.
func (c *ReviewRunCaller) settleReady(ctx context.Context) {
	attempts, err := c.workflows.WaitingAttempts(ctx, ReviewReadyStepType)
	if err != nil {
		log.Printf("review passes: the ready steps waiting: %v", err)
		return
	}
	for _, attempt := range attempts {
		step, err := c.waitingStep(ctx, attempt)
		if err != nil {
			continue
		}
		rec, err := ownedStepLoop(ctx, c.engine.db, step, true)
		var outputs map[string]any
		if err == nil {
			outputs, err = c.markStackReady(ctx, rec)
		}
		var refused *WorkflowStepError
		if err != nil && !errors.As(err, &refused) {
			log.Printf("review passes: marking loop %s ready, trying again: %v", attempt.RunID, err)
			continue
		}
		if err != nil {
			if _, failErr := c.workflows.FailWaitingError(ctx, attempt.RunID, attempt.Path, attempt.ID, err); failErr != nil && !errors.Is(failErr, store.ErrWorkflowStateConflict) {
				log.Printf("review passes: failing %s: %v", attempt.RunID, failErr)
			}
			continue
		}
		if _, err := c.workflows.CompleteWaiting(ctx, attempt.RunID, attempt.Path, attempt.ID, outputs); err != nil && !errors.Is(err, store.ErrWorkflowStateConflict) {
			log.Printf("review passes: completing %s's ready: %v", attempt.RunID, err)
		}
	}
}

// markStackReady marks a loop's approved stack ready (spec LEG-559
// github.pr.ready): only when the loop is approved and every open layer has
// legato-reviews' APPROVE (or an approval kept from before) at its head on
// GitHub now and none is held, so a withheld or held approval, or a push
// since, never makes a pull request ready (not_approved_at_heads). Then
// each open draft, bottom first, as the author App; one already ready is
// left alone and none goes back to draft. One GitHub will not mark ready
// fails the step (ready_failed), naming it; those below it stay ready.
func (c *ReviewRunCaller) markStackReady(ctx context.Context, rec *store.ReviewLoop) (map[string]any, error) {
	notApproved := func(format string, args ...any) error {
		return reviewRefused(ReviewRefusalNotApprovedAtHeads, format, args...)
	}
	if rec.ConfirmedAt == "" || rec.CleanPass == 0 || rec.CleanPass != rec.Pass ||
		(rec.State != store.ReviewLoopOpenState && rec.Outcome != ReviewOutcomeApproved) {
		return nil, notApproved("review loop %s is not approved at GitHub's heads", rec.ID)
	}
	ready, ok := c.opts.GitHub.(ReviewReadyGitHub)
	if !ok {
		return nil, reviewRefused(ReviewRefusalReadyFailed, "this Legato cannot mark pull requests ready")
	}
	layers, err := c.engine.db.ListReviewLoopPRs(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	events := map[int]string{}
	if pins, err := c.engine.db.ListHandoffReviewPRs(ctx, rec.HostTask, rec.CleanPass); err == nil {
		for _, pin := range pins {
			events[pin.Number] = pin.Event
		}
	}
	type layer struct {
		number int
		draft  bool
	}
	var open []layer
	for _, l := range layers {
		pr, state, err := c.opts.GitHub.PullRequest(ctx, rec.Repo, l.Number)
		if err != nil {
			return nil, fmt.Errorf("reading #%d: %w", l.Number, err)
		}
		if state != "open" {
			continue // merged or closed: nothing to make ready
		}
		switch {
		case pr.HeadSHA != l.CleanHead:
			return nil, notApproved("#%d's head on GitHub is %s, not %s, which the review approved", l.Number, short(pr.HeadSHA), short(l.CleanHead))
		case events[l.Number] != ReviewApprove && events[l.Number] != ReviewKept:
			return nil, notApproved("#%d has no approval from legato-reviews at %s (its review is a COMMENT: the approval was withheld or held)", l.Number, short(pr.HeadSHA))
		case c.engine.approvalHeld(ctx, rec.ID, l.Number):
			return nil, notApproved("#%d's approval is held", l.Number)
		}
		open = append(open, layer{number: l.Number, draft: pr.Draft})
	}
	flipped := []int{}
	for _, l := range open {
		if !l.draft {
			continue
		}
		// Nearest the write: the run still runs and still owns the loop
		// (D11); the window after this read is accepted.
		if fresh, err := c.engine.db.GetReviewLoop(ctx, rec.ID); err != nil || fresh.RunID != rec.RunID || !c.runRunning(ctx, rec.RunID) {
			return nil, fmt.Errorf("loop %s is no longer its run's to mark ready", rec.ID)
		}
		changed, err := ready.MarkReady(ctx, rec.Repo, l.number)
		if err != nil {
			return nil, &WorkflowStepError{Code: ReviewRefusalReadyFailed, Err: err, Outputs: map[string]any{"flipped": flipped},
				Message: fmt.Sprintf("%s: #%d would not be marked ready: %v", ReviewRefusalReadyFailed, l.number, err)}
		}
		if changed {
			flipped = append(flipped, l.number)
		}
	}
	return map[string]any{"ready": true, "flipped": flipped, "loop_id": rec.ID}, nil
}
