package handofftest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cpave3/legato/internal/service"
)

// ReviewPassOptions turn a worker's review passes on, with GitHub and the
// author's checkout faked.
type ReviewPassOptions struct {
	// MaxConcurrent is how many reviewers run at once (2 when 0).
	MaxConcurrent int
	// MaxPasses is the pass limit (3 when 0).
	MaxPasses int
	// Runtime, when set, gives the review loop its configuration and the
	// advert a main's override is checked against, as a worker's runtime
	// does, in place of the harness's codex defaults. The advert is also
	// what the worker's /api/meta says.
	Runtime func(service.ReviewPassOptions) service.ReviewPassOptions
	// CountedBots are review_passes.counted_bots as configured
	// (cubic-dev-ai[bot] when nil).
	CountedBots []string
	// HoldApproval is the sign-off seam's hold (ReviewPassOptions
	// .HoldApproval): nil holds nothing.
	HoldApproval func(ctx context.Context, loopID string, pr int) bool
}

// FakeReviewGitHub is GitHub as review passes see it: open pull requests by
// branch, and the reviews, replies and resolved threads posted.
type FakeReviewGitHub struct {
	mu       sync.Mutex
	prs      map[string]service.ReviewGitHubPR
	posted   []service.ReviewPost
	replies  []string
	comments []string
	resolved []int64
	comment  int64
	// prComments are the comments on each pull request; polls counts
	// the polls of them.
	prComments map[int][]service.PRComment
	polls      int
	// pollsOf counts the polls of each repository's pull requests, and
	// pollErrors makes every poll of a repository fail with its error
	// (a rate limit, a 5xx) while set.
	pollsOf    map[string]int
	pollErrors map[string]error
	// checks are the counted checks of each head; baseChecks the checks of
	// each base branch's head.
	checks     map[string][]service.CheckResult
	baseChecks map[string][]service.CheckResult
	// closed are the pull requests closed, by number; notInstalled the
	// repositories the reviewer App cannot read.
	closed       map[int]string
	notInstalled map[string]bool
	// fetched are the pull requests whose heads were fetched for review.
	fetched []int
	// failReads makes the next PullRequest and PullRequests reads fail, as
	// GitHub's 503, once failAfter more have succeeded.
	failReads int
	failAfter int
	// markers are the hidden markers of the replies and conversation
	// comments posted; lose is how many more GitHub takes without Legato
	// hearing back.
	markers map[string]bool
	lose    int
	// pushAfter moves a pull request's head once a review of another is
	// posted: posted number -> the pull request and its new head.
	pushAfter map[int]fakePush
	// diffs are the pull request's own diff at each head: head -> file
	// -> the head's lines it changes.
	diffs map[string]map[string][][2]int
	// reviews are the reviews GitHub has, by marker; tried every post
	// Legato sent; failPosts how many more posts answer 502, GitHub
	// taking each first when failTaken.
	reviews   map[string]service.PostedReview
	tried     []service.ReviewPost
	failPosts int
	failTaken bool
	// learnLogin is told the login each review is posted as, as the
	// publisher's login recorder is.
	learnLogin func(login string)
	// refusePosts is how many more posts GitHub refuses with a 422,
	// taking nothing.
	refusePosts int
	// onRead runs once, before a pull request read once onReadSkip more
	// have gone by, outside the fake's lock (OnRead).
	onRead     func()
	onReadSkip int
	// readied are the pull requests marked ready, in order, and
	// refuseReady the one GitHub refuses to mark ready.
	readied     []int
	refuseReady int
	// watches counts the steward's watch sessions.
	watches int
	// reads counts PullRequest calls.
	reads int
	// mergeable is each pull request's mergeable and mergeStateStatus as
	// the watch reads them (MERGEABLE and CLEAN when not set).
	mergeable map[int][2]string
	// dismissed are the reviews a person dismissed, by ID (the posted
	// review's: its place in posted, from 1).
	dismissed map[int64]bool
}

