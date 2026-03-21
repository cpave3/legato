package github

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

// mockExecCommand creates a function that returns exec.Cmd objects backed by
// the test binary itself. When the "mocked" command runs, it executes the
// TestHelperProcess test case which prints predefined output and exits.
func mockExecCommand(output string, exitCode int) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", name}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			fmt.Sprintf("GO_HELPER_OUTPUT=%s", output),
			fmt.Sprintf("GO_HELPER_EXIT_CODE=%d", exitCode),
		)
		return cmd
	}
}

// TestHelperProcess is the subprocess entry point used by mockExecCommand.
// It's not a real test — it's invoked as a subprocess.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	fmt.Fprint(os.Stdout, os.Getenv("GO_HELPER_OUTPUT"))
	exitCode := os.Getenv("GO_HELPER_EXIT_CODE")
	if exitCode == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}

func newTestClient(output string, exitCode int) *Client {
	return &Client{
		ghPath:      "gh",
		execCommand: mockExecCommand(output, exitCode),
	}
}

func TestNewReturnsErrorWhenGhNotFound(t *testing.T) {
	_, err := New(Options{
		LookPath: func(name string) (string, error) {
			return "", exec.ErrNotFound
		},
	})
	if err == nil {
		t.Error("expected error when gh not found")
	}
}

