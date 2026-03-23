package service

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/github"
	"github.com/cpave3/legato/internal/engine/store"
)

type mockGitHub struct {
	statuses       map[string]*github.PRStatus
	prByNumber     map[int]*github.PRStatus
	fetchErr       error
	branch         string
	branchErr      error
	owner, repo    string
	repoErr        error
	commentCnt     int
	commentErr     error
	queriedBranches []string // tracks branches passed to BatchFetchPRStatusWithRepo
}

func (m *mockGitHub) FetchPRStatus(branch string, repo ...string) (*github.PRStatus, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	if s, ok := m.statuses[branch]; ok {
		return s, nil
	}
	return &github.PRStatus{HasPR: false}, nil
}

func (m *mockGitHub) FetchPRByNumber(owner, repo string, prNumber int) (*github.PRStatus, error) {
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	if m.prByNumber != nil {
		if s, ok := m.prByNumber[prNumber]; ok {
			return s, nil
		}
	}
	return nil, fmt.Errorf("PR #%d not found", prNumber)
}

func (m *mockGitHub) BatchFetchPRStatus(branches []string) (map[string]*github.PRStatus, error) {
	result := make(map[string]*github.PRStatus)
	for _, b := range branches {
		s, err := m.FetchPRStatus(b)
		if err != nil {
			return result, err
		}
		result[b] = s
	}
	return result, nil
}

func (m *mockGitHub) BatchFetchPRStatusWithRepo(queries []github.BranchQuery) (map[string]*github.PRStatus, error) {
	result := make(map[string]*github.PRStatus)
	for _, q := range queries {
		m.queriedBranches = append(m.queriedBranches, q.Branch)
		s, err := m.FetchPRStatus(q.Branch, q.Repo)
		if err != nil {
			return result, err
		}
		result[q.Branch] = s
	}
	return result, nil
}

func (m *mockGitHub) DetectBranch() (string, error) {
	return m.branch, m.branchErr
}

func (m *mockGitHub) DetectRepo() (string, string, error) {
	return m.owner, m.repo, m.repoErr
}

func (m *mockGitHub) FetchCommentCount(owner, repo string, prNumber int) (int, error) {
	return m.commentCnt, m.commentErr
}