// DismissReview dismisses the newest review posted on a pull request, as a
// person with write access does on GitHub (no push): the watch reads it
// among the pull request's dismissed reviews. It returns the review's ID,
// 0 when none was posted.
func (f *FakeReviewGitHub) DismissReview(number int) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.posted) - 1; i >= 0; i-- {
		if f.posted[i].Number == number {
			if f.dismissed == nil {
				f.dismissed = map[int64]bool{}
			}
			id := int64(i + 1)
			f.dismissed[id] = true
			return id
		}
	}
	return 0
}

// SetMergeable gives a pull request GitHub's mergeable (MERGEABLE,
// CONFLICTING, UNKNOWN) and mergeStateStatus (CLEAN, BEHIND, DIRTY...), as
// the steward's watch reads them.
func (f *FakeReviewGitHub) SetMergeable(number int, mergeable, mergeState string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mergeable == nil {
		f.mergeable = map[int][2]string{}
	}
	f.mergeable[number] = [2]string{mergeable, mergeState}
}

// editPR changes the open pull request numbered number, under the lock.
func (f *FakeReviewGitHub) editPR(number int, edit func(*service.ReviewGitHubPR)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for branch, pr := range f.prs {
		if pr.Number == number {
			edit(&pr)
			f.prs[branch] = pr
		}
	}
}

// Draft makes an open pull request a draft, or ready.
func (f *FakeReviewGitHub) Draft(number int, draft bool) {
	f.editPR(number, func(pr *service.ReviewGitHubPR) { pr.Draft = draft })
}

// RefuseReady makes GitHub refuse to mark the pull request ready.
func (f *FakeReviewGitHub) RefuseReady(number int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refuseReady = number
}

// Readied are the pull requests marked ready, in order.
func (f *FakeReviewGitHub) Readied() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.readied...)
}

// MarkReady marks a draft ready, as the author App does host-side; a
// pull request already ready is left alone.
func (f *FakeReviewGitHub) MarkReady(_ context.Context, _ string, number int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if number == f.refuseReady {
		return false, &service.GitHubError{Status: 403, Message: "Resource not accessible by integration"}
	}
	for branch, pr := range f.prs {
		if pr.Number == number {
			if !pr.Draft {
				return false, nil
			}
			pr.Draft = false
			f.prs[branch] = pr
			f.readied = append(f.readied, number)
			return true, nil
		}
	}
	return false, &service.GitHubError{Status: 404, Message: "Not Found"}
}

// ReviewerLogin is the login the fake posts reviews as: the reviewer
// App's, as GitHub's REST reads name it.
const ReviewerLogin = "legato-reviewer[bot]"

// SetLoginRecorder is told the login each review is posted as, as
// ReviewPublisher.SetLoginRecorder is.
func (f *FakeReviewGitHub) SetLoginRecorder(record func(login string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.learnLogin = record
}

type fakePush struct {
	number int
	head   string
}

// PushAfterPost moves pull request number's head to head as soon as a
// review of pull request posted is posted, as an owner pushing while a
// pass posts layer by layer.
func (f *FakeReviewGitHub) PushAfterPost(posted, number int, head string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pushAfter == nil {
		f.pushAfter = map[int]fakePush{}
	}
	f.pushAfter[posted] = fakePush{number: number, head: head}
}

// Diff records that a pull request's own diff at head (GitHub's, against
// its base) changes lines first to last of path. A head with none
// recorded has a diff GitHub could not give.
func (f *FakeReviewGitHub) Diff(head, path string, first, last int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.diffs == nil {
		f.diffs = map[string]map[string][][2]int{}
	}
	if f.diffs[head] == nil {
		f.diffs[head] = map[string][][2]int{}
	}
	f.diffs[head][path] = append(f.diffs[head][path], [2]int{first, last})
}

func (f *FakeReviewGitHub) PullDiff(_ context.Context, _ string, number int) (map[string][][2]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, pr := range f.prs {
		if pr.Number != number {
			continue
		}
		recorded, ok := f.diffs[pr.HeadSHA]
		if !ok {
			return nil, nil
		}
		out := map[string][][2]int{}
		for path, ranges := range recorded {
			out[path] = append([][2]int(nil), ranges...)
		}
		return out, nil
	}
	return nil, &service.GitHubError{Status: 404, Message: "Not Found"}
}

// FailPosts answers the next n review posts with a 502. With taken, GitHub
// takes each review first, as when its answer is lost.
func (f *FakeReviewGitHub) FailPosts(n int, taken bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failPosts, f.failTaken = n, taken
}

// RefusePosts answers the next n review posts with a 422: GitHub saw each
// and took nothing.
func (f *FakeReviewGitHub) RefusePosts(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refusePosts = n
}

// Tried are the review posts Legato sent, those GitHub found or refused
// included.
func (f *FakeReviewGitHub) Tried() []service.ReviewPost {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]service.ReviewPost(nil), f.tried...)
}

