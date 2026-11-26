package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// The loop record: the review loop engine's record of each review loop
// (spec LEG-559 § Loop record schema and claims). A loop holds a claim on
// each of its pull requests from open to close; every change of a loop is
// one compare-and-swap transaction on its rev. See docs/ai/database.md
// § Loop record.

// Owner kinds and states of a loop.
const (
	ReviewLoopOwnerRun     = "run"
	ReviewLoopOwnerHandoff = "handoff"

	ReviewLoopOpenState   = "open"
	ReviewLoopClosedState = "closed"
)

// States of a pass. reviewing, submitted and posting are in flight: they
// hold one of the instance's max_concurrent slots.
const (
	ReviewLoopPassReviewing = "reviewing"
	ReviewLoopPassSubmitted = "submitted"
	ReviewLoopPassPosting   = "posting"
	ReviewLoopPassPosted    = "posted"
	ReviewLoopPassDiscarded = "discarded"
	ReviewLoopPassUnable    = "unable"
)

const reviewLoopInFlight = `('reviewing', 'submitted', 'posting')`

var (
	// ErrReviewLoopActive is a pull request another loop holds
	// (review_loop_active).
	ErrReviewLoopActive = errors.New("another review loop holds this pull request")
	// ErrAuthorLoopActive is an author task whose agent already authors an
	// open loop (author_loop_active).
	ErrAuthorLoopActive = errors.New("this task's agent already authors an open review loop")
	// ErrRunLoopActive is a workflow run that already has an open loop.
	ErrRunLoopActive = errors.New("this workflow run already has an open review loop")
	// ErrReviewLoopClosed is a write to a closed loop (loop_closed).
	ErrReviewLoopClosed = errors.New("the review loop is closed")
	// ErrReviewLoopNotOwned is a loop another owner holds (loop_not_owned).
	ErrReviewLoopNotOwned = errors.New("the review loop belongs to another owner")
	// ErrReviewSlotsFull is a pass refused because max_concurrent passes
	// are in flight.
	ErrReviewSlotsFull = errors.New("every review slot is taken")
)

// ReviewLoopConflictError is a refusal that names the loop in the way: Err
// is ErrReviewLoopActive (with Repo and PR), ErrAuthorLoopActive (with
// AuthorTask) or ErrRunLoopActive.
type ReviewLoopConflictError struct {
	Err        error
	LoopID     string
	Repo       string
	PR         int
	AuthorTask string
}

func (e *ReviewLoopConflictError) Error() string {
	switch {
	case e.PR != 0:
		return fmt.Sprintf("%v: %s#%d is held by %s", e.Err, e.Repo, e.PR, e.LoopID)
	case e.AuthorTask != "":
		return fmt.Sprintf("%v: %s's agent authors %s", e.Err, e.AuthorTask, e.LoopID)
	}
	return fmt.Sprintf("%v: %s", e.Err, e.LoopID)
}

func (e *ReviewLoopConflictError) Unwrap() error { return e.Err }

// NewReviewLoopID returns a loop ID: rl- and 12 hex characters.
func NewReviewLoopID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("reading random bytes: %v", err))
	}
	return "rl-" + hex.EncodeToString(b)
}

// ReviewLoop is one loop record.
type ReviewLoop struct {
	ID  string `db:"id"`
	Rev int64  `db:"rev"`
	// The owner: a workflow run's step, or a handoff.
	OwnerKind string `db:"owner_kind"`
	RunID     string `db:"run_id"`
	StepPath  string `db:"step_path"`
	AttemptID string `db:"attempt_id"`
	// The handoff binding: the handoff and its card's columns (JSON).
	HandoffID      string `db:"handoff_id"`
	HandoffColumns string `db:"handoff_columns"`
	// MainInstanceID is the main instance of the handoff (handoff IDs are
	// unique per main).
	MainInstanceID string `db:"main_instance_id"`
	// HostTask holds the reviewer slot and the clone: the workflow run's
	// task, or the handoff's card.
	HostTask string `db:"host_task"`
	Repo     string `db:"repo"`
	// SubjectPR is the pull request the loop was opened on, 0 when given a
	// list; Scope stack or pr ("" for a handoff's).
	SubjectPR   int    `db:"subject_pr"`
	Scope       string `db:"scope"`
	AgentAuthor bool   `db:"agent_author"`
	AuthorTask  string `db:"author_task"`
	LinkedTask  string `db:"linked_task"`
	State       string `db:"state"`
	Phase       string `db:"phase"`
	Outcome     string `db:"outcome"`
	Reason      string `db:"reason"`
	// The settings. PassesOff is a handoff whose main turned its review
	// passes off.
	PassesOff     bool   `db:"passes_off"`
	MaxPasses     int    `db:"max_passes"`
	MaxReentries  int    `db:"max_reentries"`
	Ready         bool   `db:"ready"`
	Steward       bool   `db:"steward"`
	Reviewer      string `db:"reviewer"`
	ReviewerModel string `db:"reviewer_model"`
	ReviewerTier  string `db:"reviewer_tier"`
	Skill         string `db:"skill"`
	SkillManaged  bool   `db:"skill_managed"`
	// The counters: the current pass, the pass the window began at,
	// re-entries used, passes since approval, deaths of the current pass,
	// the not-converging run, head mismatches, the last pass the author was
	// told of, and the last wake.
	Pass                int    `db:"pass"`
	WindowStart         int    `db:"window_start"`
	Reentries           int    `db:"reentries"`
	PassesSinceApproval int    `db:"passes_since_approval"`
	PassDeaths          int    `db:"pass_deaths"`
	NotConverging       int    `db:"not_converging"`
	Mismatches          int    `db:"mismatches"`
	DeliveredPass       int    `db:"delivered_pass"`
	LastWake            string `db:"last_wake"`
	LastWakeAt          string `db:"last_wake_at"`
	Held                bool   `db:"held"`
	IdleDeadline        string `db:"idle_deadline"`
	ConfirmedAt         string `db:"confirmed_at"`
	CleanPass           int    `db:"clean_pass"`
	// ConfirmClean is a clean pass GitHub has not yet confirmed at its heads.
	ConfirmClean         bool   `db:"confirm_clean"`
	NudgedAt             string `db:"nudged_at"`
	AuthorBusySinceNudge bool   `db:"author_busy_since_nudge"`
	SeenHead             string `db:"seen_head"`
	SeenAt               string `db:"seen_at"`
	// Replies are the thread replies taken as answers (JSON list of IDs).
	Replies string `db:"replies"`
	// The comment and check polls.
	PolledAt        string `db:"polled_at"`
	LastNewAt       string `db:"last_new_at"`
	SkippedComments int    `db:"skipped_comments"`
	ChecksPolledAt  string `db:"checks_polled_at"`
	ChecksHeads     string `db:"checks_heads"`
	ChecksPending   int    `db:"checks_pending"`
	// MarkerKey keys the markers of what the loop leaves on GitHub: the
	// loop ID, so two loops on one task never share one, or the old task
	// ID of a converted loop, whose earlier posts are found by it. Pin refs
	// keep the host task's ID.
	MarkerKey string `db:"marker_key"`
	CloneID   int64  `db:"clone_id"`
	CloneDir  string `db:"clone_dir"`
	QueuedAt  string `db:"queued_at"`
	CreatedAt string `db:"created_at"`
	UpdatedAt string `db:"updated_at"`
	ClosedAt  string `db:"closed_at"`
}

