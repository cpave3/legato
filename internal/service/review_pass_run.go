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
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
)

// ReviewPassesStepType is the workflow step that runs a review loop on any
// pull request.
const ReviewPassesStepType = "github.pr.review_passes"

// Outcomes of a review loop a run started.
const (
	ReviewOutcomeApproved  = "approved"
	ReviewOutcomeStuck     = "stuck"
	ReviewOutcomeClosed    = "closed"
	ReviewOutcomeCancelled = "cancelled"
)

// ReviewRunGitHub is what a run's review loop asks of GitHub as the reviewer
// App, beyond what every loop does (ReviewPublisher).
type ReviewRunGitHub interface {
	// Installed checks the reviewer App can read repo.
	Installed(ctx context.Context, repo string) error
	// PullRequest is one pull request and its state: open, closed or
	// merged.
	PullRequest(ctx context.Context, repo string, number int) (ReviewGitHubPR, string, error)
}

// ReviewRunClones are the job clones a run's loop reviews in
// (RepositoryService, and a reset of the clone to a pull request's head).
type ReviewRunClones interface {
	// PullHead fetches a pull request's head into the repository's cache,
	// from its pull ref, so a fork's head can be cloned too.
	PullHead(ctx context.Context, repo string, number int) error
	JobClone(ctx context.Context, req JobCloneRequest) (store.JobClone, error)
	RemoveJobClone(ctx context.Context, id int64) error
	// Reset puts a clone's branch, checked out and clean, at sha.
	Reset(ctx context.Context, dir, branch, sha string) error
}

// ReviewRunOwners finds the board task a pull request belongs to
// (OwnerLookupService).
type ReviewRunOwners interface {
	Lookup(ctx context.Context, q OwnerQuery) ([]OwnerMatch, error)
}

// ReviewRunWorkflows ends a run's review step (WorkflowService).
type ReviewRunWorkflows interface {
	WaitingAttempts(ctx context.Context, executorType string) ([]store.WorkflowStepAttempt, error)
	CompleteWaiting(ctx context.Context, runID, stepID, attemptID string, outputs map[string]any) (*store.WorkflowRunView, error)
}

// ReviewRunOptions are what the run caller uses.
type ReviewRunOptions struct {
	GitHub ReviewRunGitHub
	Clones ReviewRunClones
	Owners ReviewRunOwners
	// SummaryDir holds each run's Markdown summary of its loop.
	SummaryDir string
}

// ReviewRunCaller is the review-pass engine's caller for workflow runs: the
// github.pr.review_passes step. Its loop is keyed on the run's own task, the
// reviewer reviews a job clone of the pull request's head, the author is the
// branch's owner (who pushes and replies on GitHub; Legato only reviews), and
// only the reviewer's findings count. No card moves. A clean pass completes
// the step as approved; cancelling the run ends the loop; every end removes
// the clone.
type ReviewRunCaller struct {
	engine    *ReviewPassService
	opts      ReviewRunOptions
	workflows ReviewRunWorkflows
}

// NewReviewRunCaller registers the run caller with the engine. It is also the
// step's executor; SetWorkflows gives it the workflow service it ends steps
// through.
func NewReviewRunCaller(engine *ReviewPassService, opts ReviewRunOptions) *ReviewRunCaller {
	c := &ReviewRunCaller{engine: engine, opts: opts}
	engine.AddCaller(c)
	return c
}

// SetWorkflows gives the caller the workflow service.
func (c *ReviewRunCaller) SetWorkflows(workflows ReviewRunWorkflows) { c.workflows = workflows }

// reviewRun is what a run's loop keeps on its task's metadata
// (legato.review_loop).
type reviewRun struct {
	RunID      string `json:"run_id"`
	StepID     string `json:"step_id"`
	AttemptID  string `json:"attempt_id"`
	Repo       string `json:"repo"`
	PR         int    `json:"pr"`
	Branch     string `json:"branch"`
	CloneID    int64  `json:"clone_id"`
	Dir        string `json:"dir"`
	LinkedTask string `json:"linked_task"`
	// SeenHead is the pull request's head the loop last saw move, and
	// SeenAt when: the next pass waits for it to stay still for the quiet
	// time.
	SeenHead string `json:"seen_head,omitempty"`
	SeenAt   string `json:"seen_at,omitempty"`
	// Replies are the thread replies the loop has taken as answers.
	Replies []int64 `json:"replies,omitempty"`
}