// FindReview finds the review with the post's marker, as the publisher
// does.
func (f *FakeReviewGitHub) FindReview(_ context.Context, post service.ReviewPost) (service.PostedReview, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	review, ok := f.reviews[post.Marker]
	review.Existed = ok
	return review, ok, nil
}

// OnRead runs hook once, just before a pull request read (one or by
// branch), after skip reads go by: a test acts while Legato reads GitHub,
// as it does before each post.
func (f *FakeReviewGitHub) OnRead(skip int, hook func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onRead, f.onReadSkip = hook, skip
}

// beforeRead runs the hook OnRead set when its read comes. Call without
// f.mu.
func (f *FakeReviewGitHub) beforeRead() {
	f.mu.Lock()
	hook := f.onRead
	if hook != nil && f.onReadSkip > 0 {
		f.onReadSkip--
		hook = nil
	} else {
		f.onRead = nil
	}
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
}

// LoseAnswers makes GitHub take the next n replies and conversation
// comments, but answer each with a 502, as when Legato crashes or GitHub's
// answer never comes back.
func (f *FakeReviewGitHub) LoseAnswers(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lose = n
}

// take records a comment with its marker, as the publisher does: one
// already posted with that marker is found, not posted again. Call with
// f.mu held.
func (f *FakeReviewGitHub) take(marker service.CommentMarker, record func()) (found bool, err error) {
	if marker.Key == "" || marker.Kind == "" {
		return false, fmt.Errorf("a comment needs a marker")
	}
	key := marker.Kind + " " + marker.Key
	if f.markers[key] {
		return true, nil
	}
	if f.markers == nil {
		f.markers = map[string]bool{}
	}
	f.markers[key] = true
	record()
	if f.lose > 0 {
		f.lose--
		return false, &service.GitHubError{Status: 502, Message: "Bad Gateway"}
	}
	return false, nil
}

// NewFakeReviewGitHub has no pull requests.
func NewFakeReviewGitHub() *FakeReviewGitHub {
	return &FakeReviewGitHub{prs: map[string]service.ReviewGitHubPR{}, comment: 1000}
}

// OpenPR opens a pull request from branch at head.
func (f *FakeReviewGitHub) OpenPR(number int, branch, head string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prs[branch] = service.ReviewGitHubPR{Number: number, Branch: branch, Base: "main", Title: fmt.Sprintf("PR %d", number), HeadSHA: head, Author: "legato-author[bot]",
		AuthorAssociation: "OWNER"}
}

// SetAuthor makes login, with its association to the repository (OWNER,
// MEMBER, COLLABORATOR, CONTRIBUTOR, NONE...), an open pull request's
// author, as GitHub's REST reads name it.
func (f *FakeReviewGitHub) SetAuthor(number int, login, association string) {
	f.editPR(number, func(pr *service.ReviewGitHubPR) { pr.Author, pr.AuthorAssociation = login, association })
}

// Describe gives an open pull request a title and a body, as its author
// wrote them on GitHub.
func (f *FakeReviewGitHub) Describe(number int, title, body string) {
	f.editPR(number, func(pr *service.ReviewGitHubPR) { pr.Title, pr.Body = title, body })
}

