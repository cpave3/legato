package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/store"
)

// fakePR is a pull request the fake GitHub serves.
type fakePR struct {
	Number  int
	Head    string
	Base    string
	SHA     string
	State   string // open or closed
	Merged  bool
	Draft   bool
	Created string
	// Review is the one reviewer's latest review state, if any.
	Review string
	// Check is the one check run's conclusion, if any.
	Check string
}

// fakeGitHub is a GitHub API on an httptest server, serving the reads PR
// tracking makes: a repository's PR list, one PR, its reviews, and the
// checks and statuses of its head commit.
type fakeGitHub struct {
	t      *testing.T
	server *httptest.Server
	mu     sync.Mutex
	repos  map[string][]fakePR
	calls  map[string]int
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{t: t, repos: map[string][]fakePR{}, calls: map[string]int{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGitHub) set(repo string, prs ...fakePR) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repos[repo] = prs
}

func (f *fakeGitHub) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[path]
}

func (f *fakeGitHub) client() *github.Client {
	f.t.Helper()
	c, err := github.New(github.Options{Token: "test-token", BaseURL: f.server.URL})
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[r.URL.Path]++
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 || parts[0] != "repos" {
		http.NotFound(w, r)
		return
	}
	repo := parts[1] + "/" + parts[2]
	prs := f.repos[repo]
	write := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	switch {
	case len(parts) == 4 && parts[3] == "pulls":
		list := make([]any, 0, len(prs))
		for i := len(prs) - 1; i >= 0; i-- { // newest first
			list = append(list, apiJSON(repo, prs[i]))
		}
		write(list)
	case len(parts) >= 5 && parts[3] == "pulls":
		n, _ := strconv.Atoi(parts[4])
		for _, pr := range prs {
			if pr.Number != n {
				continue
			}
			if len(parts) == 6 && parts[5] == "reviews" {
				reviews := []any{}
				if pr.Review != "" {
					reviews = append(reviews, map[string]any{"state": pr.Review, "submitted_at": "2026-01-02T00:00:00Z", "user": map[string]any{"login": "reviewer"}})
				}
				write(reviews)
				return
			}
			write(apiJSON(repo, pr))
			return
		}
		http.NotFound(w, r)
	case len(parts) == 6 && parts[3] == "commits" && parts[5] == "check-runs":
		runs := []any{}
		for _, pr := range prs {
			if headSHA(pr) == parts[4] && pr.Check != "" {
				runs = append(runs, map[string]any{"status": "completed", "conclusion": pr.Check})
			}
		}
		write(map[string]any{"check_runs": runs})
	case len(parts) == 6 && parts[3] == "commits" && parts[5] == "status":
		write(map[string]any{"statuses": []any{}})
	default:
		http.NotFound(w, r)
	}
}

func apiJSON(repo string, pr fakePR) map[string]any {
	state := pr.State
	if state == "" {
		state = "open"
	}
	created := pr.Created
	if created == "" {
		created = "2026-01-01T00:00:00Z"
	}
	sha := headSHA(pr)
	base := pr.Base
	if base == "" {
		base = "main"
	}
	return map[string]any{
		"number": pr.Number, "title": pr.Head, "state": state, "draft": pr.Draft, "merged": pr.Merged,
		"html_url":   "https://github.com/" + repo + "/pull/" + strconv.Itoa(pr.Number),
		"created_at": created,
		"head":       map[string]any{"ref": pr.Head, "sha": sha, "repo": map[string]any{"full_name": repo}},
		"base":       map[string]any{"ref": base},
	}
}

func headSHA(pr fakePR) string {
	if pr.SHA != "" {
		return pr.SHA
	}
	return "sha-" + strconv.Itoa(pr.Number)
}

func newBranchTracking(t *testing.T, gh *fakeGitHub) (*store.Store, *prTrackingService, *events.Bus) {
	t.Helper()
	s := newTestPRStore(t)
	bus := events.New()
	svc := NewPRTrackingService(s, bus, gh.client(), time.Minute, time.Minute).(*prTrackingService)
	return s, svc, bus
}