var reviewRunKey = []string{"legato", "review_loop"}

func (c *ReviewRunCaller) run(ctx context.Context, taskID string) (*reviewRun, error) {
	task, err := c.engine.db.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	metadata, err := store.ParseTaskMetadata(task.Metadata)
	if err != nil {
		return nil, err
	}
	legato, _ := metadata["legato"].(map[string]any)
	raw, ok := legato["review_loop"]
	if !ok {
		return nil, fmt.Errorf("%s has no review loop", taskID)
	}
	data, _ := json.Marshal(raw)
	var run reviewRun
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

func (c *ReviewRunCaller) Type() string { return ReviewPassesStepType }

// ErrReviewAppMissing is a repository the reviewer App cannot read.
var ErrReviewAppMissing = errors.New("the reviewer App cannot read this repository")

// Start checks the run can review the pull request, makes the job clone at
// its head, and starts pass 1: the step then waits on the loop.
func (c *ReviewRunCaller) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	repo, _ := step.With["repo"].(string)
	repo = strings.TrimSpace(repo)
	number, err := workflowInt(step.With["pr_number"])
	if err != nil || number <= 0 || !strings.Contains(repo, "/") {
		return WorkflowExecutionResult{}, fmt.Errorf("a review loop needs repo (owner/name) and pr_number")
	}
	agentAuthor, _ := step.With["agent_author"].(bool)
	cfg := c.engine.config()
	if !cfg.Enabled {
		return WorkflowExecutionResult{}, errors.New("review passes are off on this Legato: turn on review_passes, with a reviewer App credential, to run a review loop")
	}
	if c.opts.GitHub == nil || c.opts.Clones == nil {
		return WorkflowExecutionResult{}, errors.New("review loops are not wired on this Legato")
	}
	settings, err := c.settings(cfg, step.With)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if err := c.opts.GitHub.Installed(ctx, repo); err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("%w: Legato's reviewer App cannot read %s (%v). Either the App is not installed on the account, %s is not among the repositories the installation selects, or there is no such repository. Install the App on %s, or run the find-only review-pr workflow instead", ErrReviewAppMissing, repo, err, repo, repo)
	}
	pr, state, err := c.opts.GitHub.PullRequest(ctx, repo, number)
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("reading pull request #%d of %s: %w", number, repo, err)
	}
	if state != "open" {
		return WorkflowExecutionResult{}, fmt.Errorf("pull request #%d of %s is %s: a review loop needs an open pull request", number, repo, state)
	}
	if active := c.engine.ActiveLoopOn(ctx, repo, number); active != nil {
		return WorkflowExecutionResult{}, active
	}
	linked := c.linkedTask(ctx, repo, number, pr.Branch)
	if agentAuthor {
		if linked == "" {
			return WorkflowExecutionResult{}, fmt.Errorf("agent_author needs a board task linked to pull request #%d of %s, and none is: Legato starts no fresh author. Run without agent_author, and the branch's owner is the author", number, repo)
		}
		return WorkflowExecutionResult{}, errors.New("agent_author is not supported yet: run without it, and the branch's owner is the author")
	}
	if err := c.opts.Clones.PullHead(ctx, repo, number); err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("fetching the head of #%d: %w", number, err)
	}
	clone, err := c.opts.Clones.JobClone(ctx, JobCloneRequest{ExecutionID: step.AttemptID, TaskID: step.TaskID, Repo: repo, Ref: pr.HeadSHA,
		FreshBranch: reviewRunBranch(number)})
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("cloning %s at the head of #%d: %w", repo, number, err)
	}
	run := reviewRun{RunID: step.RunID, StepID: step.StepID, AttemptID: step.AttemptID, Repo: repo, PR: number, Branch: clone.Branch,
		CloneID: clone.ID, Dir: clone.Path, LinkedTask: linked}
	if err := c.engine.db.SetTaskMetadata(ctx, step.TaskID, reviewRunKey, run); err != nil {
		c.removeClone(ctx, &run)
		return WorkflowExecutionResult{}, err
	}
	if err := c.engine.beginLoop(ctx, step.TaskID, settings); err != nil {
		c.removeClone(ctx, &run)
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: WorkflowStepWaiting}, nil
}