// OpenStackedPR opens a pull request from branch onto base at head, a layer
// of a stack.
func (f *FakeReviewGitHub) OpenStackedPR(number int, branch, base, head string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prs[branch] = service.ReviewGitHubPR{Number: number, Branch: branch, Base: base, Title: fmt.Sprintf("PR %d", number), HeadSHA: head, Author: "legato-author[bot]",
		AuthorAssociation: "OWNER"}
}

func (f *FakeReviewGitHub) OpenPulls(_ context.Context, _ string, q service.PullQuery) ([]service.ReviewGitHubPR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readFails() {
		return nil, &service.GitHubError{Status: 503, Message: "Service Unavailable"}
	}
	var out []service.ReviewGitHubPR
	for _, pr := range f.prs {
		if _, closed := f.closed[pr.Number]; closed {
			continue
		}
		if (q.Head != "" && pr.Branch == q.Head) || (q.Base != "" && pr.Base == q.Base) {
			out = append(out, pr)
		}
	}
	return out, nil
}

// Posted are the reviews posted, oldest first.
func (f *FakeReviewGitHub) Posted() []service.ReviewPost {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]service.ReviewPost(nil), f.posted...)
}

// Replies are the thread replies posted.
func (f *FakeReviewGitHub) Replies() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.replies...)
}

// Resolved are the comments whose threads were resolved.
func (f *FakeReviewGitHub) Resolved() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int64(nil), f.resolved...)
}

func (f *FakeReviewGitHub) PullRequests(_ context.Context, _ string, branches []string) ([]service.ReviewGitHubPR, error) {
	f.beforeRead()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readFails() {
		return nil, &service.GitHubError{Status: 503, Message: "Service Unavailable"}
	}
	var out []service.ReviewGitHubPR
	for _, b := range branches {
		if pr, ok := f.prs[b]; ok {
			out = append(out, pr)
		}
	}
	return out, nil
}

func (f *FakeReviewGitHub) PostReview(_ context.Context, post service.ReviewPost) (service.PostedReview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tried = append(f.tried, post)
	if review, ok := f.reviews[post.Marker]; ok {
		review.Existed = true
		return review, nil // a review with the marker is found, as the publisher finds it
	}
	if f.refusePosts > 0 {
		f.refusePosts--
		return service.PostedReview{}, &service.GitHubError{Status: 422, Message: "Unprocessable Entity"}
	}
	lost := false
	if f.failPosts > 0 {
		f.failPosts--
		if !f.failTaken {
			return service.PostedReview{}, &service.GitHubError{Status: 502, Message: "Bad Gateway"}
		}
		lost = true
	}
	f.posted = append(f.posted, post)
	if f.learnLogin != nil {
		f.learnLogin(ReviewerLogin)
	}
	posted := service.PostedReview{ID: int64(len(f.posted)), URL: fmt.Sprintf("https://github.com/review/%d", len(f.posted))}
	for i, finding := range post.Findings {
		if finding.Place != nil {
			f.comment++
			posted.Inline = append(posted.Inline, i)
			posted.CommentIDs = append(posted.CommentIDs, f.comment)
		}
	}
	if f.reviews == nil {
		f.reviews = map[string]service.PostedReview{}
	}
	f.reviews[post.Marker] = posted
	if push, ok := f.pushAfter[post.Number]; ok {
		delete(f.pushAfter, post.Number)
		for branch, pr := range f.prs {
			if pr.Number == push.number {
				pr.HeadSHA = push.head
				f.prs[branch] = pr
			}
		}
	}
	if lost {
		return service.PostedReview{}, &service.GitHubError{Status: 502, Message: "Bad Gateway"}
	}
	return posted, nil
}

// SetComments puts a pull request's comments on GitHub, as the next poll
// finds them.
func (f *FakeReviewGitHub) SetComments(number int, comments ...service.PRComment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.prComments == nil {
		f.prComments = map[int][]service.PRComment{}
	}
	f.prComments[number] = append([]service.PRComment(nil), comments...)
}