func newTestPRStore(t *testing.T) *store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func createPRTask(t *testing.T, s *store.Store, id string) {
	t.Helper()
	ctx := context.Background()
	err := s.CreateTask(ctx, store.Task{
		ID:        id,
		Title:     "Test task " + id,
		Status:    "Doing",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLinkBranch(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	gh := &mockGitHub{
		statuses: map[string]*github.PRStatus{
			"feature/auth": {HasPR: true, Number: 42, State: "OPEN", URL: "https://github.com/o/r/pull/42"},
		},
	}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute)

	ctx := context.Background()
	createPRTask(t, s, "task1")

	if err := svc.LinkBranch(ctx, "task1", "feature/auth", ""); err != nil {
		t.Fatal(err)
	}

	// Verify pr_meta was set
	task, err := s.GetTask(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	if task.PRMeta == nil {
		t.Fatal("expected pr_meta to be set")
	}

	meta, err := store.ParsePRMeta(task.PRMeta)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Branch != "feature/auth" {
		t.Errorf("Branch = %q, want feature/auth", meta.Branch)
	}
}

func TestUnlinkBranch(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	gh := &mockGitHub{}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute)

	ctx := context.Background()
	createPRTask(t, s, "task1")

	// Link then unlink
	if err := svc.LinkBranch(ctx, "task1", "feature/auth", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.UnlinkBranch(ctx, "task1"); err != nil {
		t.Fatal(err)
	}

	task, err := s.GetTask(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	if task.PRMeta != nil {
		t.Errorf("expected pr_meta to be nil, got %v", task.PRMeta)
	}
}

func TestUnlinkBranchIdempotent(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	gh := &mockGitHub{}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute)

	ctx := context.Background()
	createPRTask(t, s, "task1")

	// Unlink when nothing is linked
	if err := svc.UnlinkBranch(ctx, "task1"); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestPollOnceUpdatesTrackedTasks(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	sub := bus.Subscribe(events.EventPRStatusUpdated)

	gh := &mockGitHub{
		statuses: map[string]*github.PRStatus{
			"feature/auth": {HasPR: true, Number: 42, State: "OPEN", CheckStatus: "pass", ReviewDecision: "APPROVED", URL: "https://github.com/o/r/pull/42"},
		},
	}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute)

	ctx := context.Background()
	createPRTask(t, s, "task1")

	// Set initial pr_meta with branch and repo
	prMeta := `{"branch":"feature/auth","repo":"o/r"}`
	if err := s.UpdatePRMeta(ctx, "task1", &prMeta); err != nil {
		t.Fatal(err)
	}

	if err := svc.PollOnce(ctx); err != nil {
		t.Fatal(err)
	}

	// Check task was updated
	task, err := s.GetTask(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := store.ParsePRMeta(task.PRMeta)
	if err != nil {
		t.Fatal(err)
	}
	if meta.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", meta.PRNumber)
	}
	if meta.CheckStatus != "pass" {
		t.Errorf("CheckStatus = %q, want pass", meta.CheckStatus)
	}
	if meta.ReviewDecision != "APPROVED" {
		t.Errorf("ReviewDecision = %q, want APPROVED", meta.ReviewDecision)
	}

	// Check event was published
	select {
	case <-sub:
		// good
	case <-time.After(time.Second):
		t.Error("expected EventPRStatusUpdated event")
	}
}

func TestPollOnceNoTrackedTasks(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	gh := &mockGitHub{}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute)

	ctx := context.Background()
	createPRTask(t, s, "task1") // no pr_meta

	if err := svc.PollOnce(ctx); err != nil {
		t.Fatal(err)
	}
	// No error, no crash — just skip
}

func TestPollOnceNoPRFound(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	gh := &mockGitHub{} // no statuses
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute)

	ctx := context.Background()
	createPRTask(t, s, "task1")

	prMeta := `{"branch":"no-pr-branch","repo":"o/r"}`
	if err := s.UpdatePRMeta(ctx, "task1", &prMeta); err != nil {
		t.Fatal(err)
	}

	if err := svc.PollOnce(ctx); err != nil {
		t.Fatal(err)
	}

	// pr_meta should retain branch but have zero PR fields
	task, err := s.GetTask(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := store.ParsePRMeta(task.PRMeta)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Branch != "no-pr-branch" {
		t.Errorf("Branch = %q, want no-pr-branch", meta.Branch)
	}
	if meta.PRNumber != 0 {
		t.Errorf("PRNumber = %d, want 0", meta.PRNumber)
	}
}

func TestGetPRStatus(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	gh := &mockGitHub{}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute)

	ctx := context.Background()
	createPRTask(t, s, "task1")

	prMeta := `{"branch":"feature/auth","pr_number":42,"check_status":"pass"}`
	if err := s.UpdatePRMeta(ctx, "task1", &prMeta); err != nil {
		t.Fatal(err)
	}

	meta, err := svc.GetPRStatus(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	if meta.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", meta.PRNumber)
	}
	if meta.CheckStatus != "pass" {
		t.Errorf("CheckStatus = %q, want pass", meta.CheckStatus)
	}
}

func TestGetPRStatusNoPRMeta(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	gh := &mockGitHub{}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute)

	ctx := context.Background()
	createPRTask(t, s, "task1")

	meta, err := svc.GetPRStatus(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	if meta != nil {
		t.Errorf("expected nil, got %v", meta)
	}
}

func TestAutoLinkBranch(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	gh := &mockGitHub{
		branch: "feature/auto",
		owner:  "myorg",
		repo:   "myrepo",
		statuses: map[string]*github.PRStatus{
			"feature/auto": {HasPR: false},
		},
	}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute).(*prTrackingService)

	ctx := context.Background()
	createPRTask(t, s, "task1")

	svc.AutoLinkBranch(ctx, "task1")

	// Wait briefly for async poll
	time.Sleep(50 * time.Millisecond)

	task, err := s.GetTask(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	if task.PRMeta == nil {
		t.Fatal("expected pr_meta to be set after auto-link")
	}
	meta, err := store.ParsePRMeta(task.PRMeta)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Branch != "feature/auto" {
		t.Errorf("Branch = %q, want feature/auto", meta.Branch)
	}
	if meta.Repo != "myorg/myrepo" {
		t.Errorf("Repo = %q, want myorg/myrepo", meta.Repo)
	}
}

func TestAutoLinkBranchSkipsWhenRepoDetectionFails(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	gh := &mockGitHub{
		branch:  "main",
		repoErr: fmt.Errorf("not a git repository"),
	}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute).(*prTrackingService)

	ctx := context.Background()
	createPRTask(t, s, "task1")

	svc.AutoLinkBranch(ctx, "task1")
	time.Sleep(50 * time.Millisecond)

	task, err := s.GetTask(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	if task.PRMeta != nil {
		t.Error("expected pr_meta to remain nil when repo detection fails")
	}
}

func TestAutoLinkBranchSkipsExisting(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	gh := &mockGitHub{branch: "feature/new"}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute).(*prTrackingService)

	ctx := context.Background()
	createPRTask(t, s, "task1")

	// Pre-link a different branch
	prMeta := `{"branch":"feature/existing"}`
	if err := s.UpdatePRMeta(ctx, "task1", &prMeta); err != nil {
		t.Fatal(err)
	}

	svc.AutoLinkBranch(ctx, "task1")

	// Should still have the original branch
	task, err := s.GetTask(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	meta, err := store.ParsePRMeta(task.PRMeta)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Branch != "feature/existing" {
		t.Errorf("Branch = %q, want feature/existing (should not overwrite)", meta.Branch)
	}
}

func TestPollOnceSkipsBranchesWithNoRepo(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()

	gh := &mockGitHub{
		statuses: map[string]*github.PRStatus{
			"feature/no-repo":   {HasPR: true, Number: 1, State: "OPEN", URL: "https://github.com/o/r/pull/1"},
			"feature/with-repo": {HasPR: true, Number: 2, State: "OPEN", URL: "https://github.com/o/r/pull/2"},
		},
	}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute)

	ctx := context.Background()

	// Task with branch but no repo — should be skipped
	createPRTask(t, s, "task-no-repo")
	noRepoMeta := `{"branch":"feature/no-repo"}`
	if err := s.UpdatePRMeta(ctx, "task-no-repo", &noRepoMeta); err != nil {
		t.Fatal(err)
	}

	// Task with branch and repo — should be polled
	createPRTask(t, s, "task-with-repo")
	withRepoMeta := `{"branch":"feature/with-repo","repo":"o/r"}`
	if err := s.UpdatePRMeta(ctx, "task-with-repo", &withRepoMeta); err != nil {
		t.Fatal(err)
	}

	if err := svc.PollOnce(ctx); err != nil {
		t.Fatal(err)
	}

	// Only the branch with repo should have been queried
	if len(gh.queriedBranches) != 1 {
		t.Fatalf("expected 1 queried branch, got %d: %v", len(gh.queriedBranches), gh.queriedBranches)
	}
	if gh.queriedBranches[0] != "feature/with-repo" {
		t.Errorf("expected feature/with-repo, got %s", gh.queriedBranches[0])
	}
}