// Cancel ends a cancelled run's loop: the reviewer stops, nothing more is
// posted, and the clone goes.
func (c *ReviewRunCaller) Cancel(ctx context.Context, attempt store.WorkflowStepAttempt) error {
	c.engine.StopReview(ctx, attempt.TaskID)
	if err := c.engine.endLoop(ctx, attempt.TaskID, ReviewOutcomeCancelled); err != nil {
		log.Printf("review passes: ending the loop of %s: %v", attempt.TaskID, err)
	}
	if run, err := c.run(ctx, attempt.TaskID); err == nil {
		c.removeClone(ctx, run)
	}
	return nil
}

// settings are a run's loop settings: this Legato's review-pass settings,
// with the step's max_passes and reviewer (an agent tool, or tool:tier) on
// them when they are set. A reviewer this Legato does not offer is refused.
func (c *ReviewRunCaller) settings(cfg ReviewPassConfig, with map[string]any) (ReviewPassSettings, error) {
	var o ReviewPassOverride
	if raw, ok := with["max_passes"]; ok && raw != nil && raw != "" {
		n, err := workflowInt(raw)
		if err != nil || n < 0 {
			return ReviewPassSettings{}, fmt.Errorf("max_passes must be a number of passes, not %v", raw)
		}
		o.MaxPasses = n
	}
	if reviewer, _ := with["reviewer"].(string); strings.TrimSpace(reviewer) != "" {
		o.Agent, o.Tier, _ = strings.Cut(strings.TrimSpace(reviewer), ":")
	}
	if o.MaxPasses == 0 && o.Agent == "" {
		return cfg.Defaults, nil
	}
	settings, err := c.engine.Override(o)
	if err != nil && o.Agent != "" && c.engine.opts.Advert != nil {
		if advert := c.engine.opts.Advert(); advert != nil {
			var names []string
			for _, a := range advert.Adapters {
				names = append(names, a.Name)
			}
			return ReviewPassSettings{}, fmt.Errorf("%v; this Legato's reviewers: %s", err, strings.Join(names, ", "))
		}
	}
	return settings, err
}

// linkedTask is the board task the pull request belongs to, or "".
func (c *ReviewRunCaller) linkedTask(ctx context.Context, repo string, number int, branch string) string {
	if c.opts.Owners == nil {
		return ""
	}
	matches, err := c.opts.Owners.Lookup(ctx, OwnerQuery{Repo: repo, PR: number, Branch: branch})
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0].Task.ID
}

func (c *ReviewRunCaller) removeClone(ctx context.Context, run *reviewRun) {
	if run.CloneID == 0 {
		return
	}
	if err := c.opts.Clones.RemoveJobClone(ctx, run.CloneID); err != nil && !errors.Is(err, store.ErrNotFound) {
		log.Printf("review passes: removing the clone of %s#%d: %v", run.Repo, run.PR, err)
	}
}

func reviewRunBranch(number int) string { return fmt.Sprintf("legato-review/pr-%d", number) }

// Loops are the runs whose review step waits on its loop.
func (c *ReviewRunCaller) Loops(ctx context.Context) ([]ReviewLoop, error) {
	if c.workflows == nil {
		return nil, nil
	}
	attempts, err := c.workflows.WaitingAttempts(ctx, ReviewPassesStepType)
	if err != nil {
		return nil, err
	}
	var out []ReviewLoop
	for _, attempt := range attempts {
		if l, ok := c.Loop(ctx, attempt.TaskID); ok {
			out = append(out, l)
		}
	}
	return out, nil
}

// Loop is a run task's loop: the person who owns the branch is its author,
// and only findings count.
func (c *ReviewRunCaller) Loop(ctx context.Context, taskID string) (ReviewLoop, bool) {
	run, err := c.run(ctx, taskID)
	if err != nil {
		return ReviewLoop{}, false
	}
	return ReviewLoop{TaskID: taskID, Repo: run.Repo, PR: run.PR, Name: "run " + run.RunID, Link: "/workflows/runs/" + run.RunID}, true
}