// SetChecks sets the counted checks of a head, as the next poll finds
// them.
func (f *FakeReviewGitHub) SetChecks(head string, checks ...service.CheckResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.checks == nil {
		f.checks = map[string][]service.CheckResult{}
	}
	f.checks[head] = append([]service.CheckResult(nil), checks...)
}

// SetBaseChecks sets the checks of a base branch's head: a check failing
// on a pull request's head that fails there too is the base's (LEG-711).
func (f *FakeReviewGitHub) SetBaseChecks(base string, checks ...service.CheckResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.baseChecks == nil {
		f.baseChecks = map[string][]service.CheckResult{}
	}
	f.baseChecks[base] = append([]service.CheckResult(nil), checks...)
}

// failingOnBase says the base's head fails a check. Call with f.mu held.
func (f *FakeReviewGitHub) failingOnBase(base, name string) bool {
	for _, c := range f.baseChecks[base] {
		if c.Name == name && c.State == service.CheckFailing {
			return true
		}
	}
	return false
}

func (f *FakeReviewGitHub) Checks(_ context.Context, _, base, head string, _ []string) ([]service.CheckResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]service.CheckResult(nil), f.checks[head]...)
	for i, c := range out {
		if c.State == service.CheckFailing && f.failingOnBase(base, c.Name) {
			out[i].State = service.CheckFailingOnBase
		}
	}
	return out, nil
}

// Polls counts the polls of the pull requests' comments.
func (f *FakeReviewGitHub) Polls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.polls
}

// FailPolls makes every poll of repo's pull requests fail with err (a
// *service.GitHubRateLimit, a *service.GitHubError with a 5xx), until it is
// called again with nil.
func (f *FakeReviewGitHub) FailPolls(repo string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pollErrors == nil {
		f.pollErrors = map[string]error{}
	}
	if err == nil {
		delete(f.pollErrors, repo)
		return
	}
	f.pollErrors[repo] = err
}

// PollsOf counts the polls of repo's pull requests, failed ones included.
func (f *FakeReviewGitHub) PollsOf(repo string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pollsOf[repo]
}

func (f *FakeReviewGitHub) PollComments(_ context.Context, repo string, number int, _ service.PollPages) (service.CommentPoll, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls++
	if f.pollsOf == nil {
		f.pollsOf = map[string]int{}
	}
	f.pollsOf[repo]++
	if err := f.pollErrors[repo]; err != nil {
		return service.CommentPoll{}, err
	}
	return service.CommentPoll{Changed: true, Comments: append([]service.PRComment(nil), f.prComments[number]...)}, nil
}

// Comments are the conversation comments posted, "identity: body".
func (f *FakeReviewGitHub) Comments() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.comments...)
}

func (f *FakeReviewGitHub) PostIssueComment(_ context.Context, who service.GitHubIdentity, _ string, _ int, body string, marker service.CommentMarker) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, err := f.take(marker, func() { f.comments = append(f.comments, identityName(who)+": "+body) })
	return int64(len(f.comments)), err
}

func identityName(who service.GitHubIdentity) string {
	if who == service.AsAuthor {
		return "author"
	}
	return "reviewer"
}

func (f *FakeReviewGitHub) Reply(_ context.Context, who service.GitHubIdentity, _ string, _ int, commentID int64, body string, marker service.CommentMarker) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, err := f.take(marker, func() { f.replies = append(f.replies, fmt.Sprintf("%s %d: %s", identityName(who), commentID, body)) })
	return err
}

func (f *FakeReviewGitHub) ResolveThread(_ context.Context, _ string, _ int, commentID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved = append(f.resolved, commentID)
	return nil
}