func TestStartPollingAndStop(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	gh := &mockGitHub{}
	svc := NewPRTrackingService(s, bus, gh, 10*time.Millisecond, 10*time.Millisecond)

	ctx := context.Background()
	stop := svc.StartPolling(ctx)

	// Let it tick once
	time.Sleep(30 * time.Millisecond)

	// Stop should not panic
	stop()
}

func TestLinkPR(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	sub := bus.Subscribe(events.EventPRStatusUpdated)

	gh := &mockGitHub{
		prByNumber: map[int]*github.PRStatus{
			42: {HasPR: true, Number: 42, Title: "Add auth", State: "OPEN", URL: "https://github.com/owner/repo/pull/42", HeadBranch: "feature/auth", CommentCount: 3},
		},
	}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute)

	ctx := context.Background()
	createPRTask(t, s, "task1")

	if err := svc.LinkPR(ctx, "task1", "owner", "repo", 42); err != nil {
		t.Fatal(err)
	}

	// Verify pr_meta was set with full data
	task, err := s.GetTask(ctx, "task1")
	if err != nil {
		t.Fatal(err)
	}
	if task.PRMeta == nil {
		t.Fatal("expected pr_meta to be set")
	}

	meta, err := store.ParsePRMeta(task.PRMeta)
	if err != nil {
		t.Fatal(err)
	}
	if meta.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", meta.PRNumber)
	}
	if meta.Branch != "feature/auth" {
		t.Errorf("Branch = %q, want feature/auth", meta.Branch)
	}
	if meta.State != "OPEN" {
		t.Errorf("State = %q, want OPEN", meta.State)
	}

	// Check event was published
	select {
	case <-sub:
		// good
	case <-time.After(time.Second):
		t.Error("expected EventPRStatusUpdated event")
	}
}

func TestLinkPRNotFound(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	gh := &mockGitHub{}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute)

	ctx := context.Background()
	createPRTask(t, s, "task1")

	err := svc.LinkPR(ctx, "task1", "owner", "repo", 999)
	if err == nil {
		t.Error("expected error for non-existent PR")
	}
}