// Checkout puts the run's clone at the pull request's head on GitHub now and
// names it; a pull request no longer open has nothing to review.
func (c *ReviewRunCaller) Checkout(ctx context.Context, l ReviewLoop) (string, []ReviewGitHubPR, error) {
	run, err := c.run(ctx, l.TaskID)
	if err != nil {
		return "", nil, &ReviewCheckoutError{Err: err}
	}
	pr, state, err := c.opts.GitHub.PullRequest(ctx, run.Repo, run.PR)
	if err != nil {
		return "", nil, err
	}
	if state != "open" {
		return run.Dir, nil, nil
	}
	if err := c.opts.Clones.PullHead(ctx, run.Repo, run.PR); err != nil {
		return "", nil, err
	}
	if err := c.opts.Clones.Reset(ctx, run.Dir, run.Branch, pr.HeadSHA); err != nil {
		return "", nil, &ReviewCheckoutError{Err: err}
	}
	pr.Branch = run.Branch
	return run.Dir, []ReviewGitHubPR{pr}, nil
}

// AuthorDone says the branch's owner has finished a round: the pull
// request's head moved past the last pass's and stayed still for the quiet
// time, or someone replied in a blocking finding's thread (a dispute, which
// the next pass rules on). A pull request merged or closed ends the loop as
// closed. There is no timeout.
func (c *ReviewRunCaller) AuthorDone(ctx context.Context, l ReviewLoop) bool {
	run, err := c.run(ctx, l.TaskID)
	if err != nil {
		return false
	}
	pr, state, err := c.opts.GitHub.PullRequest(ctx, run.Repo, run.PR)
	if err != nil {
		return false // GitHub may answer next time
	}
	if state != "open" {
		c.finish(ctx, l.TaskID, ReviewOutcomeClosed)
		return false
	}
	pinned := c.pinnedHead(ctx, l.TaskID)
	now := c.engine.opts.Now()
	if pr.HeadSHA != pinned {
		if pr.HeadSHA != run.SeenHead {
			// A push, or another one: the quiet time starts again.
			run.SeenHead, run.SeenAt = pr.HeadSHA, now.UTC().Format(time.RFC3339)
			_ = c.engine.db.SetTaskMetadata(ctx, l.TaskID, reviewRunKey, run)
			return false
		}
		seen, err := time.Parse(time.RFC3339, run.SeenAt)
		return err == nil && now.Sub(seen) >= c.engine.config().QuietTime
	}
	return c.disputed(ctx, l, run)
}

// pinnedHead is the head the loop's last pass reviewed.
func (c *ReviewRunCaller) pinnedHead(ctx context.Context, taskID string) string {
	loop, err := c.engine.db.GetHandoffReview(ctx, taskID)
	if err != nil || loop == nil {
		return ""
	}
	prs, err := c.engine.db.ListHandoffReviewPRs(ctx, taskID, loop.Pass)
	if err != nil || len(prs) == 0 {
		return ""
	}
	return prs[0].HeadSHA
}

// disputed reads the pull request's threads, and takes a reply in the thread
// of an open blocking finding, by anyone but Legato, as the author's answer
// to it.
func (c *ReviewRunCaller) disputed(ctx context.Context, l ReviewLoop, run *reviewRun) bool {
	loop, err := c.engine.db.GetHandoffReview(ctx, l.TaskID)
	if err != nil || loop == nil {
		return false
	}
	items, err := currentItems(ctx, c.engine.db, loop)
	if err != nil {
		return false
	}
	threads := map[int64]store.HandoffReviewItem{}
	for _, item := range items {
		if item.Kind == ItemBlocking && item.ThreadCommentID != 0 && item.Answer == "" {
			threads[item.ThreadCommentID] = item
		}
	}
	if len(threads) == 0 {
		return false
	}
	poll, err := c.engine.opts.GitHub.PollComments(ctx, run.Repo, run.PR, storePollPages{db: c.engine.db, taskID: l.TaskID, pr: run.PR})
	if err != nil {
		return false
	}
	legato := map[string]bool{}
	for _, comment := range poll.Comments {
		if comment.Kind == "review" && strings.Contains(comment.Body, "<!-- legato-review-pass ") {
			legato[strings.ToLower(comment.Author)] = true
		}
	}
	seen := map[int64]bool{}
	for _, id := range run.Replies {
		seen[id] = true
	}
	answered := false
	for _, comment := range poll.Comments {
		item, ok := threads[comment.ThreadRoot]
		if comment.Kind != "inline" || !ok || comment.ID == comment.ThreadRoot || seen[comment.ID] || legato[strings.ToLower(comment.Author)] {
			continue
		}
		note := strings.TrimSpace(comment.Body)
		if note == "" {
			continue
		}
		at := c.engine.opts.Now().UTC().Format(time.RFC3339)
		if err := c.engine.db.AnswerHandoffReviewItem(ctx, l.TaskID, item.ID, AnswerReplied, comment.Author+" replied on GitHub: "+note, at); err != nil {
			continue
		}
		// It is on GitHub already: nothing to post as the author App.
		_ = c.engine.db.MarkHandoffReviewItemPosted(ctx, l.TaskID, item.ID)
		run.Replies = append(run.Replies, comment.ID)
		delete(threads, comment.ThreadRoot)
		answered = true
	}
	if answered {
		_ = c.engine.db.SetTaskMetadata(ctx, l.TaskID, reviewRunKey, run)
	}
	return answered
}

