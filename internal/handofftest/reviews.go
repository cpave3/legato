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
}

// NewFakeReviewGitHub has no pull requests.
func NewFakeReviewGitHub() *FakeReviewGitHub {
	return &FakeReviewGitHub{prs: map[string]service.ReviewGitHubPR{}, comment: 1000}
}

// OpenPR opens a pull request from branch at head.
func (f *FakeReviewGitHub) OpenPR(number int, branch, head string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prs[branch] = service.ReviewGitHubPR{Number: number, Branch: branch, Base: "main", Title: fmt.Sprintf("PR %d", number), HeadSHA: head, Author: "legato-author[bot]"}
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
	f.mu.Lock()
	defer f.mu.Unlock()
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
	f.posted = append(f.posted, post)
	posted := service.PostedReview{ID: int64(len(f.posted)), URL: fmt.Sprintf("https://github.com/review/%d", len(f.posted))}
	for i, finding := range post.Findings {
		if finding.Place != nil {
			f.comment++
			posted.Inline = append(posted.Inline, i)
			posted.CommentIDs = append(posted.CommentIDs, f.comment)
		}
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

// Polls counts the polls of the pull requests' comments.
func (f *FakeReviewGitHub) Polls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.polls
}

func (f *FakeReviewGitHub) PollComments(_ context.Context, _ string, number int, _ service.PollPages) (service.CommentPoll, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls++
	return service.CommentPoll{Changed: true, Comments: append([]service.PRComment(nil), f.prComments[number]...)}, nil
}

// Comments are the conversation comments posted, "identity: body".
func (f *FakeReviewGitHub) Comments() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.comments...)
}

func (f *FakeReviewGitHub) PostIssueComment(_ context.Context, who service.GitHubIdentity, _ string, _ int, body string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments = append(f.comments, identityName(who)+": "+body)
	return int64(len(f.comments)), nil
}

func identityName(who service.GitHubIdentity) string {
	if who == service.AsAuthor {
		return "author"
	}
	return "reviewer"
}

func (f *FakeReviewGitHub) Reply(_ context.Context, who service.GitHubIdentity, _ string, _ int, commentID int64, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, fmt.Sprintf("%s %d: %s", identityName(who), commentID, body))
	return nil
}

func (f *FakeReviewGitHub) ResolveThread(_ context.Context, _ string, _ int, commentID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved = append(f.resolved, commentID)
	return nil
}

// FakeCheckout is the author's checkout as review passes read it: one
// folder, its branch tips, HEAD, whether it is clean, and its files' line
// counts.
type FakeCheckout struct {
	mu       sync.Mutex
	dir      string
	branches []string
	tips     map[string]string
	head     string
	clean    bool
	lines    map[string]int
	pins     map[string]string
}

// NewFakeCheckout is a clean checkout with no branches.
func NewFakeCheckout() *FakeCheckout {
	return &FakeCheckout{tips: map[string]string{}, clean: true, lines: map[string]int{}, pins: map[string]string{}}
}

// Set puts the checkout in dir with branch at tip, checked out.
func (f *FakeCheckout) Set(dir, branch, tip string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dir, f.branches, f.tips[branch], f.head = dir, []string{branch}, tip, tip
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

func (f *FakeCheckout) Branches(context.Context, string, string) (string, []string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dir, append([]string(nil), f.branches...), nil
}

func (f *FakeCheckout) Tip(_ context.Context, _, branch string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tips[branch], nil
}

func (f *FakeCheckout) State(context.Context, string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	return service.ReviewPassConfig{Enabled: true, PassTimeout: 45 * time.Minute, MaxConcurrent: concurrent, ReviewColumn: "Review",
		PollInterval: time.Minute, QuietTime: 2 * time.Minute, CountedBots: []string{"cubic-dev-ai[bot]"},
		Defaults: service.ReviewPassSettings{Enabled: true, Agent: "codex", Model: "gpt-5.5", Skill: "3-tier-review", MaxPasses: passes}}
}

// typedTo is everything typed to a session, joined.
func (w *Worker) typedTo(session string) string {
	return strings.Join(w.Tmux.Typed(session), "\n")
}
