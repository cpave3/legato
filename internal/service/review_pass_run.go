package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
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

// ReviewRunDiscover looks again for the pull requests of a repository's
// tasks (PRTrackingService's DiscoverRepository).
type ReviewRunDiscover interface {
	DiscoverRepository(ctx context.Context, repo string) error
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
	// Discover is PR tracking, asked to look again in the repository when
	// an agent-authored loop finds no task linked to its pull request: an
	// agent may have opened it a moment ago. Nil: no second look.
	Discover ReviewRunDiscover
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
	stops     runAuthorStops
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
	// AuthorTask is the board task whose agent is the author (agent_author);
	// empty for the branch's owner.
	AuthorTask string `json:"author_task,omitempty"`
	// SeenHead is the pull request's head the loop last saw move, and
	// SeenAt when: the next pass waits for it to stay still for the quiet
	// time.
	SeenHead string `json:"seen_head,omitempty"`
	SeenAt   string `json:"seen_at,omitempty"`
	// Replies are the thread replies the loop has taken as answers.
	Replies []int64 `json:"replies,omitempty"`
	// ConfirmClean is a clean pass whose head GitHub has not yet confirmed
	// is still the pull request's: each tick asks again.
	ConfirmClean bool `json:"confirm_clean,omitempty"`
	// Scope is stack or pr (ReviewScope*); Stack the pull requests a
	// stack run reviews, bottom first, PR among them (empty for a run
	// started before stacks: PR alone, on Branch).
	Scope string        `json:"scope,omitempty"`
	Stack []reviewRunPR `json:"stack,omitempty"`
}

var reviewRunKey = []string{"legato", "review_loop"}

func (c *ReviewRunCaller) run(ctx context.Context, taskID string) (*reviewRun, error) {
	return readReviewRun(ctx, c.engine.db, taskID)
}