// FakeCheckout is the author's checkout as review passes read it: one
// folder, its branch tips, HEAD, whether it is clean, and its files' line
// counts. SetFor gives a card a checkout of its own, in its own folder.
type FakeCheckout struct {
	mu       sync.Mutex
	dir      string
	branches []string
	// cards are the cards with a checkout of their own; heads the HEAD
	// of each such checkout's folder.
	cards   map[string]fakeCardCheckout
	heads   map[string]string
	tips    map[string]string
	head    string
	clean   bool
	lines   map[string]int
	pins    map[string]string
	changes map[string]map[string][][2]int // commit -> file -> lines
	// since are changes from one commit to another, "from..to" -> file ->
	// lines; they win over changes.
	since map[string]map[string][][2]int
}

// NewFakeCheckout is a clean checkout with no branches.
func NewFakeCheckout() *FakeCheckout {
	return &FakeCheckout{tips: map[string]string{}, clean: true, lines: map[string]int{}, pins: map[string]string{}, changes: map[string]map[string][][2]int{}}
}

// Set puts the checkout in dir with branch at tip, checked out.
func (f *FakeCheckout) Set(dir, branch, tip string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dir, f.branches, f.tips[branch], f.head = dir, []string{branch}, tip, tip
}

type fakeCardCheckout struct {
	dir      string
	branches []string
}

// SetFor gives the card a checkout of its own in dir, with branch at tip
// checked out; other cards keep the one Set puts.
func (f *FakeCheckout) SetFor(card, dir, branch, tip string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cards == nil {
		f.cards, f.heads = map[string]fakeCardCheckout{}, map[string]string{}
	}
	f.cards[card] = fakeCardCheckout{dir: dir, branches: []string{branch}}
	f.tips[branch], f.heads[dir] = tip, tip
}

// Dirty changes the working tree, or cleans it.
func (f *FakeCheckout) Dirty(dirty bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clean = !dirty
}

// Commit moves HEAD and the branch to sha.
func (f *FakeCheckout) Commit(branch, sha string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tips[branch], f.head = sha, sha
}

// File gives path n lines at every commit.
func (f *FakeCheckout) File(path string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines[path] = n
}

// Change records that commit sha changed lines first to last of path,
// against any other commit.
func (f *FakeCheckout) Change(sha, path string, first, last int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.changes[sha] == nil {
		f.changes[sha] = map[string][][2]int{}
	}
	f.changes[sha][path] = append(f.changes[sha][path], [2]int{first, last})
}

// ChangeSince records that from..to changed lines first to last of path:
// what Changed says of that pair, in place of what Change recorded of to.
func (f *FakeCheckout) ChangeSince(from, to, path string, first, last int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.since == nil {
		f.since = map[string]map[string][][2]int{}
	}
	key := from + ".." + to
	if f.since[key] == nil {
		f.since[key] = map[string][][2]int{}
	}
	f.since[key][path] = append(f.since[key][path], [2]int{first, last})
}

func (f *FakeCheckout) Changed(_ context.Context, _, from, to string) (map[string][][2]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][][2]int{}
	if from == to {
		return out, nil
	}
	changes := f.changes[to]
	if since, ok := f.since[from+".."+to]; ok {
		changes = since
	}
	for path, ranges := range changes {
		out[path] = append([][2]int(nil), ranges...)
	}
	return out, nil
}

// Pins are the refs pinned, ref -> commit.
func (f *FakeCheckout) Pins() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]string{}
	for k, v := range f.pins {
		out[k] = v
	}
	return out
}

func (f *FakeCheckout) Branches(_ context.Context, card, _ string) (string, []string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if own, ok := f.cards[card]; ok {
		return own.dir, append([]string(nil), own.branches...), nil
	}
	return f.dir, append([]string(nil), f.branches...), nil
}

func (f *FakeCheckout) Tip(_ context.Context, _, branch string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tips[branch], nil
}

func (f *FakeCheckout) State(_ context.Context, dir string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if head, ok := f.heads[dir]; ok {
		return head, f.clean, nil
	}
	return f.head, f.clean, nil
}

func (f *FakeCheckout) Pin(_ context.Context, _, ref, sha string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pins[ref] = sha
	return nil
}