// Pushed says whether the pull request's head on GitHub moved past the head
// the last pass pinned.
func (c *ReviewRunCaller) Pushed(ctx context.Context, l ReviewLoop, pinned []store.HandoffReviewPR) (bool, error) {
	run, err := c.run(ctx, l.TaskID)
	if err != nil {
		return false, err
	}
	pr, _, err := c.opts.GitHub.PullRequest(ctx, run.Repo, run.PR)
	if err != nil {
		return false, err
	}
	for _, p := range pinned {
		if p.Number == run.PR && p.HeadSHA != pr.HeadSHA {
			return true, nil
		}
	}
	return false, nil
}

// AnotherPass starts the next pass of a run's review step now: Run another
// pass. Refused unless the loop waits for its author.
func (c *ReviewRunCaller) AnotherPass(ctx context.Context, runID, stepID string) error {
	if c.workflows == nil {
		return ErrReviewNotWaiting
	}
	attempts, err := c.workflows.WaitingAttempts(ctx, ReviewPassesStepType)
	if err != nil {
		return err
	}
	for _, attempt := range attempts {
		if attempt.RunID == runID && attempt.StepID == stepID {
			return c.engine.NextPass(ctx, attempt.TaskID)
		}
	}
	return ErrReviewNotWaiting
}

// Stage ends the step when a clean pass approved, or when no open pull
// request is left to review.
func (c *ReviewRunCaller) Stage(ctx context.Context, l ReviewLoop, stage string) {
	switch stage {
	case ReviewStageClean:
		c.finish(ctx, l.TaskID, ReviewOutcomeApproved)
	case ReviewStageNone:
		c.finish(ctx, l.TaskID, ReviewOutcomeClosed)
	}
}

// ResumeAuthor: the author is a person.
func (c *ReviewRunCaller) ResumeAuthor(context.Context, ReviewLoop) error {
	return ErrCannotResumeAuthor
}

// Changed says the run changed, for its page.
func (c *ReviewRunCaller) Changed(taskID string) {
	if c.engine.opts.Bus == nil {
		return
	}
	if run, err := c.run(context.Background(), taskID); err == nil {
		c.engine.opts.Bus.Publish(events.Event{Type: events.EventWorkflowChanged, Payload: events.WorkflowChangedPayload{RunID: run.RunID, StepID: run.StepID, Kind: ReviewLoopChangedKind}, At: c.engine.opts.Now()})
	}
}

// Subject is what the reviewer reviews.
func (c *ReviewRunCaller) Subject() string { return "a pull request on GitHub" }

// finish ends a loop with its outcome: the step completes with its outputs,
// and the clone goes.
func (c *ReviewRunCaller) finish(ctx context.Context, taskID, outcome string) {
	run, err := c.run(ctx, taskID)
	if err != nil {
		log.Printf("review passes: ending %s: %v", taskID, err)
		return
	}
	if outcome != ReviewOutcomeApproved && c.engine.opts.Agents != nil {
		// Called inside a tick as well: stop the reviewer without the
		// engine's lock.
		_ = c.engine.opts.Agents.KillAgentInSlot(ctx, taskID, SlotReviewer)
	}
	if err := c.engine.endLoop(ctx, taskID, outcome); err != nil {
		log.Printf("review passes: ending %s: %v", taskID, err)
	}
	outputs, err := c.outputs(ctx, taskID, run, outcome)
	if err != nil {
		log.Printf("review passes: the outputs of %s: %v", run.RunID, err)
		return
	}
	c.removeClone(ctx, run)
	if c.workflows == nil {
		return
	}
	if _, err := c.workflows.CompleteWaiting(ctx, run.RunID, run.StepID, run.AttemptID, outputs); err != nil && !errors.Is(err, store.ErrWorkflowStateConflict) {
		log.Printf("review passes: completing %s: %v", run.RunID, err)
	}
}