// readReviewRun is the review loop a run keeps on its task's metadata.
func readReviewRun(ctx context.Context, db *store.Store, taskID string) (*reviewRun, error) {
	task, err := db.GetTask(ctx, taskID)
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
	scope, err := reviewScope(step.With)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
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
	pr.Number = number
	prs, err := c.runStack(ctx, repo, pr, scope)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	for _, p := range prs {
		if active := c.engine.ActiveLoopOn(ctx, repo, p.Number); active != nil {
			return WorkflowExecutionResult{}, active
		}
	}
	findLinked := func() string {
		linked := c.linkedTask(ctx, repo, number, pr.Branch)
		for _, p := range prs {
			if linked == "" && p.Number != number {
				// A stack's card may track another of its layers.
				linked = c.linkedTask(ctx, repo, p.Number, p.Branch)
			}
		}
		return linked
	}
	linked := findLinked()
	if agentAuthor && linked == "" && c.opts.Discover != nil {
		// The agent may have opened it since the last poll: look once more.
		if err := c.opts.Discover.DiscoverRepository(ctx, repo); err != nil {
			log.Printf("review passes: looking again for the task of %s#%d: %v", repo, number, err)
		}
		linked = findLinked()
	}
	if agentAuthor && linked == "" {
		head := pr.Branch
		if head == "" {
			head = "<head-branch>"
		}
		looked := ""
		if c.opts.Discover != nil {
			looked = ", even after Legato looked again,"
		}
		return WorkflowExecutionResult{}, fmt.Errorf("pull request #%d of %s is not linked to any board task yet%s "+
			"and agent_author needs the task whose agent wrote it: Legato starts no fresh author. "+
			"Link it to its task with legato task link <task-id> --repo %s --branch %s, or p on the task's card on the board, then run again; "+
			"or run without agent_author, and the branch's owner is the author", number, repo, looked, repo, head)
	}
	author := ""
	if agentAuthor {
		author = linked
		// One loop per author: its feedback commands find one loop.
		if other := c.authoredLoop(ctx, author); other != "" {
			return WorkflowExecutionResult{}, fmt.Errorf("%s's agent already authors the review loop of run %s (/workflows/runs/%s): one agent-authored loop runs per task", author, other, other)
		}
	}
	for _, p := range prs {
		if err := c.opts.Clones.PullHead(ctx, repo, p.Number); err != nil {
			return WorkflowExecutionResult{}, fmt.Errorf("fetching the head of #%d: %w", p.Number, err)
		}
	}
	clone, err := c.opts.Clones.JobClone(ctx, JobCloneRequest{ExecutionID: step.AttemptID, TaskID: step.TaskID, Repo: repo, Ref: pr.HeadSHA,
		FreshBranch: reviewRunBranch(number)})
	if err != nil {
		return WorkflowExecutionResult{}, fmt.Errorf("cloning %s at the head of #%d: %w", repo, number, err)
	}
	run := reviewRun{RunID: step.RunID, StepID: step.StepID, AttemptID: step.AttemptID, Repo: repo, PR: number, Branch: clone.Branch,
		CloneID: clone.ID, Dir: clone.Path, LinkedTask: linked, AuthorTask: author, Scope: scope}
	if len(prs) > 1 {
		// One branch of the clone per layer, each put at its head by
		// Checkout before every pass.
		for _, p := range prs {
			branch := reviewRunBranch(p.Number)
			if p.Number == number {
				branch = clone.Branch
			}
			run.Stack = append(run.Stack, reviewRunPR{Number: p.Number, Branch: branch})
		}
		if err := c.engine.db.RetitleWorkflowRun(ctx, step.RunID, "Review loop on "+reviewStackName(repo, run.numbers())); err != nil {
			log.Printf("review passes: naming run %s after its stack: %v", step.RunID, err)
		}
	}
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

// linkedTask is the board task the pull request belongs to, or "": a task
// that tracks it on one of its branches first (a stack's middle pull request
// too), else owner lookup.
func (c *ReviewRunCaller) linkedTask(ctx context.Context, repo string, number int, branch string) string {
	if tracked, err := c.engine.db.ListTrackedTaskBranches(ctx); err == nil {
		ids := make([]string, 0, len(tracked))
		for id := range tracked {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			for _, b := range tracked[id] {
				if b.PR != nil && b.PR.PRNumber == number && strings.EqualFold(b.Repo, repo) {
					return id
				}
			}
		}
	}
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
	// An agent author handles failing checks too (LEG-305); a person's
	// loop is findings only (LEG-255).
	return ReviewLoop{TaskID: taskID, Repo: run.Repo, PR: run.PR, PRs: run.numbers(), AuthorTaskID: run.AuthorTask, Pending: run.ConfirmClean, CountChecks: run.AuthorTask != "",
		Name: "run " + run.RunID, Link: "/workflows/runs/" + run.RunID}, true
}

// Checkout puts each of the run's pull requests still open at its head on
// GitHub now, on its branch of the clone, bottom of the stack first (so
// HEAD ends at the top), and names them; none open leaves nothing to
// review.
func (c *ReviewRunCaller) Checkout(ctx context.Context, l ReviewLoop) (string, []ReviewGitHubPR, error) {
	run, err := c.run(ctx, l.TaskID)
	if err != nil {
		return "", nil, &ReviewCheckoutError{Err: err}
	}
	branches := map[int]string{}
	for _, p := range run.prs() {
		branches[p.Number] = p.Branch
	}
	open, err := c.openRunPRs(ctx, run)
	if err != nil {
		return "", nil, err
	}
	for i, pr := range open {
		if err := c.opts.Clones.PullHead(ctx, run.Repo, pr.Number); err != nil {
			return "", nil, err
		}
		if err := c.opts.Clones.Reset(ctx, run.Dir, branches[pr.Number], pr.HeadSHA); err != nil {
			return "", nil, &ReviewCheckoutError{Err: err}
		}
		open[i].Branch = branches[pr.Number]
	}
	return run.Dir, open, nil
}

// OpenPRs are the run's pull requests still open.
func (c *ReviewRunCaller) OpenPRs(ctx context.Context, l ReviewLoop) ([]ReviewGitHubPR, error) {
	run, err := c.run(ctx, l.TaskID)
	if err != nil {
		return nil, err
	}
	return c.openRunPRs(ctx, run)
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
	c.noteAuthor(ctx, l.TaskID, run)
	if run.ConfirmClean {
		c.confirmClean(ctx, l.TaskID)
		return false
	}
	open, err := c.openRunPRs(ctx, run)
	if err != nil {
		return false // GitHub may answer next time
	}
	if len(open) == 0 {
		c.finish(ctx, l.TaskID, ReviewOutcomeClosed)
		return false
	}
	pushed := moved(open, c.pinnedHeads(ctx, l.TaskID))
	now := c.engine.opts.Now()
	if l.AuthorTaskID != "" && c.agentDone(ctx, l, pushed) {
		return true
	}
	if pushed {
		if heads := headsKey(open); heads != run.SeenHead {
			// A push, or another one to any layer (a restack moves
			// several): the quiet time starts again.
			run.SeenHead, run.SeenAt = heads, now.UTC().Format(time.RFC3339)
			_ = c.engine.db.SetTaskMetadata(ctx, l.TaskID, reviewRunKey, run)
			return false
		}
		seen, err := time.Parse(time.RFC3339, run.SeenAt)
		return err == nil && now.Sub(seen) >= c.engine.config().QuietTime
	}
	return c.disputed(ctx, l, run, open)
}

// agentDone says an agent author finished its round: idle, every finding
// that needs an answer answered, and it pushed or disputed one.
func (c *ReviewRunCaller) agentDone(ctx context.Context, l ReviewLoop, pushed bool) bool {
	if !c.engine.authorIdle(ctx, l.AuthorTaskID) {
		return false
	}
	loop, err := c.engine.db.GetHandoffReview(ctx, l.TaskID)
	if err != nil || loop == nil || loop.DeliveredPass < loop.Pass {
		return false
	}
	items, err := currentItems(ctx, c.engine.db, loop)
	if err != nil {
		return false
	}
	disputed := false
	// Only failing checks, each answered by a reply: nothing to review
	// again, and the loop can end.
	checksSettled, checks := true, 0
	for _, item := range items {
		if item.Kind == ItemCheck {
			checks++
		}
		if item.Kind != ItemNit && item.Answer == "" {
			return false
		}
		disputed = disputed || (item.Kind == ItemBlocking && item.Answer == AnswerReplied)
		checksSettled = checksSettled && (item.Kind == ItemNit || (item.Kind == ItemCheck && item.Answer == AnswerReplied))
	}
	return pushed || disputed || (checksSettled && checks > 0)
}

// disputed reads the pull request's threads, and takes a reply in the thread
// of an open blocking finding, by anyone but Legato, as the author's answer
// to it.
func (c *ReviewRunCaller) disputed(ctx context.Context, l ReviewLoop, run *reviewRun, open []ReviewGitHubPR) bool {
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
	var poll CommentPoll
	for _, pr := range open {
		got, err := c.engine.opts.GitHub.PollComments(ctx, run.Repo, pr.Number, storePollPages{db: c.engine.db, taskID: l.TaskID, pr: pr.Number})
		if err != nil {
			return false
		}
		poll.Comments = append(poll.Comments, got.Comments...)
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

// Pushed says whether a pull request's head on GitHub moved past the head
// the last pass pinned.
func (c *ReviewRunCaller) Pushed(ctx context.Context, l ReviewLoop, pinned []store.HandoffReviewPR) (bool, error) {
	run, err := c.run(ctx, l.TaskID)
	if err != nil {
		return false, err
	}
	open, err := c.openRunPRs(ctx, run)
	if err != nil {
		return false, err
	}
	heads := map[int]string{}
	for _, pr := range open {
		heads[pr.Number] = pr.HeadSHA
	}
	for _, p := range pinned {
		if head, ok := heads[p.Number]; ok && head != p.HeadSHA {
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
		if attempt.RunID == runID && attempt.Path == stepID {
			return c.anotherPass(ctx, attempt.TaskID)
		}
	}
	return ErrReviewNotWaiting
}

// Stage ends the step when a clean pass approved, or when no open pull
// request is left to review.
func (c *ReviewRunCaller) Stage(ctx context.Context, l ReviewLoop, stage string) {
	switch stage {
	case ReviewStageClean:
		c.confirmClean(ctx, l.TaskID)
	case ReviewStageNone:
		c.finish(ctx, l.TaskID, ReviewOutcomeClosed)
	}
}

// ResumeAuthor resumes an agent author that is not running from the
// conversation its main slot saved. Legato never starts a fresh one: with
// none saved, the loop goes stuck. A person cannot be resumed.
func (c *ReviewRunCaller) ResumeAuthor(ctx context.Context, l ReviewLoop) error {
	if l.AuthorTaskID == "" {
		return ErrCannotResumeAuthor
	}
	agents := c.engine.opts.Agents
	_ = agents.ReconcileSessions(ctx)
	if session, err := agents.GetAgentSession(ctx, l.AuthorTaskID); err == nil && session != nil {
		return nil
	}
	kind := ""
	if last, err := c.engine.db.LatestAgentSessionForTask(ctx, l.AuthorTaskID); err == nil && last != nil {
		kind = last.AgentKind
	}
	saved, err := agents.GetTaskResumeSessionInSlot(ctx, l.AuthorTaskID, store.SlotMain, kind)
	if err != nil {
		return fmt.Errorf("the author %s cannot be resumed: reading its saved conversation: %v", l.AuthorTaskID, err)
	}
	if saved == nil || saved.ID == "" {
		return fmt.Errorf("the author %s cannot be resumed: it has no saved conversation, and Legato starts no fresh author", l.AuthorTaskID)
	}
	opts := AgentSpawnOptions{AgentKind: kind, SessionPolicy: "resume", ResumeSessionID: saved.ID, WorkingDir: saved.CWD}
	if err := agents.SpawnAgent(ctx, l.AuthorTaskID, 0, 0, opts); err != nil {
		return fmt.Errorf("the author %s cannot be resumed: %v", l.AuthorTaskID, err)
	}
	return nil
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
	fmt.Fprintf(&b, "Review loop on %s: %s after %d %s.\n", reviewStackName(run.Repo, run.numbers()), outcome, passes, plural(passes, "pass", "passes"))
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
// shows it: its phase, the pass, a line ("Reviewing · pass 2", "Author
// fixing · 3 blocking", "Waiting for author · 3 blocking", "Approved",
// "Stuck: …"), and every review posted. NeedsYou says the loop waits on the
// operator, as the step's attention does: stuck, or waiting for an author
// that will not move it alone.
type ReviewRunDetails struct {
	Phase        string            `json:"phase"`
	Pass         int               `json:"pass"`
	Summary      string            `json:"summary"`
	BlockingOpen int               `json:"blocking_open"`
	Reason       string            `json:"reason,omitempty"`
	NeedsYou     bool              `json:"needs_you"`
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
		if run, err := c.run(ctx, attempt.TaskID); err == nil {
			state := c.authorState(ctx, run, loop)
			d.NeedsYou = c.authorNeedsYou(attempt.TaskID, state)
			if state == runAuthorFixing {
				d.Summary = "Author fixing"
			}
		}
		if d.BlockingOpen > 0 {
			d.Summary += fmt.Sprintf(" · %d blocking", d.BlockingOpen)
		}
	case ReviewPhaseClean:
		d.Summary = "Approved"
	case ReviewPhaseStuck:
		d.Summary, d.NeedsYou = "Stuck: "+loop.Reason, true
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

// Attention is why a run's loop needs the person: it is stuck, which offers
// end, or it waits for an author that will not move it alone (a person, or
// an agent author waiting on the operator, stopped or ended). A loop with an
// agent author also offers message. An agent author at work is the agent's
// wait, not the person's.
func (c *ReviewRunCaller) Attention(ctx context.Context, attempt store.WorkflowStepAttempt) (string, []string) {
	loop, err := c.engine.db.GetHandoffReview(ctx, attempt.TaskID)
	if err != nil || loop == nil {
		return "", nil
	}
	run, err := c.run(ctx, attempt.TaskID)
	if err != nil {
		return "", nil
	}
	var actions []string
	var reason string
	switch loop.Phase {
	case ReviewPhaseStuck:
		reason, actions = "Review loop stuck: "+loop.Reason, []string{"end"}
	case ReviewPhaseWaitingForAuthor:
		state := c.authorState(ctx, run, loop)
		if !c.authorNeedsYou(attempt.TaskID, state) {
			return "", nil
		}
		reason = authorReason(state)
	default:
		return "", nil
	}
	if run.AuthorTask != "" {
		actions = append(actions, "message")
	}
	return reason, actions
}

// Act takes a stuck loop's actions: another_pass reviews again (one pass
// past the limit, or a retry), end ends it with outcome stuck, so the step
// completes and the workflow goes on.
func (c *ReviewRunCaller) Act(ctx context.Context, attempt store.WorkflowStepAttempt, action, text string) error {
	switch action {
	case "message":
		return c.messageAuthor(ctx, attempt.TaskID, text)
	case "another_pass":
		return c.anotherPass(ctx, attempt.TaskID)
	case "end":
		loop, err := c.engine.db.GetHandoffReview(ctx, attempt.TaskID)
		if err != nil || loop == nil || loop.Phase != ReviewPhaseStuck {
			return ErrReviewNotStuck
		}
		c.finish(ctx, attempt.TaskID, ReviewOutcomeStuck)
		return nil
	}
	return fmt.Errorf("a review loop takes no action %q", action)
}

// anotherPass is Run another pass: at once on a loop waiting for its author,
// or past its limit on a stuck one.
func (c *ReviewRunCaller) anotherPass(ctx context.Context, taskID string) error {
	loop, err := c.engine.db.GetHandoffReview(ctx, taskID)
	if err == nil && loop != nil && loop.Phase == ReviewPhaseStuck {
		return c.engine.AnotherPass(ctx, taskID)
	}
	return c.engine.NextPass(ctx, taskID)
}

// Attention is the run caller's, once it is attached.
func (s *ReviewPassesStep) Attention(ctx context.Context, attempt store.WorkflowStepAttempt) (string, []string) {
	if caller := s.current(); caller != nil {
		return caller.Attention(ctx, attempt)
	}
	return "", nil
}

// Act is the run caller's, once it is attached.
func (s *ReviewPassesStep) Act(ctx context.Context, attempt store.WorkflowStepAttempt, action, text string) error {
	if caller := s.current(); caller != nil {
		return caller.Act(ctx, attempt, action, text)
	}
	return errors.New("review passes are off on this Legato")
}

// ReviewOperatorTag opens the operator's words relayed to an agent author.
const reviewOperatorPrefix = ReviewTag + " From the operator: "

// messageAuthor sends an agent author the operator's words, tagged, resumed
// first when it is not running.
func (c *ReviewRunCaller) messageAuthor(ctx context.Context, taskID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("a message needs text")
	}
	l, ok := c.Loop(ctx, taskID)
	if !ok || l.AuthorTaskID == "" {
		return errors.New("the author is a person, who reads GitHub")
	}
	l.caller = c
	if err := c.engine.authorRunning(ctx, l); err != nil {
		return err
	}
	return c.engine.tellAuthor(ctx, l, reviewOperatorPrefix+text)
}

// reviewLoopTask is the task whose ledger holds the loop an agent of taskID
// answers: its own (a handoff's card), else the newest run loop it authors.
func reviewLoopTask(ctx context.Context, db *store.Store, taskID string) string {
	if loop, err := db.GetHandoffReview(ctx, taskID); err == nil && loop != nil {
		return taskID
	}
	ids, err := db.TasksWithMetadataText(ctx, []string{"legato", "review_loop", "author_task"}, taskID)
	if err != nil {
		return taskID
	}
	for _, id := range ids {
		if loop, err := db.GetHandoffReview(ctx, id); err == nil && loop != nil && loop.Phase != ReviewPhaseEnded && loop.Phase != ReviewPhaseClean {
			return id
		}
	}
	return taskID
}

// authoredLoop is the run whose live loop a task's agent authors, or "".
func (c *ReviewRunCaller) authoredLoop(ctx context.Context, author string) string {
	if c.workflows == nil {
		return ""
	}
	attempts, err := c.workflows.WaitingAttempts(ctx, ReviewPassesStepType)
	if err != nil {
		return ""
	}
	for _, attempt := range attempts {
		if run, err := c.run(ctx, attempt.TaskID); err == nil && run.AuthorTask == author {
			return run.RunID
		}
	}
	return ""
}

// reopen sends a loop whose clean pass reviewed a head that moved to the
// next pass. Called with the engine's lock held (from post).
func (s *ReviewPassService) reopen(ctx context.Context, taskID string) error {
	loop, err := s.db.GetHandoffReview(ctx, taskID)
	if err != nil || loop == nil {
		return err
	}
	loop.Phase, loop.Reason = ReviewPhaseReady, ""
	return s.saveChanged(ctx, loop)
}

// confirmClean ends a clean pass only once GitHub says the pull request's
// head is still the one it reviewed: approved; a head that moved while the
// reviewer worked goes to the next pass; a closed pull request ends the loop
// closed. When GitHub does not answer, the check is kept and asked again each
// tick, and nothing is approved meanwhile.
func (c *ReviewRunCaller) confirmClean(ctx context.Context, taskID string) {
	run, err := c.run(ctx, taskID)
	if err != nil {
		log.Printf("review passes: confirming %s: %v", taskID, err)
		return
	}
	open, err := c.openRunPRs(ctx, run)
	if err != nil {
		if !run.ConfirmClean {
			run.ConfirmClean = true
			_ = c.engine.db.SetTaskMetadata(ctx, taskID, reviewRunKey, run)
		}
		log.Printf("review passes: confirming the heads of %s, trying again: %v", reviewStackName(run.Repo, run.numbers()), err)
		return
	}
	if run.ConfirmClean {
		run.ConfirmClean = false
		_ = c.engine.db.SetTaskMetadata(ctx, taskID, reviewRunKey, run)
	}
	switch {
	case len(open) == 0:
		c.finish(ctx, taskID, ReviewOutcomeClosed)
	case moved(open, c.pinnedHeads(ctx, taskID)):
		if err := c.engine.reopen(ctx, taskID); err != nil {
			log.Printf("review passes: %s moved during its pass: %v", taskID, err)
		}
	default:
		if l, ok := c.Loop(ctx, taskID); ok && l.CountChecks {
			l.caller = c
			loop, err := c.engine.db.GetHandoffReview(ctx, taskID)
			if err != nil || loop == nil {
				return
			}
			if c.engine.failingChecks(ctx, taskID) {
				// Not approved while a counted check fails: the author
				// handles it first.
				if err := c.engine.checksReopen(ctx, l, loop); err != nil {
					log.Printf("review passes: %s: %v", taskID, err)
				}
				return
			}
			if err := c.engine.approveSettled(ctx, l, loop); err != nil {
				log.Printf("review passes: %s: %v", taskID, err)
			}
		}
		c.finish(ctx, taskID, ReviewOutcomeApproved)
	}
}