// ReviewLoopPR is a layer of a loop, in stack order (Position).
type ReviewLoopPR struct {
	LoopID   string `db:"loop_id"`
	Number   int    `db:"number"`
	Position int    `db:"position"`
	Branch   string `db:"branch"`
	Base     string `db:"base"`
	Title    string `db:"title"`
	URL      string `db:"url"`
	HeadRepo string `db:"head_repo"`
	// State is open, closed or merged.
	State string `db:"state"`
	Draft bool   `db:"draft"`
	// Mergeable is the steward's word for whether it can merge, as its
	// watch last read it: MERGEABLE, CONFLICTING, UNKNOWN or BEHIND;
	// "" before a watch read it.
	Mergeable string `db:"mergeable"`
	// Author is its author on GitHub, AuthorAssociation GitHub's word for
	// them, AuthorTask the board task whose agent wrote it.
	Author            string `db:"author"`
	AuthorAssociation string `db:"author_association"`
	AuthorTask        string `db:"author_task"`
	// PinnedHead is the head the last pass pinned, CleanHead the head a
	// clean pass was confirmed at.
	PinnedHead string `db:"pinned_head"`
	CleanHead  string `db:"clean_head"`
	ReviewID   int64  `db:"review_id"`
	ReviewURL  string `db:"review_url"`
	ClaimedAt  string `db:"claimed_at"`
	// LocalBranch is the branch of the reviewer's clone the layer is put
	// on, when it is not Branch.
	LocalBranch string `db:"local_branch"`
}

// ReviewLoopPin is one pull request of a pass, pinned at its head, and the
// review the pass posted on it.
type ReviewLoopPin struct {
	Number      int    `json:"number"`
	Repo        string `json:"repo,omitempty"`
	Branch      string `json:"branch,omitempty"`
	Base        string `json:"base,omitempty"`
	Title       string `json:"title,omitempty"`
	Body        string `json:"body,omitempty"`
	URL         string `json:"url,omitempty"`
	HeadSHA     string `json:"head_sha"`
	Author      string `json:"author,omitempty"`
	LocalBranch string `json:"local_branch,omitempty"`
	HeadRepo    string `json:"head_repo,omitempty"`
	ReviewID    int64  `json:"review_id,omitempty"`
	ReviewURL   string `json:"review_url,omitempty"`
	// Event is APPROVE or COMMENT as posted, kept for an approval that
	// stands, "" before posting.
	Event string `json:"event,omitempty"`
}

// ReviewLoopPins are a pass's pins, kept as JSON.
type ReviewLoopPins []ReviewLoopPin

// Value implements driver.Valuer.
func (p ReviewLoopPins) Value() (driver.Value, error) {
	if p == nil {
		return "[]", nil
	}
	b, err := json.Marshal(p)
	return string(b), err
}

// Scan implements sql.Scanner.
func (p *ReviewLoopPins) Scan(src any) error {
	var b []byte
	switch v := src.(type) {
	case string:
		b = []byte(v)
	case []byte:
		b = v
	case nil:
		*p = nil
		return nil
	default:
		return fmt.Errorf("pins: unexpected %T", src)
	}
	return json.Unmarshal(b, p)
}

// ReviewLoopPass is one pass of a loop.
type ReviewLoopPass struct {
	LoopID           string         `db:"loop_id"`
	Pass             int            `db:"pass"`
	Window           int            `db:"pass_window"`
	StepPath         string         `db:"step_path"`
	State            string         `db:"state"`
	Verdict          string         `db:"verdict"`
	Summary          string         `db:"summary"`
	Reason           string         `db:"reason"`
	ExecutionID      string         `db:"execution_id"`
	Checkout         string         `db:"checkout"`
	HeadSHA          string         `db:"head_sha"`
	Pins             ReviewLoopPins `db:"pins"`
	SubmissionDigest string         `db:"submission_digest"`
	PostingStartedAt string         `db:"posting_started_at"`
	Attempts         int            `db:"attempts"`
	// Attempt is how many times the pass's number was pinned before: a pass
	// discarded because a head moved is pinned again under its number.
	Attempt   int    `db:"attempt"`
	StartedAt string `db:"started_at"`
	EndedAt   string `db:"ended_at"`
}

// ReviewLoopFinding is one finding of a pass, with a later pass's ruling.
type ReviewLoopFinding struct {
	LoopID    string `db:"loop_id"`
	ID        string `db:"id"`
	Pass      int    `db:"pass"`
	PR        int    `db:"pr"`
	Severity  string `db:"severity"`
	Critical  bool   `db:"critical"`
	Title     string `db:"title"`
	Body      string `db:"body"`
	Path      string `db:"path"`
	Line      int    `db:"line"`
	StartLine int    `db:"start_line"`
	Side      string `db:"side"`
	CommentID int64  `db:"comment_id"`
	Status    string `db:"status"`
	RuledPass int    `db:"ruled_pass"`
	Ruling    string `db:"ruling"`
}