// outputs are the step's outputs: the outcome, the passes, the findings, the
// reviews posted, a Markdown summary and the linked board task.
func (c *ReviewRunCaller) outputs(ctx context.Context, taskID string, run *reviewRun, outcome string) (map[string]any, error) {
	block := c.engine.ReviewBlock(ctx, taskID)
	passes := 0
	if block != nil {
		passes = block.Pass
	}
	findings, err := c.engine.db.ListHandoffReviewFindings(ctx, taskID)
	if err != nil {
		return nil, err
	}
	var reviews []map[string]any
	for pass := 1; pass <= passes; pass++ {
		prs, _ := c.engine.db.ListHandoffReviewPRs(ctx, taskID, pass)
		for _, pr := range prs {
			if pr.ReviewURL != "" {
				reviews = append(reviews, map[string]any{"pass": pass, "pr": pr.Number, "url": pr.ReviewURL})
			}
		}
	}
	if reviews == nil {
		reviews = []map[string]any{}
	}
	summary := reviewRunSummary(run, outcome, passes, findings, reviews)
	path := ""
	if c.opts.SummaryDir != "" {
		dir := filepath.Join(c.opts.SummaryDir, run.RunID)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		path = filepath.Join(dir, "review-summary.md")
		if err := os.WriteFile(path, []byte(summary), 0o600); err != nil {
			return nil, err
		}
	}
	return map[string]any{"outcome": outcome, "passes": passes, "findings": len(findings), "reviews": reviews,
		"summary_path": path, "linked_task": run.LinkedTask}, nil
}

// reviewRunSummary is the loop in Markdown, for the linked task's note.
func reviewRunSummary(run *reviewRun, outcome string, passes int, findings []store.HandoffReviewFinding, reviews []map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Review loop on %s#%d: %s after %d %s.\n", run.Repo, run.PR, outcome, passes, plural(passes, "pass", "passes"))
	for _, r := range reviews {
		fmt.Fprintf(&b, "\n- Pass %v review: %v", r["pass"], r["url"])
	}
	blocking, open := 0, 0
	for _, f := range findings {
		if f.Severity == SeverityBlocking {
			blocking++
			if f.Status == "open" {
				open++
			}
		}
	}
	fmt.Fprintf(&b, "\n\n%d %s, %d blocking, %d still open.\n", len(findings), plural(len(findings), "finding", "findings"), blocking, open)
	return b.String()
}

// ReviewPhaseEnded is a loop that ended without a clean pass: closed with
// its pull request, or cancelled. Its reason is the outcome.
const ReviewPhaseEnded = "ended"

// beginLoop starts a loop at once: pass 1 starts on the next tick.
func (s *ReviewPassService) beginLoop(ctx context.Context, taskID string, settings ReviewPassSettings) error {
	data, err := encodeJSON(settings)
	if err != nil {
		return err
	}
	loop := &store.HandoffReview{TaskID: taskID, Phase: ReviewPhaseReady, Settings: data}
	return s.saveChanged(ctx, loop)
}

// endLoop records how a loop ended; an approved one is clean already.
func (s *ReviewPassService) endLoop(ctx context.Context, taskID, outcome string) error {
	if outcome == ReviewOutcomeApproved {
		return nil
	}
	loop, err := s.db.GetHandoffReview(ctx, taskID)
	if err != nil || loop == nil {
		return err
	}
	loop.Phase, loop.Reason, loop.QueuedAt = ReviewPhaseEnded, outcome, ""
	return s.saveChanged(ctx, loop)
}

// ReviewPassesStep is the github.pr.review_passes executor every workflow
// service registers. It runs on the run caller once review passes are wired
// (Attach), and refuses to start before.
type ReviewPassesStep struct {
	mu     sync.RWMutex
	caller *ReviewRunCaller
}

// NewReviewPassesStep is the step with no caller yet.
func NewReviewPassesStep() *ReviewPassesStep { return &ReviewPassesStep{} }