func TestNewSucceedsWhenGhFound(t *testing.T) {
	c, err := New(Options{
		LookPath: func(name string) (string, error) {
			return "/usr/bin/gh", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestFetchPRStatusWithOpenPR(t *testing.T) {
	output := `[{"number":42,"headRefName":"feature/auth","title":"Add auth","state":"OPEN","isDraft":false,"reviewDecision":"APPROVED","url":"https://github.com/owner/repo/pull/42","statusCheckRollup":[{"conclusion":"SUCCESS","status":"COMPLETED"}],"comments":[{},{}],"reviews":[{},{},{}]}]`
	c := newTestClient(output, 0)

	status, err := c.FetchPRStatus("feature/auth")
	if err != nil {
		t.Fatal(err)
	}
	if !status.HasPR {
		t.Error("expected HasPR=true")
	}
	if status.Number != 42 {
		t.Errorf("Number = %d, want 42", status.Number)
	}
	if status.State != "OPEN" {
		t.Errorf("State = %q, want OPEN", status.State)
	}
	if status.ReviewDecision != "APPROVED" {
		t.Errorf("ReviewDecision = %q, want APPROVED", status.ReviewDecision)
	}
	if status.CheckStatus != "pass" {
		t.Errorf("CheckStatus = %q, want pass", status.CheckStatus)
	}
	if status.CommentCount != 5 {
		t.Errorf("CommentCount = %d, want 5", status.CommentCount)
	}
	if status.URL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("URL = %q, want https://github.com/owner/repo/pull/42", status.URL)
	}
}

func TestFetchPRStatusNoPR(t *testing.T) {
	c := newTestClient("[]", 0)

	status, err := c.FetchPRStatus("no-pr-branch")
	if err != nil {
		t.Fatal(err)
	}
	if status.HasPR {
		t.Error("expected HasPR=false")
	}
}

func TestFetchPRStatusMerged(t *testing.T) {
	output := `[{"number":10,"headRefName":"feature/done","title":"Done","state":"MERGED","isDraft":false,"reviewDecision":"APPROVED","url":"https://github.com/owner/repo/pull/10","statusCheckRollup":[],"comments":[],"reviews":[]}]`
	c := newTestClient(output, 0)

	status, err := c.FetchPRStatus("feature/done")
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "MERGED" {
		t.Errorf("State = %q, want MERGED", status.State)
	}
}

func TestFetchPRStatusDraft(t *testing.T) {
	output := `[{"number":5,"headRefName":"wip","title":"WIP","state":"OPEN","isDraft":true,"reviewDecision":"","url":"https://github.com/owner/repo/pull/5","statusCheckRollup":[],"comments":[],"reviews":[]}]`
	c := newTestClient(output, 0)

	status, err := c.FetchPRStatus("wip")
	if err != nil {
		t.Fatal(err)
	}
	if !status.IsDraft {
		t.Error("expected IsDraft=true")
	}
}

func TestDeriveCheckStatus(t *testing.T) {
	tests := []struct {
		name   string
		checks []ghCheck
		want   string
	}{
		{"no checks", nil, ""},
		{"empty checks", []ghCheck{}, ""},
		{"all passing", []ghCheck{
			{Conclusion: "SUCCESS", Status: "COMPLETED"},
			{Conclusion: "NEUTRAL", Status: "COMPLETED"},
			{Conclusion: "SKIPPED", Status: "COMPLETED"},
		}, "pass"},
		{"one failure", []ghCheck{
			{Conclusion: "SUCCESS", Status: "COMPLETED"},
			{Conclusion: "FAILURE", Status: "COMPLETED"},
		}, "fail"},
		{"error conclusion", []ghCheck{
			{Conclusion: "ERROR", Status: "COMPLETED"},
		}, "fail"},
		{"cancelled", []ghCheck{
			{Conclusion: "CANCELLED", Status: "COMPLETED"},
		}, "fail"},
		{"timed out", []ghCheck{
			{Conclusion: "TIMED_OUT", Status: "COMPLETED"},
		}, "fail"},
		{"action required", []ghCheck{
			{Conclusion: "ACTION_REQUIRED", Status: "COMPLETED"},
		}, "fail"},
		{"pending - no conclusion", []ghCheck{
			{Conclusion: "SUCCESS", Status: "COMPLETED"},
			{Conclusion: "", Status: "IN_PROGRESS"},
		}, "pending"},
		{"pending - queued", []ghCheck{
			{Conclusion: "", Status: "QUEUED"},
		}, "pending"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveCheckStatus(tt.checks)
			if got != tt.want {
				t.Errorf("deriveCheckStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBatchFetchPRStatusEmpty(t *testing.T) {
	c := newTestClient("", 0)
	statuses, err := c.BatchFetchPRStatus(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 0 {
		t.Errorf("got %d statuses, want 0", len(statuses))
	}
}

func TestBatchFetchPRStatusMultiple(t *testing.T) {
	// For batch, each call gets the same mock output. That's OK for testing the fan-out logic.
	output := `[{"number":1,"headRefName":"branch","title":"PR","state":"OPEN","isDraft":false,"reviewDecision":"","url":"https://github.com/o/r/pull/1","statusCheckRollup":[],"comments":[],"reviews":[]}]`
	c := newTestClient(output, 0)

	branches := []string{"branch-a", "branch-b", "branch-c"}
	statuses, err := c.BatchFetchPRStatus(branches)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 3 {
		t.Errorf("got %d statuses, want 3", len(statuses))
	}
	for _, b := range branches {
		if _, ok := statuses[b]; !ok {
			t.Errorf("missing status for branch %s", b)
		}
	}
}

func TestDetectRepoSSH(t *testing.T) {
	c := newTestClient("git@github.com:owner/repo.git\n", 0)
	owner, repo, err := c.DetectRepo()
	if err != nil {
		t.Fatal(err)
	}
	if owner != "owner" {
		t.Errorf("owner = %q, want owner", owner)
	}
	if repo != "repo" {
		t.Errorf("repo = %q, want repo", repo)
	}
}

func TestDetectRepoHTTPS(t *testing.T) {
	c := newTestClient("https://github.com/myorg/myrepo.git\n", 0)
	owner, repo, err := c.DetectRepo()
	if err != nil {
		t.Fatal(err)
	}
	if owner != "myorg" {
		t.Errorf("owner = %q, want myorg", owner)
	}
	if repo != "myrepo" {
		t.Errorf("repo = %q, want myrepo", repo)
	}
}

func TestDetectRepoHTTPSNoGitSuffix(t *testing.T) {
	c := newTestClient("https://github.com/myorg/myrepo\n", 0)
	owner, repo, err := c.DetectRepo()
	if err != nil {
		t.Fatal(err)
	}
	if owner != "myorg" {
		t.Errorf("owner = %q, want myorg", owner)
	}
	if repo != "myrepo" {
		t.Errorf("repo = %q, want myrepo", repo)
	}
}

func TestDetectRepoNoRemote(t *testing.T) {
	c := newTestClient("fatal: no such remote 'origin'\n", 1)
	_, _, err := c.DetectRepo()
	if err == nil {
		t.Error("expected error when no remote")
	}
}

func TestDetectBranch(t *testing.T) {
	c := newTestClient("feature/auth\n", 0)
	branch, err := c.DetectBranch()
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feature/auth" {
		t.Errorf("branch = %q, want feature/auth", branch)
	}
}

func TestDetectBranchNotGitRepo(t *testing.T) {
	c := newTestClient("fatal: not a git repository\n", 1)
	_, err := c.DetectBranch()
	if err == nil {
		t.Error("expected error when not a git repo")
	}
}

func TestFetchCommentCount(t *testing.T) {
	c := newTestClient("7\n", 0)
	count, err := c.FetchCommentCount("owner", "repo", 42)
	if err != nil {
		t.Fatal(err)
	}
	if count != 7 {
		t.Errorf("count = %d, want 7", count)
	}
}

func TestFetchCommentCountZero(t *testing.T) {
	c := newTestClient("0\n", 0)
	count, err := c.FetchCommentCount("owner", "repo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}

func TestFetchPRByNumber(t *testing.T) {
	output := `{"number":42,"title":"Add auth","state":"open","draft":false,"merged":false,"html_url":"https://github.com/owner/repo/pull/42","comments":2,"review_comments":3,"head":{"ref":"feature/auth"}}`
	c := newTestClient(output, 0)

	status, err := c.FetchPRByNumber("owner", "repo", 42)
	if err != nil {
		t.Fatal(err)
	}
	if !status.HasPR {
		t.Error("expected HasPR=true")
	}
	if status.Number != 42 {
		t.Errorf("Number = %d, want 42", status.Number)
	}
	if status.Title != "Add auth" {
		t.Errorf("Title = %q, want Add auth", status.Title)
	}
	if status.State != "OPEN" {
		t.Errorf("State = %q, want OPEN", status.State)
	}
	if status.HeadBranch != "feature/auth" {
		t.Errorf("HeadBranch = %q, want feature/auth", status.HeadBranch)
	}
	if status.CommentCount != 5 {
		t.Errorf("CommentCount = %d, want 5", status.CommentCount)
	}
	if status.URL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("URL = %q", status.URL)
	}
}

func TestFetchPRByNumberMerged(t *testing.T) {
	output := `{"number":10,"title":"Done","state":"closed","draft":false,"merged":true,"html_url":"https://github.com/o/r/pull/10","comments":0,"review_comments":0,"head":{"ref":"feature/done"}}`
	c := newTestClient(output, 0)

	status, err := c.FetchPRByNumber("o", "r", 10)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != "MERGED" {
		t.Errorf("State = %q, want MERGED", status.State)
	}
}

func TestFetchPRByNumberDraft(t *testing.T) {
	output := `{"number":5,"title":"WIP","state":"open","draft":true,"merged":false,"html_url":"https://github.com/o/r/pull/5","comments":0,"review_comments":0,"head":{"ref":"wip"}}`
	c := newTestClient(output, 0)

	status, err := c.FetchPRByNumber("o", "r", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !status.IsDraft {
		t.Error("expected IsDraft=true")
	}
}

func TestFetchPRByNumberNotFound(t *testing.T) {
	c := newTestClient("Not Found\n", 1)
	_, err := c.FetchPRByNumber("owner", "repo", 999)
	if err == nil {
		t.Error("expected error for non-existent PR")
	}
}

// TestDetectRepoSSHNoGitSuffix tests SSH URL without .git suffix
func TestDetectRepoSSHNoGitSuffix(t *testing.T) {
	c := newTestClient("git@github.com:owner/repo\n", 0)
	owner, repo, err := c.DetectRepo()
	if err != nil {
		t.Fatal(err)
	}
	if owner != "owner" {
		t.Errorf("owner = %q, want owner", owner)
	}
	if repo != "repo" {
		t.Errorf("repo = %q, want repo", repo)
	}
}