// ReviewLoopItem is one thing the author must handle (a finding, a counted
// comment, a failing check, mergeability, a merged layer), and its answer.
type ReviewLoopItem struct {
	LoopID          string `db:"loop_id" json:"-"`
	ID              string `db:"id" json:"id"`
	Kind            string `db:"kind" json:"kind"`
	Pass            int    `db:"pass" json:"pass"`
	Repo            string `db:"repo" json:"repo,omitempty"`
	PR              int    `db:"pr" json:"pr,omitempty"`
	Path            string `db:"path" json:"path,omitempty"`
	Line            int    `db:"line" json:"line,omitempty"`
	URL             string `db:"url" json:"url,omitempty"`
	Version         int    `db:"version" json:"version"`
	Outdated        bool   `db:"outdated" json:"outdated,omitempty"`
	Author          string `db:"author" json:"author,omitempty"`
	Title           string `db:"title" json:"title,omitempty"`
	Body            string `db:"body" json:"body,omitempty"`
	ThreadCommentID int64  `db:"thread_comment_id" json:"-"`
	SourceID        string `db:"source_id" json:"-"`
	Answer          string `db:"answer" json:"answer,omitempty"`
	AnswerNote      string `db:"answer_note" json:"answer_note,omitempty"`
	AnsweredAt      string `db:"answered_at" json:"answered_at,omitempty"`
	// AnswerMarker marks the answer's post, so a retry finds it.
	AnswerMarker string `db:"answer_marker" json:"-"`
	Posted       bool   `db:"posted" json:"posted,omitempty"`
	Closed       bool   `db:"closed" json:"-"`
	Delivered    bool   `db:"delivered" json:"-"`
	CreatedAt    string `db:"created_at" json:"-"`
}

// ReviewLoopOpen is a loop to open with its pull requests, bottom first.
// YieldAuthor opens an agent-authored loop whose author already authors an
// open loop as not the agent's (agent_author false) instead of refusing it:
// a handed-off card's loop, which reviews whoever authors it.
type ReviewLoopOpen struct {
	Loop        ReviewLoop
	PRs         []ReviewLoopPR
	YieldAuthor bool
}

// ReviewLoopOwner names who may write to a loop.
// A run owner may name its step attempt (AttemptID), and a handoff owner
// its main instance (MainInstanceID, as handoff IDs are unique per main);
// each, when set, must match too.
type ReviewLoopOwner struct {
	Kind           string
	RunID          string
	AttemptID      string
	HandoffID      string
	MainInstanceID string
}

func (o ReviewLoopOwner) owns(l *ReviewLoop) bool {
	if o.Kind != l.OwnerKind {
		return false
	}
	if o.Kind == ReviewLoopOwnerRun {
		return o.RunID == l.RunID && (o.AttemptID == "" || o.AttemptID == l.AttemptID)
	}
	return o.HandoffID == l.HandoffID && (o.MainInstanceID == "" || o.MainInstanceID == l.MainInstanceID)
}

func reviewLoopNow() string { return time.Now().UTC().Format(time.RFC3339) }

func getReviewLoop(ctx context.Context, q sqlx.QueryerContext, id string) (*ReviewLoop, error) {
	var l ReviewLoop
	err := sqlx.GetContext(ctx, q, &l, `SELECT * FROM review_loops WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// OpenReviewLoop opens a loop and claims its pull requests in one
// transaction. It is idempotent per owner: a run's step (run, step path)
// or a handoff (main instance, handoff) that opened a loop before gets it
// back, open or closed, with created false; another handoff of the same
// card is another owner, with a loop of its own. A pull request another
// loop holds, an author task whose agent authors another open loop
// (unless YieldAuthor), or a run with another open loop is refused with a
// *ReviewLoopConflictError.
func (s *Store) OpenReviewLoop(ctx context.Context, open ReviewLoopOpen) (*ReviewLoop, bool, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	loop, created, err := openReviewLoopTx(ctx, tx, open)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return loop, created, nil
}

// ownedReviewLoop is the newest loop of an owner, nil when it has none.
func ownedReviewLoop(ctx context.Context, tx *sqlx.Tx, l ReviewLoop) (*ReviewLoop, error) {
	var existing string
	var err error
	switch l.OwnerKind {
	case ReviewLoopOwnerRun:
		err = tx.GetContext(ctx, &existing, `SELECT id FROM review_loops WHERE run_id = ? AND step_path = ? AND owner_kind = 'run'
			ORDER BY created_at DESC, rowid DESC LIMIT 1`, l.RunID, l.StepPath)
	default:
		err = tx.GetContext(ctx, &existing, `SELECT id FROM review_loops WHERE main_instance_id = ? AND handoff_id = ? AND owner_kind = 'handoff'
			AND host_task = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, l.MainInstanceID, l.HandoffID, l.HostTask)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return getReviewLoop(ctx, tx, existing)
}

func openReviewLoopTx(ctx context.Context, tx *sqlx.Tx, open ReviewLoopOpen) (*ReviewLoop, bool, error) {
	l := open.Loop
	if l.OwnerKind != ReviewLoopOwnerRun && l.OwnerKind != ReviewLoopOwnerHandoff {
		return nil, false, fmt.Errorf("review loop owner kind %q", l.OwnerKind)
	}
	if l.OwnerKind == ReviewLoopOwnerRun || l.HandoffID != "" {
		existing, err := ownedReviewLoop(ctx, tx, l)
		if err != nil || existing != nil {
			return existing, false, err
		}
	}
	if l.OwnerKind == ReviewLoopOwnerRun {
		var existing string
		if err := tx.GetContext(ctx, &existing, `SELECT id FROM review_loops WHERE run_id = ? AND owner_kind = 'run' AND state = 'open'`, l.RunID); err == nil {
			return nil, false, &ReviewLoopConflictError{Err: ErrRunLoopActive, LoopID: existing}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, false, err
		}
	}
	if l.AgentAuthor && l.AuthorTask != "" {
		var other string
		err := tx.GetContext(ctx, &other, `SELECT id FROM review_loops WHERE author_task = ? AND agent_author = 1 AND state = 'open'`, l.AuthorTask)
		switch {
		case err == nil && open.YieldAuthor:
			l.AgentAuthor = false
		case err == nil:
			return nil, false, &ReviewLoopConflictError{Err: ErrAuthorLoopActive, LoopID: other, AuthorTask: l.AuthorTask}
		case !errors.Is(err, sql.ErrNoRows):
			return nil, false, err
		}
	}
	for _, pr := range open.PRs {
		if err := checkReviewLoopClaim(ctx, tx, l.Repo, pr.Number); err != nil {
			return nil, false, err
		}
	}
	if l.ID == "" {
		l.ID = NewReviewLoopID()
	}
	if l.MarkerKey == "" {
		l.MarkerKey = l.ID
	}
	if l.CreatedAt == "" {
		l.CreatedAt = reviewLoopNow()
	}
	if l.UpdatedAt == "" {
		l.UpdatedAt = l.CreatedAt
	}
	if l.Replies == "" {
		l.Replies = "[]"
	}
	l.Rev = 1
	l.State = ReviewLoopOpenState
	l.ClosedAt = ""
	if _, err := tx.NamedExecContext(ctx, `INSERT INTO review_loops (`+reviewLoopColumns+`)
		VALUES (`+reviewLoopValues+`)`, l); err != nil {
		return nil, false, err
	}
	for i, pr := range open.PRs {
		pr.LoopID = l.ID
		pr.Position = i
		if pr.ClaimedAt == "" {
			pr.ClaimedAt = l.CreatedAt
		}
		if err := addReviewLoopPR(ctx, tx, l.Repo, pr); err != nil {
			return nil, false, err
		}
	}
	return &l, true, nil
}