func (f *FakeCheckout) Lines(_ context.Context, _, _, path string) (int, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.lines[path]
	return n, ok, nil
}

// reviewConfig is the worker's review_passes: codex reviews with
// 3-tier-review.
func (o ReviewPassOptions) config() service.ReviewPassConfig {
	concurrent, passes := o.MaxConcurrent, o.MaxPasses
	if concurrent == 0 {
		concurrent = 2
	}
	if passes == 0 {
		passes = 3
	}
	bots := o.CountedBots
	if bots == nil {
		bots = []string{"cubic-dev-ai[bot]"}
	}
	return service.ReviewPassConfig{Enabled: true, PassTimeout: 45 * time.Minute, MaxConcurrent: concurrent, ReviewColumn: "Review",
		PollInterval: time.Minute, QuietTime: 2 * time.Minute, CountedBots: bots,
		Defaults: service.ReviewPassSettings{Enabled: true, Agent: "codex", Model: "gpt-5.5", Skill: "3-tier-review", MaxPasses: passes}}
}

// advert is what the worker says of its review passes: codex or Claude
// Code, codex's tier careful, and the 3-tier-review skill.
func (o ReviewPassOptions) advert() *service.ReviewPassesAdvert {
	return &service.ReviewPassesAdvert{Defaults: o.config().Defaults, Skills: []string{"3-tier-review"},
		Adapters: []service.ReviewAdapter{{Name: "claude-code"}, {Name: "codex", Tiers: []string{"careful"}}}}
}

// typedTo is everything typed to a session, joined.
func (w *Worker) typedTo(session string) string {
	return strings.Join(w.Tmux.Typed(session), "\n")
}

// ClosePR closes a pull request.
func (f *FakeReviewGitHub) ClosePR(number int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed == nil {
		f.closed = map[int]string{}
	}
	f.closed[number] = "closed"
}

// MergePR merges a pull request.
func (f *FakeReviewGitHub) MergePR(number int) {
	f.ClosePR(number)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed[number] = "merged"
}

// NotInstalled makes repo one the reviewer App cannot read.
func (f *FakeReviewGitHub) NotInstalled(repo string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notInstalled == nil {
		f.notInstalled = map[string]bool{}
	}
	f.notInstalled[repo] = true
}

// Push moves a pull request's head, as its owner pushing.
func (f *FakeReviewGitHub) Push(number int, head string) {
	f.editPR(number, func(pr *service.ReviewGitHubPR) { pr.HeadSHA = head })
}

func (f *FakeReviewGitHub) Installed(_ context.Context, repo string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.notInstalled[repo] {
		return &service.GitHubError{Status: 404, Message: "Not Found"}
	}
	return nil
}

// FailReads makes the next n pull request reads (one or by branch) fail.
func (f *FakeReviewGitHub) FailReads(n int) { f.FailReadsAfter(0, n) }

// FailReadsAfter lets ok pull request reads succeed, then fails n.
func (f *FakeReviewGitHub) FailReadsAfter(ok, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failAfter, f.failReads = ok, n
}

// readFails says whether this read fails. Call with f.mu held.
func (f *FakeReviewGitHub) readFails() bool {
	if f.failReads == 0 {
		return false
	}
	if f.failAfter > 0 {
		f.failAfter--
		return false
	}
	f.failReads--
	return true
}

func (f *FakeReviewGitHub) PullRequest(_ context.Context, _ string, number int) (service.ReviewGitHubPR, string, error) {
	f.beforeRead()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.readFails() {
		return service.ReviewGitHubPR{}, "", &service.GitHubError{Status: 503, Message: "Service Unavailable"}
	}
	for _, pr := range f.prs {
		if pr.Number == number {
			state := "open"
			if closed, ok := f.closed[number]; ok {
				state = closed
			}
			return pr, state, nil
		}
	}
	return service.ReviewGitHubPR{}, "", &service.GitHubError{Status: 404, Message: "Not Found"}
}