func TestPRMetaChanged(t *testing.T) {
	base := &store.PRMeta{Branch: "main", PRNumber: 1, State: "OPEN", CheckStatus: "pass"}

	if !prMetaChanged(nil, base) {
		t.Error("nil old should be changed")
	}
	if prMetaChanged(base, base) {
		t.Error("same values should not be changed")
	}

	modified := *base
	modified.CheckStatus = "fail"
	if !prMetaChanged(base, &modified) {
		t.Error("different check_status should be changed")
	}
}

func TestPollAllIncludesResolvedPRs(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()
	sub := bus.Subscribe(events.EventPRStatusUpdated)

	gh := &mockGitHub{
		statuses: map[string]*github.PRStatus{
			"feature/resolved": {HasPR: true, Number: 99, State: "MERGED", URL: "https://github.com/o/r/pull/99"},
		},
	}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute)

	ctx := context.Background()
	createPRTask(t, s, "task1")

	// Set pr_meta with a PR number (resolved PR)
	prMeta := `{"branch":"feature/resolved","pr_number":99,"state":"OPEN","repo":"o/r"}`
	if err := s.UpdatePRMeta(ctx, "task1", &prMeta); err != nil {
		t.Fatal(err)
	}

	if err := svc.PollAll(ctx); err != nil {
		t.Fatal(err)
	}

	// Verify the resolved branch was queried (not skipped)
	if len(gh.queriedBranches) == 0 {
		t.Fatal("PollAll should query resolved PRs, but no branches were queried")
	}
	found := false
	for _, b := range gh.queriedBranches {
		if b == "feature/resolved" {
			found = true
		}
	}
	if !found {
		t.Errorf("PollAll should have queried feature/resolved, queried: %v", gh.queriedBranches)
	}

	// Verify event was published (state changed from OPEN to MERGED)
	select {
	case <-sub:
		// good
	case <-time.After(time.Second):
		t.Error("expected EventPRStatusUpdated event")
	}
}

func TestPollOnceSkipsResolvedPRs(t *testing.T) {
	s := newTestPRStore(t)
	bus := events.New()

	gh := &mockGitHub{
		statuses: map[string]*github.PRStatus{
			"feature/unresolved": {HasPR: false},
			"feature/resolved":   {HasPR: true, Number: 42, State: "OPEN", URL: "https://github.com/o/r/pull/42"},
		},
	}
	svc := NewPRTrackingService(s, bus, gh, time.Minute, 10*time.Minute).(*prTrackingService)

	ctx := context.Background()

	// Task with unresolved PR (branch only, no PR number)
	createPRTask(t, s, "task-unresolved")
	unresolvedMeta := `{"branch":"feature/unresolved","repo":"o/r"}`
	if err := s.UpdatePRMeta(ctx, "task-unresolved", &unresolvedMeta); err != nil {
		t.Fatal(err)
	}

	// Task with resolved PR (has PR number)
	createPRTask(t, s, "task-resolved")
	resolvedMeta := `{"branch":"feature/resolved","pr_number":42,"repo":"o/r"}`
	if err := s.UpdatePRMeta(ctx, "task-resolved", &resolvedMeta); err != nil {
		t.Fatal(err)
	}

	// Set lastResolvedPoll to now — resolved PRs should be skipped
	svc.mu.Lock()
	svc.lastResolvedPoll = time.Now()
	svc.mu.Unlock()

	if err := svc.PollOnce(ctx); err != nil {
		t.Fatal(err)
	}

	// Only the unresolved branch should have been queried
	if len(gh.queriedBranches) != 1 {
		t.Fatalf("expected 1 queried branch, got %d: %v", len(gh.queriedBranches), gh.queriedBranches)
	}
	if gh.queriedBranches[0] != "feature/unresolved" {
		t.Errorf("expected feature/unresolved, got %s", gh.queriedBranches[0])
	}

	// Now simulate resolvedInterval elapsed — both should be queried
	gh.queriedBranches = nil
	svc.mu.Lock()
	svc.lastResolvedPoll = time.Now().Add(-11 * time.Minute)
	svc.mu.Unlock()

	if err := svc.PollOnce(ctx); err != nil {
		t.Fatal(err)
	}

	if len(gh.queriedBranches) != 2 {
		t.Fatalf("expected 2 queried branches after interval elapsed, got %d: %v", len(gh.queriedBranches), gh.queriedBranches)
	}
}