// checkReviewLoopClaim refuses a pull request another loop holds.
func checkReviewLoopClaim(ctx context.Context, tx *sqlx.Tx, repo string, pr int) error {
	var holder string
	err := tx.GetContext(ctx, &holder, `SELECT loop_id FROM review_loop_claims WHERE repo = ? AND pr = ?`, repo, pr)
	if err == nil {
		return &ReviewLoopConflictError{Err: ErrReviewLoopActive, LoopID: holder, Repo: repo, PR: pr}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

// addReviewLoopPR writes a layer and its claim.
func addReviewLoopPR(ctx context.Context, tx *sqlx.Tx, repo string, pr ReviewLoopPR) error {
	if pr.State == "" {
		pr.State = "open"
	}
	if _, err := tx.NamedExecContext(ctx, `INSERT INTO review_loop_prs (`+reviewLoopPRColumns+`)
		VALUES (`+reviewLoopPRValues+`)`, pr); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO review_loop_claims (repo, pr, loop_id, claimed_at) VALUES (?, ?, ?, ?)`,
		repo, pr.Number, pr.LoopID, pr.ClaimedAt)
	return err
}

// GetReviewLoop returns a loop, ErrNotFound when there is none.
func (s *Store) GetReviewLoop(ctx context.Context, id string) (*ReviewLoop, error) {
	return getReviewLoop(ctx, s.db, id)
}

// ReviewLoopForStep returns the loop a run's step opened (the newest), nil
// when it opened none.
func (s *Store) ReviewLoopForStep(ctx context.Context, runID, stepPath string) (*ReviewLoop, error) {
	var l ReviewLoop
	err := s.db.GetContext(ctx, &l, `SELECT * FROM review_loops WHERE run_id = ? AND step_path = ? AND owner_kind = 'run'
		ORDER BY created_at DESC, rowid DESC LIMIT 1`, runID, stepPath)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &l, err
}

// ActiveReviewLoop returns the loop that holds repo's pull request (the
// repository's case ignored), nil when none does.
func (s *Store) ActiveReviewLoop(ctx context.Context, repo string, pr int) (*ReviewLoop, error) {
	var l ReviewLoop
	err := s.db.GetContext(ctx, &l, `SELECT l.* FROM review_loop_claims c JOIN review_loops l ON l.id = c.loop_id
		WHERE c.repo = ? AND c.pr = ?`, repo, pr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &l, err
}

// ListOpenReviewLoops returns the open loops, oldest queued first.
func (s *Store) ListOpenReviewLoops(ctx context.Context) ([]ReviewLoop, error) {
	var out []ReviewLoop
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM review_loops WHERE state = 'open' ORDER BY queued_at, created_at, id`)
	return out, err
}

// CheckReviewLoopOwner is the re-check before a GitHub write: the loop as
// it stands, ErrNotFound, ErrReviewLoopClosed or ErrReviewLoopNotOwned.
func (s *Store) CheckReviewLoopOwner(ctx context.Context, id string, owner ReviewLoopOwner) (*ReviewLoop, error) {
	l, err := getReviewLoop(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if !owner.owns(l) {
		return l, ErrReviewLoopNotOwned
	}
	if l.State != ReviewLoopOpenState {
		return l, ErrReviewLoopClosed
	}
	return l, nil
}

// ReviewLoopTx is one compare-and-swap transition of a loop. Loop is the
// loop as read: change it, and UpdateReviewLoop writes it when the change
// returns. Its state, ID and creation are not written: Close closes a loop.
type ReviewLoopTx struct {
	Loop *ReviewLoop
	ctx  context.Context
	tx   *sqlx.Tx
}

// UpdateReviewLoop runs change as one transaction on loop id at rev: a
// stale rev is ErrConflict and a closed loop ErrReviewLoopClosed, and
// either changes nothing; an error from change rolls back every write it
// made. It returns the loop as written, its rev moved on.
func (s *Store) UpdateReviewLoop(ctx context.Context, id string, rev int64, change func(*ReviewLoopTx) error) (*ReviewLoop, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	l, err := getReviewLoop(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if l.State != ReviewLoopOpenState {
		return nil, ErrReviewLoopClosed
	}
	if l.Rev != rev {
		return nil, fmt.Errorf("%w: review loop %s is at rev %d, not %d", ErrConflict, id, l.Rev, rev)
	}
	read := *l
	rt := &ReviewLoopTx{Loop: l, ctx: ctx, tx: tx}
	if err := change(rt); err != nil {
		return nil, err
	}
	if err := writeReviewLoop(ctx, tx, read, *rt.Loop); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return getReviewLoop(ctx, s.db, id)
}

// writeReviewLoop writes next over the loop read, a compare-and-swap on
// read's rev: a loop that moved on since is ErrConflict.
func writeReviewLoop(ctx context.Context, tx *sqlx.Tx, read, next ReviewLoop) error {
	next.ID = read.ID
	next.Rev = read.Rev + 1
	if next.UpdatedAt == read.UpdatedAt || next.UpdatedAt == "" {
		next.UpdatedAt = reviewLoopNow()
	}
	if next.Replies == "" {
		next.Replies = "[]"
	}
	result, err := tx.NamedExecContext(ctx, `UPDATE review_loops SET `+reviewLoopUpdates+`, rev = :rev WHERE id = :id AND rev = `+fmt.Sprint(read.Rev), next)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("%w: review loop %s moved on", ErrConflict, read.ID)
	}
	if (next.OwnerKind == ReviewLoopOwnerHandoff || next.HandoffID != "") && handoffLoopLetsGo(next) {
		// A handoff's loop (its own, or a review-loop run's bound to it)
		// that approved, had nothing to review or ended lets go of its
		// pull requests while its card stays open; a later pass claims
		// them again (holdReviewLoopLayer). A clean pass Confirm has yet
		// to record approved still holds them.
		if _, err := tx.ExecContext(ctx, `DELETE FROM review_loop_claims WHERE loop_id = ?`, read.ID); err != nil {
			return err
		}
	}
	return nil
}

// handoffLoopLetsGo says a handoff loop holds no claims: approved (clean,
// and Confirm recorded this pass confirmed), skipped (nothing to review) or
// ended.
func handoffLoopLetsGo(l ReviewLoop) bool {
	switch l.Phase {
	case "clean":
		return !l.ConfirmClean && l.ConfirmedAt != "" && l.CleanPass == l.Pass
	case "skipped", "ended":
		return true
	}
	return false
}

// SetLocalBranch records the branch of the reviewer's clone a layer is put
// on.
func (t *ReviewLoopTx) SetLocalBranch(number int, branch string) error {
	result, err := t.tx.ExecContext(t.ctx, `UPDATE review_loop_prs SET local_branch = ? WHERE loop_id = ? AND number = ?`, branch, t.Loop.ID, number)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddPR adds a layer to the loop and claims it, after the layers it has. A
// pull request another loop holds is a *ReviewLoopConflictError naming it,
// and nothing is written.
func (t *ReviewLoopTx) AddPR(pr ReviewLoopPR) error {
	if err := checkReviewLoopClaim(t.ctx, t.tx, t.Loop.Repo, pr.Number); err != nil {
		return err
	}
	var next int
	if err := t.tx.GetContext(t.ctx, &next, `SELECT COALESCE(MAX(position) + 1, 0) FROM review_loop_prs WHERE loop_id = ?`, t.Loop.ID); err != nil {
		return err
	}
	pr.LoopID = t.Loop.ID
	pr.Position = next
	if pr.ClaimedAt == "" {
		pr.ClaimedAt = reviewLoopNow()
	}
	return addReviewLoopPR(t.ctx, t.tx, t.Loop.Repo, pr)
}

// SavePR writes a layer's fields; its claim and position stay.
func (t *ReviewLoopTx) SavePR(pr ReviewLoopPR) error {
	pr.LoopID = t.Loop.ID
	result, err := t.tx.NamedExecContext(t.ctx, `UPDATE review_loop_prs SET branch = :branch, base = :base, title = :title, url = :url,
		head_repo = :head_repo, state = :state, draft = :draft, mergeable = :mergeable, author = :author,
		author_association = :author_association, author_task = :author_task, pinned_head = :pinned_head,
		clean_head = :clean_head, review_id = :review_id, review_url = :review_url, local_branch = :local_branch
		WHERE loop_id = :loop_id AND number = :number`, pr)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// StartPass takes one of maxConcurrent review slots for the loop's next
// pass and records it in flight: the slots are the in-flight passes of
// open loops, counted and taken in this transaction. Every slot taken is
// ErrReviewSlotsFull.
func (t *ReviewLoopTx) StartPass(p ReviewLoopPass, maxConcurrent int) error {
	if err := takeReviewSlot(t.ctx, t.tx, maxConcurrent); err != nil {
		return err
	}
	p.LoopID = t.Loop.ID
	if p.State == "" {
		p.State = ReviewLoopPassReviewing
	}
	if p.StartedAt == "" {
		p.StartedAt = reviewLoopNow()
	}
	_, err := t.tx.NamedExecContext(t.ctx, `INSERT INTO review_loop_passes (`+reviewLoopPassColumns+`) VALUES (`+reviewLoopPassValues+`)`, p)
	return err
}

// takeReviewSlot is the slot check of a pass start, run in the
// transaction that writes the pass: ErrReviewSlotsFull when maxConcurrent
// passes of open loops are in flight. The transaction is immediate, so no
// other writer counts or takes a slot between this count and the pass's
// insert.
func takeReviewSlot(ctx context.Context, q sqlx.QueryerContext, maxConcurrent int) error {
	inFlight, err := reviewPassesInFlight(ctx, q)
	if err != nil {
		return err
	}
	if inFlight >= maxConcurrent {
		return ErrReviewSlotsFull
	}
	return nil
}

func reviewPassesInFlight(ctx context.Context, q sqlx.QueryerContext) (int, error) {
	var n int
	err := sqlx.GetContext(ctx, q, &n, `SELECT COUNT(*) FROM review_loop_passes p JOIN review_loops l ON l.id = p.loop_id
		WHERE l.state = 'open' AND p.state IN `+reviewLoopInFlight)
	return n, err
}

// ReviewPassesInFlight counts the review slots taken: the in-flight passes
// (reviewing, submitted, posting) of open loops. It is a read for deciding
// whether to try a start; the start itself takes its slot atomically.
func (s *Store) ReviewPassesInFlight(ctx context.Context) (int, error) {
	return reviewPassesInFlight(ctx, s.db)
}

// SavePass writes a pass's state and what it recorded.
func (t *ReviewLoopTx) SavePass(p ReviewLoopPass) error {
	p.LoopID = t.Loop.ID
	result, err := t.tx.NamedExecContext(t.ctx, `UPDATE review_loop_passes SET pass_window = :pass_window, step_path = :step_path,
		state = :state, verdict = :verdict, summary = :summary, reason = :reason, execution_id = :execution_id,
		checkout = :checkout, head_sha = :head_sha, pins = :pins, submission_digest = :submission_digest,
		posting_started_at = :posting_started_at, attempts = :attempts, attempt = :attempt, ended_at = :ended_at
		WHERE loop_id = :loop_id AND pass = :pass`, p)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ReplaceFindings sets a pass's findings, dropping any recorded for it
// before (a submission refused, then made again).
func (t *ReviewLoopTx) ReplaceFindings(pass int, findings []ReviewLoopFinding) error {
	if _, err := t.tx.ExecContext(t.ctx, `DELETE FROM review_loop_findings WHERE loop_id = ? AND pass = ?`, t.Loop.ID, pass); err != nil {
		return err
	}
	for _, f := range findings {
		f.LoopID = t.Loop.ID
		f.Pass = pass
		if f.Status == "" {
			f.Status = "open"
		}
		if _, err := t.tx.NamedExecContext(t.ctx, `INSERT INTO review_loop_findings (`+reviewLoopFindingColumns+`)
			VALUES (`+reviewLoopFindingValues+`)`, f); err != nil {
			return err
		}
	}
	return nil
}

// SaveFinding writes a finding's thread and ruling.
func (t *ReviewLoopTx) SaveFinding(f ReviewLoopFinding) error {
	f.LoopID = t.Loop.ID
	result, err := t.tx.NamedExecContext(t.ctx, `UPDATE review_loop_findings SET comment_id = :comment_id, status = :status,
		ruled_pass = :ruled_pass, ruling = :ruling WHERE loop_id = :loop_id AND id = :id`, f)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpsertItem writes an item of the loop: a new one, or one raised again.
func (t *ReviewLoopTx) UpsertItem(item ReviewLoopItem) error {
	item.LoopID = t.Loop.ID
	return upsertReviewLoopItem(t.ctx, t.tx, item)
}

// UpsertReviewLoopItem writes an item of a loop outside a transition: a
// new one, or one raised again (its answer cleared, its version moved on).
func (s *Store) UpsertReviewLoopItem(ctx context.Context, item ReviewLoopItem) error {
	return upsertReviewLoopItem(ctx, s.db, item)
}

func upsertReviewLoopItem(ctx context.Context, e sqlx.ExtContext, item ReviewLoopItem) error {
	if item.CreatedAt == "" {
		item.CreatedAt = reviewLoopNow()
	}
	if item.Version == 0 {
		item.Version = 1
	}
	_, err := sqlx.NamedExecContext(ctx, e, `INSERT INTO review_loop_items (`+reviewLoopItemColumns+`) VALUES (`+reviewLoopItemValues+`)
		ON CONFLICT(loop_id, id) DO UPDATE SET kind = excluded.kind, pass = excluded.pass, repo = excluded.repo, pr = excluded.pr,
			path = excluded.path, line = excluded.line, url = excluded.url, version = excluded.version, outdated = excluded.outdated,
			author = excluded.author, title = excluded.title, body = excluded.body, thread_comment_id = excluded.thread_comment_id,
			source_id = excluded.source_id, answer = excluded.answer, answer_note = excluded.answer_note,
			answered_at = excluded.answered_at, answer_marker = excluded.answer_marker, posted = excluded.posted,
			closed = excluded.closed, delivered = excluded.delivered`, item)
	return err
}

// GetReviewLoopItem returns one item of a loop.
func (s *Store) GetReviewLoopItem(ctx context.Context, loopID, id string) (*ReviewLoopItem, error) {
	var item ReviewLoopItem
	err := s.db.GetContext(ctx, &item, `SELECT * FROM review_loop_items WHERE loop_id = ? AND id = ?`, loopID, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &item, err
}

// ListReviewLoopItems returns a loop's items, oldest first.
func (s *Store) ListReviewLoopItems(ctx context.Context, loopID string) ([]ReviewLoopItem, error) {
	var out []ReviewLoopItem
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM review_loop_items WHERE loop_id = ? ORDER BY created_at, rowid`, loopID)
	return out, err
}

// AnswerReviewLoopItem records the author's answer to an open item.
func (s *Store) AnswerReviewLoopItem(ctx context.Context, loopID, id, answer, note, at string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE review_loop_items SET answer = ?, answer_note = ?, answered_at = ?, posted = 0
		WHERE loop_id = ? AND id = ? AND closed = 0`, answer, note, at, loopID, id)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetReviewLoopItemAnswerMarker records the marker an answer's post
// carries, before it is posted.
func (s *Store) SetReviewLoopItemAnswerMarker(ctx context.Context, loopID, id, marker string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE review_loop_items SET answer_marker = ? WHERE loop_id = ? AND id = ?`, marker, loopID, id)
	return err
}

// MarkReviewLoopItemPosted records that an item's answer is on GitHub.
func (s *Store) MarkReviewLoopItemPosted(ctx context.Context, loopID, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE review_loop_items SET posted = 1 WHERE loop_id = ? AND id = ?`, loopID, id)
	return err
}

// MarkReviewLoopItemsDelivered records that the author was told of items.
func (s *Store) MarkReviewLoopItemsDelivered(ctx context.Context, loopID string, ids []string) error {
	return withTxDB(ctx, s, func(exec func(string, ...any) error) error {
		for _, id := range ids {
			if err := exec(`UPDATE review_loop_items SET delivered = 1 WHERE loop_id = ? AND id = ?`, loopID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReviewLoopPollPage returns a list page polled before, false when none.
func (s *Store) ReviewLoopPollPage(ctx context.Context, loopID string, pr int, list string, page int) (etag, body string, ok bool) {
	var row struct {
		ETag string `db:"etag"`
		Body string `db:"body"`
	}
	if err := s.db.GetContext(ctx, &row, `SELECT etag, body FROM review_loop_poll_pages WHERE loop_id = ? AND pr = ? AND list = ? AND page = ?`, loopID, pr, list, page); err != nil {
		return "", "", false
	}
	return row.ETag, row.Body, true
}

// SaveReviewLoopPollPage keeps a list page and its ETag.
func (s *Store) SaveReviewLoopPollPage(ctx context.Context, loopID string, pr int, list string, page int, etag, body string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO review_loop_poll_pages (loop_id, pr, list, page, etag, body) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(loop_id, pr, list, page) DO UPDATE SET etag = excluded.etag, body = excluded.body`, loopID, pr, list, page, etag, body)
	return err
}

// ListReviewLoopPRs returns a loop's layers in stack order.
func (s *Store) ListReviewLoopPRs(ctx context.Context, loopID string) ([]ReviewLoopPR, error) {
	var out []ReviewLoopPR
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM review_loop_prs WHERE loop_id = ? ORDER BY position`, loopID)
	return out, err
}

// ReviewLoopPRStatus is what GitHub last reported of a layer: its state,
// whether it is a draft, and its mergeability (ReviewLoopPR.Mergeable).
type ReviewLoopPRStatus struct {
	Number    int
	State     string
	Draft     bool
	Mergeable string
}

// SetReviewLoopPRStatus writes what the steward's watch read of an open
// loop's layers, for the run page. It is GitHub's word, not a transition,
// so the loop's rev does not move; a closed loop and a number that is not
// one of its layers are left as they are.
func (s *Store) SetReviewLoopPRStatus(ctx context.Context, loopID string, layers []ReviewLoopPRStatus) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, layer := range layers {
		if _, err := tx.ExecContext(ctx, `UPDATE review_loop_prs SET state = ?, draft = ?, mergeable = ?
			WHERE loop_id = ? AND number = ? AND EXISTS (SELECT 1 FROM review_loops WHERE id = ? AND state = ?)`,
			layer.State, layer.Draft, layer.Mergeable, loopID, layer.Number, loopID, ReviewLoopOpenState); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetReviewLoopPass returns one pass, ErrNotFound when there is none.
func (s *Store) GetReviewLoopPass(ctx context.Context, loopID string, pass int) (*ReviewLoopPass, error) {
	var p ReviewLoopPass
	err := s.db.GetContext(ctx, &p, `SELECT * FROM review_loop_passes WHERE loop_id = ? AND pass = ?`, loopID, pass)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// ListReviewLoopPasses returns a loop's passes, first first.
func (s *Store) ListReviewLoopPasses(ctx context.Context, loopID string) ([]ReviewLoopPass, error) {
	var out []ReviewLoopPass
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM review_loop_passes WHERE loop_id = ? ORDER BY pass`, loopID)
	return out, err
}

// ListReviewLoopFindings returns a loop's findings, oldest pass first.
func (s *Store) ListReviewLoopFindings(ctx context.Context, loopID string) ([]ReviewLoopFinding, error) {
	var out []ReviewLoopFinding
	err := s.db.SelectContext(ctx, &out, `SELECT * FROM review_loop_findings WHERE loop_id = ? ORDER BY pass, rowid`, loopID)
	return out, err
}

// CloseReviewLoop closes a loop in one transaction: its claims released,
// it marked closed with outcome, and a pass in flight discarded. Closing a
// closed loop changes nothing (closed false). It returns the loop as it
// stands.
func (s *Store) CloseReviewLoop(ctx context.Context, id, outcome, at string) (*ReviewLoop, bool, error) {
	if at == "" {
		at = reviewLoopNow()
	}
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	l, err := getReviewLoop(ctx, tx, id)
	if err != nil {
		return nil, false, err
	}
	if l.State != ReviewLoopOpenState {
		return l, false, nil
	}
	if err := closeReviewLoopTx(ctx, tx, id, outcome, at); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	l, err = getReviewLoop(ctx, s.db, id)
	return l, true, err
}

// EndReviewLoop is CloseReviewLoop with the loop's phase first set to phase
// with reason, when phase is not "", in the same transaction.
func (s *Store) EndReviewLoop(ctx context.Context, id, phase, reason, outcome, at string) (*ReviewLoop, bool, error) {
	if at == "" {
		at = reviewLoopNow()
	}
	closed := false
	err := withTx(ctx, s.db, func(tx *sqlx.Tx) error {
		l, err := getReviewLoop(ctx, tx, id)
		if err != nil || l.State != ReviewLoopOpenState {
			return err
		}
		if phase != "" {
			next := *l
			next.Phase, next.Reason, next.QueuedAt, next.UpdatedAt = phase, reason, "", at
			if err := writeReviewLoop(ctx, tx, *l, next); err != nil {
				return err
			}
		}
		closed = true
		return closeReviewLoopTx(ctx, tx, id, outcome, at)
	})
	if err != nil {
		return nil, false, err
	}
	l, err := getReviewLoop(ctx, s.db, id)
	return l, closed, err
}

// ErrReviewLoopNotClean is a Confirm of a loop whose pass is not the clean
// one it read: it moved on (another pass, feedback) in the meantime.
var ErrReviewLoopNotClean = errors.New("the review loop is not at the clean pass it was confirmed for")

// ConfirmReviewLoop records a loop's clean pass confirmed at GitHub's heads
// (the engine's Confirm, invariant 9), in one compare-and-swap transition:
// confirmed_at and clean_pass set, the pass window reset to start after the
// pass, confirm_clean cleared, and each layer's clean_head the head it was
// confirmed at. A loop that is not open, not clean, or past pass is refused
// (ErrReviewLoopClosed, ErrReviewLoopNotClean), and nothing is written.
func (s *Store) ConfirmReviewLoop(ctx context.Context, id string, pass int, heads map[int]string, at string) error {
	if at == "" {
		at = reviewLoopNow()
	}
	var err error
	for try := 0; try < 5; try++ {
		err = withTx(ctx, s.db, func(tx *sqlx.Tx) error {
			l, err := getReviewLoop(ctx, tx, id)
			if err != nil {
				return err
			}
			if l.State != ReviewLoopOpenState {
				return ErrReviewLoopClosed
			}
			if l.Phase != "clean" || l.Pass != pass {
				return fmt.Errorf("%w: %s is %q at pass %d, not clean at pass %d", ErrReviewLoopNotClean, id, l.Phase, l.Pass, pass)
			}
			next := *l
			next.ConfirmedAt, next.CleanPass, next.WindowStart, next.ConfirmClean, next.UpdatedAt = at, pass, pass, false, at
			if err := writeReviewLoop(ctx, tx, *l, next); err != nil {
				return err
			}
			for number, head := range heads {
				if _, err := tx.ExecContext(ctx, `UPDATE review_loop_prs SET clean_head = ? WHERE loop_id = ? AND number = ?`, head, id, number); err != nil {
					return err
				}
			}
			return nil
		})
		if !errors.Is(err, ErrConflict) {
			return err
		}
	}
	return err
}

// ClosedLoopReviewer is a reviewer execution of a loop that closed: its
// host task and the execution a pass of it ran.
type ClosedLoopReviewer struct {
	LoopID      string `db:"loop_id"`
	HostTask    string `db:"host_task"`
	ExecutionID string `db:"execution_id"`
}

// ClosedLoopReviewers are the reviewer executions of the loops closed at
// or after since whose host task hosts no open loop: what Close's side
// effects stop, for a sweep after a crash between the close and them.
func (s *Store) ClosedLoopReviewers(ctx context.Context, since string) ([]ClosedLoopReviewer, error) {
	var out []ClosedLoopReviewer
	err := s.db.SelectContext(ctx, &out, `SELECT DISTINCT l.id AS loop_id, l.host_task, p.execution_id
		FROM review_loops l JOIN review_loop_passes p ON p.loop_id = l.id
		WHERE l.state = 'closed' AND l.closed_at >= ? AND p.execution_id != ''
			AND NOT EXISTS (SELECT 1 FROM review_loops o WHERE o.host_task = l.host_task AND o.state = 'open')
		ORDER BY l.closed_at, l.id`, since)
	return out, err
}

// closeReviewLoopTx closes an open loop in a transaction: its claims
// released, a pass in flight discarded, it marked closed with outcome.
func closeReviewLoopTx(ctx context.Context, tx *sqlx.Tx, id, outcome, at string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM review_loop_claims WHERE loop_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE review_loop_passes SET state = 'discarded', ended_at = ?
		WHERE loop_id = ? AND state IN `+reviewLoopInFlight, at, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE review_loops SET state = 'closed', outcome = ?, closed_at = ?, updated_at = ?, rev = rev + 1
		WHERE id = ?`, outcome, at, at, id)
	return err
}

const reviewLoopColumns = `id, rev, owner_kind, run_id, step_path, attempt_id, handoff_id, handoff_columns, main_instance_id, host_task, repo,
	subject_pr, scope, agent_author, author_task, linked_task, state, phase, outcome, reason,
	passes_off, max_passes, max_reentries, ready, steward, reviewer, reviewer_model, reviewer_tier, skill, skill_managed,
	pass, window_start, reentries, passes_since_approval, pass_deaths, not_converging, mismatches, delivered_pass,
	last_wake, last_wake_at, held, idle_deadline, confirmed_at, clean_pass, confirm_clean,
	nudged_at, author_busy_since_nudge, seen_head, seen_at, replies,
	polled_at, last_new_at, skipped_comments, checks_polled_at, checks_heads, checks_pending,
	marker_key, clone_id, clone_dir, queued_at, created_at, updated_at, closed_at`

const reviewLoopValues = `:id, :rev, :owner_kind, :run_id, :step_path, :attempt_id, :handoff_id, :handoff_columns, :main_instance_id, :host_task, :repo,
	:subject_pr, :scope, :agent_author, :author_task, :linked_task, :state, :phase, :outcome, :reason,
	:passes_off, :max_passes, :max_reentries, :ready, :steward, :reviewer, :reviewer_model, :reviewer_tier, :skill, :skill_managed,
	:pass, :window_start, :reentries, :passes_since_approval, :pass_deaths, :not_converging, :mismatches, :delivered_pass,
	:last_wake, :last_wake_at, :held, :idle_deadline, :confirmed_at, :clean_pass, :confirm_clean,
	:nudged_at, :author_busy_since_nudge, :seen_head, :seen_at, :replies,
	:polled_at, :last_new_at, :skipped_comments, :checks_polled_at, :checks_heads, :checks_pending,
	:marker_key, :clone_id, :clone_dir, :queued_at, :created_at, :updated_at, :closed_at`

// reviewLoopUpdates is what a transition writes: everything but the ID,
// rev, state and the times of creation and closing.
const reviewLoopUpdates = `owner_kind = :owner_kind, run_id = :run_id, step_path = :step_path, attempt_id = :attempt_id,
	handoff_id = :handoff_id, handoff_columns = :handoff_columns, main_instance_id = :main_instance_id, host_task = :host_task, repo = :repo,
	subject_pr = :subject_pr, scope = :scope, agent_author = :agent_author, author_task = :author_task,
	linked_task = :linked_task, phase = :phase, outcome = :outcome, reason = :reason,
	passes_off = :passes_off, max_passes = :max_passes, max_reentries = :max_reentries, ready = :ready, steward = :steward,
	reviewer = :reviewer, reviewer_model = :reviewer_model, reviewer_tier = :reviewer_tier, skill = :skill,
	skill_managed = :skill_managed, pass = :pass, window_start = :window_start, reentries = :reentries,
	passes_since_approval = :passes_since_approval, pass_deaths = :pass_deaths, not_converging = :not_converging,
	mismatches = :mismatches, delivered_pass = :delivered_pass, last_wake = :last_wake, last_wake_at = :last_wake_at,
	held = :held, idle_deadline = :idle_deadline, confirmed_at = :confirmed_at, clean_pass = :clean_pass,
	confirm_clean = :confirm_clean, nudged_at = :nudged_at, author_busy_since_nudge = :author_busy_since_nudge,
	seen_head = :seen_head, seen_at = :seen_at, replies = :replies, polled_at = :polled_at, last_new_at = :last_new_at,
	skipped_comments = :skipped_comments, checks_polled_at = :checks_polled_at, checks_heads = :checks_heads,
	checks_pending = :checks_pending, marker_key = :marker_key, clone_id = :clone_id, clone_dir = :clone_dir,
	queued_at = :queued_at, updated_at = :updated_at`

const reviewLoopPRColumns = `loop_id, number, position, branch, base, title, url, head_repo, state, draft, mergeable,
	author, author_association, author_task, pinned_head, clean_head, review_id, review_url, claimed_at, local_branch`

const reviewLoopPRValues = `:loop_id, :number, :position, :branch, :base, :title, :url, :head_repo, :state, :draft, :mergeable,
	:author, :author_association, :author_task, :pinned_head, :clean_head, :review_id, :review_url, :claimed_at, :local_branch`

const reviewLoopPassColumns = `loop_id, pass, pass_window, step_path, state, verdict, summary, reason, execution_id, checkout,
	head_sha, pins, submission_digest, posting_started_at, attempts, attempt, started_at, ended_at`

const reviewLoopPassValues = `:loop_id, :pass, :pass_window, :step_path, :state, :verdict, :summary, :reason, :execution_id, :checkout,
	:head_sha, :pins, :submission_digest, :posting_started_at, :attempts, :attempt, :started_at, :ended_at`

const reviewLoopFindingColumns = `loop_id, id, pass, pr, severity, critical, title, body, path, line, start_line, side,
	comment_id, status, ruled_pass, ruling`

const reviewLoopFindingValues = `:loop_id, :id, :pass, :pr, :severity, :critical, :title, :body, :path, :line, :start_line, :side,
	:comment_id, :status, :ruled_pass, :ruling`

const reviewLoopItemColumns = `loop_id, id, kind, pass, repo, pr, path, line, url, version, outdated, author, title, body,
	thread_comment_id, source_id, answer, answer_note, answered_at, answer_marker, posted, closed, delivered, created_at`

const reviewLoopItemValues = `:loop_id, :id, :kind, :pass, :repo, :pr, :path, :line, :url, :version, :outdated, :author, :title, :body,
	:thread_comment_id, :source_id, :answer, :answer_note, :answered_at, :answer_marker, :posted, :closed, :delivered, :created_at`