func setRepoContext(t *testing.T, s *store.Store, taskID, repo, branch string) {
	t.Helper()
	if err := s.SetCodeContext(context.Background(), taskID, &store.CodeContext{Repo: repo, Branch: branch, RepoSource: "manual", BranchSource: "manual"}); err != nil {
		t.Fatal(err)
	}
}

// prList is a task's tracked branches as "branch:#number:flags", in order.
func prList(t *testing.T, svc *prTrackingService, taskID string) []string {
	t.Helper()
	branches, err := svc.TrackedBranches(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, b := range branches {
		item := b.Branch
		if b.PR != nil && b.PR.PRNumber > 0 {
			item += ":#" + strconv.Itoa(b.PR.PRNumber)
			if b.PR.IsDraft {
				item += ":draft"
			}
			if b.PR.CheckStatus != "" {
				item += ":" + b.PR.CheckStatus
			}
			if b.PR.ReviewDecision != "" {
				item += ":" + b.PR.ReviewDecision
			}
		}
		out = append(out, item)
	}
	return out
}

func TestAddedBranchesFindTheirPRsInStackOrder(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.set("acme/widget",
		fakePR{Number: 11, Head: "legato/task-1-a", Check: "failure", Review: "CHANGES_REQUESTED"},
		fakePR{Number: 12, Head: "legato/task-1-b", Base: "legato/task-1-a", Draft: true},
		fakePR{Number: 13, Head: "someone-else"},
	)
	s, svc, _ := newBranchTracking(t, gh)
	ctx := context.Background()
	createPRTask(t, s, "task-1")
	setRepoContext(t, s, "task-1", "acme/widget", "")

	added, err := svc.AddTaskBranches(ctx, "task-1", "acme/widget", []string{"legato/task-1-a", "legato/task-1-b"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(added, []string{"legato/task-1-a", "legato/task-1-b"}) {
		t.Fatalf("added = %v", added)
	}
	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}

	want := []string{"legato/task-1-a:#11:fail:CHANGES_REQUESTED", "legato/task-1-b:#12:draft"}
	if got := prList(t, svc, "task-1"); !reflect.DeepEqual(got, want) {
		t.Fatalf("tracked = %v, want %v", got, want)
	}
	if n := gh.count("/repos/acme/widget/pulls"); n != 1 {
		t.Fatalf("listed the repository's PRs %d times, want once", n)
	}
	// The single PR field still names the first PR, for its old readers.
	meta, err := svc.GetPRStatus(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil || meta.PRNumber != 11 || meta.SyncOutcome != "linked" || meta.Repo != "acme/widget" || meta.Branch != "legato/task-1-a" {
		t.Fatalf("pr_meta = %#v", meta)
	}

	// The board lists both, in order, without GitHub.
	listed, err := NewBoardService(s, nil).(PullRequestLister).ListPullRequests(ctx, []string{"task-1"})
	if err != nil {
		t.Fatal(err)
	}
	if prs := listed["task-1"]; len(prs) != 2 || prs[0].PRNumber != 11 || prs[1].PRNumber != 12 || prs[1].Branch != "legato/task-1-b" {
		t.Fatalf("listed = %+v", listed)
	}

	// Adding them again adds nothing.
	added, err = svc.AddTaskBranches(ctx, "task-1", "acme/widget", []string{"legato/task-1-a"})
	if err != nil || len(added) != 0 {
		t.Fatalf("added again = %v, %v", added, err)
	}
}

func TestAStackRecordedForTheTasksCloneIsTracked(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.set("acme/widget",
		fakePR{Number: 21, Head: "legato/task-1-api"},
		fakePR{Number: 22, Head: "legato/task-1-ui", Base: "legato/task-1-api"},
	)
	s, svc, _ := newBranchTracking(t, gh)
	ctx := context.Background()
	createPRTask(t, s, "task-1")
	setRepoContext(t, s, "task-1", "acme/widget", "legato/task-1")
	cloneID, err := s.CreateJobClone(ctx, store.JobClone{ExecutionID: "exec-1", TaskID: "task-1", Repo: "acme/widget", Branch: "legato/task-1", Path: t.TempDir(), CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	branches := `[{"name":"legato/task-1-api","parent":"main","base_sha":"b0","state":"pushed"},{"name":"legato/task-1-ui","parent":"legato/task-1-api","base_sha":"b1","state":"pushed"}]`
	if err := s.PutCloneStack(ctx, store.CloneStack{CloneID: cloneID, Root: "main", Branches: branches, ReadAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{"legato/task-1", "legato/task-1-api:#21", "legato/task-1-ui:#22"}
	if got := prList(t, svc, "task-1"); !reflect.DeepEqual(got, want) {
		t.Fatalf("tracked = %v, want %v", got, want)
	}
}

func TestATicketKeyStackIsNotAmbiguous(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.set("acme/widget",
		fakePR{Number: 31, Head: "legato/task-1-model"},
		fakePR{Number: 32, Head: "legato/task-1-api", Base: "legato/task-1-model"},
		fakePR{Number: 33, Head: "legato/task-1-ui", Base: "legato/task-1-api"},
	)
	s, svc, _ := newBranchTracking(t, gh)
	ctx := context.Background()
	createPRTask(t, s, "task-1")
	setRepoContext(t, s, "task-1", "acme/widget", "")

	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{"legato/task-1-model:#31", "legato/task-1-api:#32", "legato/task-1-ui:#33"}
	if got := prList(t, svc, "task-1"); !reflect.DeepEqual(got, want) {
		t.Fatalf("tracked = %v, want %v", got, want)
	}
	meta, _ := svc.GetPRStatus(ctx, "task-1")
	if meta == nil || meta.SyncOutcome != "linked" {
		t.Fatalf("pr_meta = %#v, want linked", meta)
	}
}

func TestRediscoveryLooksAgainAtEveryBranch(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.set("acme/widget",
		fakePR{Number: 41, Head: "legato/task-1-a"},
	)
	s, svc, _ := newBranchTracking(t, gh)
	ctx := context.Background()
	createPRTask(t, s, "task-1")
	setRepoContext(t, s, "task-1", "acme/widget", "")
	if _, err := svc.AddTaskBranches(ctx, "task-1", "acme/widget", []string{"legato/task-1-a", "legato/task-1-b"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RediscoverPR(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	if got, want := prList(t, svc, "task-1"), []string{"legato/task-1-a:#41", "legato/task-1-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tracked = %v, want %v", got, want)
	}

	// The first PR is closed and opened again as a new one; the second
	// branch gets its PR. Rediscovery finds both, in one listing.
	gh.set("acme/widget",
		fakePR{Number: 41, Head: "legato/task-1-a", State: "closed", Created: "2026-01-01T00:00:00Z"},
		fakePR{Number: 42, Head: "legato/task-1-b", Base: "legato/task-1-a", Created: "2026-01-02T00:00:00Z"},
		fakePR{Number: 43, Head: "legato/task-1-a", Created: "2026-01-03T00:00:00Z", Check: "success"},
	)
	before := gh.count("/repos/acme/widget/pulls")
	if err := svc.RediscoverPR(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	if got, want := prList(t, svc, "task-1"), []string{"legato/task-1-a:#43:pass", "legato/task-1-b:#42"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tracked = %v, want %v", got, want)
	}
	if n := gh.count("/repos/acme/widget/pulls") - before; n != 1 {
		t.Fatalf("listed the repository's PRs %d times, want once", n)
	}
}

func TestDiscoveringPRsKeepsThoseFoundAndFindsNewOnes(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.set("acme/widget", fakePR{Number: 51, Head: "legato/task-1-a"})
	s, svc, _ := newBranchTracking(t, gh)
	ctx := context.Background()
	createPRTask(t, s, "task-1")
	setRepoContext(t, s, "task-1", "acme/widget", "")
	if _, err := svc.AddTaskBranches(ctx, "task-1", "acme/widget", []string{"legato/task-1-a"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DiscoverPRs(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}

	// A worker pushes a branch on top; only the new branch is looked up.
	gh.set("acme/widget",
		fakePR{Number: 51, Head: "legato/task-1-a"},
		fakePR{Number: 52, Head: "legato/task-1-b", Base: "legato/task-1-a", Draft: true},
	)
	added, err := svc.AddTaskBranches(ctx, "task-1", "acme/widget", []string{"legato/task-1-a", "legato/task-1-b"})
	if err != nil || !reflect.DeepEqual(added, []string{"legato/task-1-b"}) {
		t.Fatalf("added = %v, %v", added, err)
	}
	refetches := gh.count("/repos/acme/widget/pulls/51")
	if err := svc.DiscoverPRs(ctx, "task-1"); err != nil {
		t.Fatal(err)
	}
	if got, want := prList(t, svc, "task-1"), []string{"legato/task-1-a:#51", "legato/task-1-b:#52:draft"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tracked = %v, want %v", got, want)
	}
	if gh.count("/repos/acme/widget/pulls/51") != refetches {
		t.Fatal("discovery fetched again a PR it had found")
	}
}

func TestASinglePRTaskFromBeforeKeepsItsPR(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.set("acme/widget", fakePR{Number: 7, Head: "feature/task-1", Review: "APPROVED"})
	s, svc, _ := newBranchTracking(t, gh)
	ctx := context.Background()
	createPRTask(t, s, "task-1")
	setRepoContext(t, s, "task-1", "acme/widget", "feature/task-1")
	// A task linked before branches were tracked has only the single field.
	legacy := `{"branch":"","pr_number":7,"pr_url":"https://github.com/acme/widget/pull/7","state":"OPEN","pinned":true,"match_method":"manual","sync_outcome":"linked"}`
	if err := s.UpdatePRMeta(ctx, "task-1", &legacy); err != nil {
		t.Fatal(err)
	}

	board := NewBoardService(s, nil)
	card, err := board.GetCard(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(card.PullRequests) != 1 || card.PullRequests[0].PRNumber != 7 || card.PullRequests[0].Repo != "acme/widget" {
		t.Fatalf("pull requests before the first poll = %#v", card.PullRequests)
	}

	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}
	branches, err := svc.TrackedBranches(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 1 || branches[0].Branch != "feature/task-1" || branches[0].PR == nil ||
		branches[0].PR.PRNumber != 7 || !branches[0].PR.Pinned || branches[0].PR.ReviewDecision != "APPROVED" {
		t.Fatalf("branches after the first poll = %#v", branches)
	}
	card, _ = board.GetCard(ctx, "task-1")
	if len(card.PullRequests) != 1 || card.PullRequests[0].PRNumber != 7 || card.PullRequests[0].Branch != "feature/task-1" {
		t.Fatalf("pull requests after the first poll = %#v", card.PullRequests)
	}
}

func TestMergeBlockersAreRaisedForEachReadyPRWithABlocker(t *testing.T) {
	gh := newFakeGitHub(t)
	gh.set("acme/widget",
		fakePR{Number: 61, Head: "legato/task-1-a", Check: "failure"},
		fakePR{Number: 62, Head: "legato/task-1-b", Base: "legato/task-1-a", Review: "CHANGES_REQUESTED"},
		fakePR{Number: 63, Head: "legato/task-1-c", Base: "legato/task-1-b", Draft: true, Check: "failure"},
		fakePR{Number: 64, Head: "legato/task-1-d", Base: "legato/task-1-c", Check: "success", Review: "APPROVED"},
	)
	s, svc, _ := newBranchTracking(t, gh)
	ctx := context.Background()
	createPRTask(t, s, "task-1")
	setRepoContext(t, s, "task-1", "acme/widget", "")
	if _, err := svc.AddTaskBranches(ctx, "task-1", "acme/widget", []string{"legato/task-1-a", "legato/task-1-b", "legato/task-1-c", "legato/task-1-d"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}

	items, err := NewPRAttentionAdapter(s).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, item := range items {
		got[item.SourceKey] = item.Title + " / " + item.Reason
	}
	want := map[string]string{
		"task-1:61": "PR #61 · Test task task-1 / CI failing",
		"task-1:62": "PR #62 · Test task task-1 / Changes requested",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
}