// PullHead fetches a pull request's head for review, as the repository
// cache does.
func (f *FakeReviewGitHub) PullHead(_ context.Context, _ string, number int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetched = append(f.fetched, number)
	return nil
}

// Fetched are the pull requests whose heads were fetched for review.
func (f *FakeReviewGitHub) Fetched() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.fetched...)
}

// OpenForkPR opens a pull request onto main from branch of headRepo, a
// fork, at head.
func (f *FakeReviewGitHub) OpenForkPR(number int, branch, head, headRepo string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prs[headRepo+":"+branch] = service.ReviewGitHubPR{Number: number, Branch: branch, Base: "main", Title: fmt.Sprintf("PR %d", number), HeadSHA: head,
		Author: "someone", AuthorAssociation: "NONE", HeadRepo: headRepo}
}

// WatchSession is the steward's reads of a loop in one tick (spec LEG-559
// C7): its pull requests' state, heads and counted checks, as one read, and
// their comments. Every check the fake has is one branch protection
// requires.
func (f *FakeReviewGitHub) WatchSession(repo string) service.ReviewWatchSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.watches++
	return &fakeWatchSession{f: f, repo: repo}
}

// Reads counts the reads of one pull request (PullRequest).
func (f *FakeReviewGitHub) Reads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

// Watches counts the steward's watch sessions opened.
func (f *FakeReviewGitHub) Watches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.watches
}

type fakeWatchSession struct {
	f    *FakeReviewGitHub
	repo string
}

func (s *fakeWatchSession) Watch(_ context.Context, numbers []int) ([]service.PRWatch, error) {
	f := s.f
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]service.PRWatch, 0, len(numbers))
	for _, n := range numbers {
		var pr *service.ReviewGitHubPR
		for _, p := range f.prs {
			if p.Number == n {
				p := p
				pr = &p
			}
		}
		if pr == nil {
			return nil, fmt.Errorf("GitHub has no pull request %s#%d", s.repo, n)
		}
		state := "open"
		if closed, ok := f.closed[n]; ok {
			state = closed
		}
		w := service.PRWatch{Number: n, State: state, Draft: pr.Draft, HeadSHA: pr.HeadSHA, HeadBranch: pr.Branch, BaseBranch: pr.Base,
			Mergeable: "MERGEABLE", MergeState: "CLEAN"}
		if m, ok := f.mergeable[n]; ok {
			w.Mergeable, w.MergeState = m[0], m[1]
		}
		for i, post := range f.posted {
			if post.Number == n && f.dismissed[int64(i+1)] {
				w.DismissedReviews = append(w.DismissedReviews, int64(i+1))
			}
		}
		if base, ok := f.baseChecks[pr.Base]; ok {
			var history []service.WatchCheck
			for _, c := range base {
				history = append(history, service.WatchCheck{Name: c.Name, State: c.State, Conclusion: c.Conclusion})
			}
			w.BaseHistory = [][]service.WatchCheck{history}
		}
		for _, c := range f.checks[pr.HeadSHA] {
			w.Checks = append(w.Checks, service.WatchCheck{Name: c.Name, State: c.State, Conclusion: c.Conclusion, URL: c.URL})
			switch {
			case c.State == service.CheckFailing:
				w.Rollup = service.CheckFailing
			case c.State == service.CheckPending && w.Rollup != service.CheckFailing:
				w.Rollup = service.CheckPending
			case w.Rollup == "":
				w.Rollup = service.CheckPassing
			}
		}
		out = append(out, w)
	}
	return out, nil
}

func (s *fakeWatchSession) PollComments(ctx context.Context, number int, pages service.PollPages) (service.CommentPoll, error) {
	return s.f.PollComments(ctx, s.repo, number, pages)
}

func (s *fakeWatchSession) Stacked(ctx context.Context, base string) ([]service.ReviewGitHubPR, error) {
	return s.f.OpenPulls(ctx, s.repo, service.PullQuery{Base: base})
}

func (s *fakeWatchSession) Close() {}