// Attach runs the step on caller.
func (s *ReviewPassesStep) Attach(caller *ReviewRunCaller) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.caller = caller
}

func (s *ReviewPassesStep) current() *ReviewRunCaller {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.caller
}

func (s *ReviewPassesStep) Type() string { return ReviewPassesStepType }

func (s *ReviewPassesStep) Start(ctx context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	caller := s.current()
	if caller == nil {
		return WorkflowExecutionResult{}, errors.New("review passes are off on this Legato: turn on review_passes, with a reviewer App credential, to run a review loop (legato serve runs it)")
	}
	return caller.Start(ctx, step)
}

func (s *ReviewPassesStep) Cancel(ctx context.Context, attempt store.WorkflowStepAttempt) error {
	if caller := s.current(); caller != nil {
		return caller.Cancel(ctx, attempt)
	}
	return nil
}

// ReviewRunDetails is where a run's review loop stands, as the run page
// shows it: its phase, the pass, a line ("Reviewing · pass 2", "Waiting for
// author · 3 blocking", "Approved", "Stuck: …"), and every review posted.
type ReviewRunDetails struct {
	Phase        string            `json:"phase"`
	Pass         int               `json:"pass"`
	Summary      string            `json:"summary"`
	BlockingOpen int               `json:"blocking_open"`
	Reason       string            `json:"reason,omitempty"`
	Reviews      []ReviewRunReview `json:"reviews"`
}

// ReviewRunReview is one review a pass posted.
type ReviewRunReview struct {
	Pass int    `json:"pass"`
	PR   int    `json:"pr"`
	URL  string `json:"url"`
}

// Details is where the run task's loop stands; nil before it has one.
func (c *ReviewRunCaller) Details(ctx context.Context, attempt store.WorkflowStepAttempt) any {
	loop, err := c.engine.db.GetHandoffReview(ctx, attempt.TaskID)
	if err != nil || loop == nil {
		return nil
	}
	d := ReviewRunDetails{Phase: loop.Phase, Pass: loop.Pass, Reason: loop.Reason, Reviews: []ReviewRunReview{}}
	if block := c.engine.ReviewBlock(ctx, attempt.TaskID); block != nil {
		d.BlockingOpen = block.BlockingOpen
	}
	for pass := 1; pass <= loop.Pass; pass++ {
		prs, _ := c.engine.db.ListHandoffReviewPRs(ctx, attempt.TaskID, pass)
		for _, pr := range prs {
			if pr.ReviewURL != "" {
				d.Reviews = append(d.Reviews, ReviewRunReview{Pass: pass, PR: pr.Number, URL: pr.ReviewURL})
			}
		}
	}
	switch loop.Phase {
	case ReviewPhaseReady, ReviewPhaseWaitingForReviewer:
		d.Phase, d.Summary = ReviewPhaseReviewing, fmt.Sprintf("Reviewing · pass %d", loop.Pass+1)
	case ReviewPhaseReviewing:
		d.Summary = fmt.Sprintf("Reviewing · pass %d", loop.Pass)
	case ReviewPhaseWaitingForAuthor:
		d.Summary = "Waiting for author"
		if d.BlockingOpen > 0 {
			d.Summary += fmt.Sprintf(" · %d blocking", d.BlockingOpen)
		}
	case ReviewPhaseClean:
		d.Summary = "Approved"
	case ReviewPhaseStuck:
		d.Summary = "Stuck: " + loop.Reason
	case ReviewPhaseSkipped:
		d.Phase, d.Summary = ReviewPhaseEnded, "Closed"
	case ReviewPhaseEnded:
		d.Summary = map[string]string{ReviewOutcomeClosed: "Closed", ReviewOutcomeCancelled: "Cancelled", ReviewOutcomeStuck: "Ended while stuck"}[loop.Reason]
	}
	return d
}

// Details is the run caller's, once it is attached.
func (s *ReviewPassesStep) Details(ctx context.Context, attempt store.WorkflowStepAttempt) any {
	if caller := s.current(); caller != nil {
		return caller.Details(ctx, attempt)
	}
	return nil
}

// ReviewLoopChangedKind marks the EventWorkflowChanged a run's loop
// publishes; the web server forwards those to open run pages.
const ReviewLoopChangedKind = "review_loop"
