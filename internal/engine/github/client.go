package github

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

// PRStatus holds the state of a pull request for a given branch.
type PRStatus struct {
	HasPR          bool
	Number         int
	Title          string
	URL            string
	State          string // OPEN, MERGED, CLOSED
	IsDraft        bool
	ReviewDecision string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, ""
	CheckStatus    string // pass, fail, pending, ""
	CommentCount   int
	HeadBranch     string // branch name (populated by FetchPRByNumber)
}

// Options configures the GitHub Client.
type Options struct {
	// LookPath overrides exec.LookPath for testing.
	LookPath func(name string) (string, error)
	// ExecCommand overrides exec.Command for testing.
	ExecCommand func(name string, args ...string) *exec.Cmd
}

// Client wraps gh and git CLI operations for PR status queries.
type Client struct {
	ghPath      string
	execCommand func(name string, args ...string) *exec.Cmd
}

// New creates a Client, verifying gh is available.
func New(opts Options) (*Client, error) {
	lookup := opts.LookPath
	if lookup == nil {
		lookup = exec.LookPath
	}

	path, err := lookup("gh")
	if err != nil {
		return nil, fmt.Errorf("gh CLI not found — install from https://cli.github.com: %w", err)
	}

	execCmd := opts.ExecCommand
	if execCmd == nil {
		execCmd = exec.Command
	}

	return &Client{
		ghPath:      path,
		execCommand: execCmd,
	}, nil
}

// CheckAvailable verifies the gh CLI is accessible.
func (c *Client) CheckAvailable() error {
	cmd := c.execCommand(c.ghPath, "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh CLI not accessible: %w", err)
	}
	return nil
}

// ghPR is the JSON shape returned by gh pr list.
type ghPR struct {
	Number          int           `json:"number"`
	HeadRefName     string        `json:"headRefName"`
	Title           string        `json:"title"`
	State           string        `json:"state"`
	IsDraft         bool          `json:"isDraft"`
	ReviewDecision  string        `json:"reviewDecision"`
	URL             string        `json:"url"`
	StatusCheckRollup []ghCheck        `json:"statusCheckRollup"`
	Comments          json.RawMessage  `json:"comments"`
	ReviewComments    json.RawMessage  `json:"reviews"`
}

type ghCheck struct {
	Conclusion string `json:"conclusion"`
	Status     string `json:"status"`
}

// FetchPRStatus queries GitHub for the PR associated with a branch.
// If repo is non-empty (owner/repo format), it scopes the query to that repo.
func (c *Client) FetchPRStatus(branch string, repo ...string) (*PRStatus, error) {
	args := []string{"pr", "list",
		"--head", branch,
		"--state", "all",
		"--limit", "1",
		"--json", "number,headRefName,title,state,isDraft,reviewDecision,url,statusCheckRollup,comments,reviews"}
	if len(repo) > 0 && repo[0] != "" {
		args = append(args, "--repo", repo[0])
	}
	cmd := c.execCommand(c.ghPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh pr list: %s: %w", strings.TrimSpace(string(out)), err)
	}

	var prs []ghPR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("parsing gh output: %w", err)
	}

	if len(prs) == 0 {
		return &PRStatus{HasPR: false}, nil
	}

	pr := prs[0]
	return &PRStatus{
		HasPR:          true,
		Number:         pr.Number,
		Title:          pr.Title,
		URL:            pr.URL,
		State:          pr.State,
		IsDraft:        pr.IsDraft,
		ReviewDecision: pr.ReviewDecision,
		CheckStatus:    deriveCheckStatus(pr.StatusCheckRollup),
		CommentCount:   countJSONArray(pr.Comments) + countJSONArray(pr.ReviewComments),
	}, nil
}

// countJSONArray returns the length of a JSON array, or 0 if not an array.
func countJSONArray(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		return len(arr)
	}
	// Fallback: might be {totalCount: N} format
	var obj struct{ TotalCount int `json:"totalCount"` }
	if json.Unmarshal(raw, &obj) == nil {
		return obj.TotalCount
	}
	return 0
}

// deriveCheckStatus aggregates CI check conclusions into a single status.
func deriveCheckStatus(checks []ghCheck) string {
	if len(checks) == 0 {
		return ""
	}

	for _, c := range checks {
		switch strings.ToUpper(c.Conclusion) {
		case "FAILURE", "ERROR", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED":
			return "fail"
		}
	}

	for _, c := range checks {
		if c.Conclusion == "" && strings.ToUpper(c.Status) != "COMPLETED" {
			return "pending"
		}
	}

	return "pass"
}

// BranchQuery pairs a branch name with an optional repo for batch fetching.
type BranchQuery struct {
	Branch string
	Repo   string // owner/repo format, empty to use current repo context
}

// BatchFetchPRStatus fetches PR status for multiple branches concurrently.
// Concurrency is limited to 5 at a time.
func (c *Client) BatchFetchPRStatus(branches []string) (map[string]*PRStatus, error) {
	queries := make([]BranchQuery, len(branches))
	for i, b := range branches {
		queries[i] = BranchQuery{Branch: b}
	}
	return c.BatchFetchPRStatusWithRepo(queries)
}

// BatchFetchPRStatusWithRepo fetches PR status for multiple branches with per-branch repo scoping.
func (c *Client) BatchFetchPRStatusWithRepo(queries []BranchQuery) (map[string]*PRStatus, error) {
	if len(queries) == 0 {
		return map[string]*PRStatus{}, nil
	}

	type result struct {
		branch string
		status *PRStatus
		err    error
	}

	sem := make(chan struct{}, 5)
	results := make(chan result, len(queries))

	var wg sync.WaitGroup
	for _, q := range queries {
		wg.Add(1)
		go func(query BranchQuery) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			status, err := c.FetchPRStatus(query.Branch, query.Repo)
			results <- result{branch: query.Branch, status: status, err: err}
		}(q)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	statuses := make(map[string]*PRStatus)
	var errs []string
	for r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", r.branch, r.err))
			continue
		}
		statuses[r.branch] = r.status
	}

	if len(errs) > 0 {
		return statuses, fmt.Errorf("batch PR fetch errors: %s", strings.Join(errs, "; "))
	}
	return statuses, nil
}

// FetchPRByNumber queries GitHub for a PR by its number using the REST API.
func (c *Client) FetchPRByNumber(owner, repo string, prNumber int) (*PRStatus, error) {
	cmd := c.execCommand(c.ghPath, "api",
		fmt.Sprintf("repos/%s/%s/pulls/%d", owner, repo, prNumber))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("gh api: %s: %w", strings.TrimSpace(string(out)), err)
	}

	var raw struct {
		Number         int    `json:"number"`
		Title          string `json:"title"`
		State          string `json:"state"`
		Draft          bool   `json:"draft"`
		Merged         bool   `json:"merged"`
		HTMLURL        string `json:"html_url"`
		Comments       int    `json:"comments"`
		ReviewComments int    `json:"review_comments"`
		Head           struct {
			Ref string `json:"ref"`
		} `json:"head"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parsing PR response: %w", err)
	}

	state := strings.ToUpper(raw.State)
	if raw.Merged {
		state = "MERGED"
	}

	return &PRStatus{
		HasPR:        true,
		Number:       raw.Number,
		Title:        raw.Title,
		URL:          raw.HTMLURL,
		State:        state,
		IsDraft:      raw.Draft,
		CheckStatus:  "",
		CommentCount: raw.Comments + raw.ReviewComments,
		HeadBranch:   raw.Head.Ref,
	}, nil
}

// FetchCommentCount returns the total comment count for a PR.
func (c *Client) FetchCommentCount(owner, repo string, prNumber int) (int, error) {
	cmd := c.execCommand(c.ghPath, "api",
		fmt.Sprintf("repos/%s/%s/pulls/%d", owner, repo, prNumber),
		"--jq", ".comments + .review_comments")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("gh api: %s: %w", strings.TrimSpace(string(out)), err)
	}

	var count int
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &count); err != nil {
		return 0, fmt.Errorf("parsing comment count: %w", err)
	}
	return count, nil
}

var (
	sshRemoteRe   = regexp.MustCompile(`git@github\.com:([^/]+)/(.+?)(?:\.git)?$`)
	httpsRemoteRe = regexp.MustCompile(`https://github\.com/([^/]+)/(.+?)(?:\.git)?$`)
)

// DetectRepo returns the GitHub owner and repo from the git remote.
func (c *Client) DetectRepo() (owner, repo string, err error) {
	cmd := c.execCommand("git", "remote", "get-url", "origin")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("detecting repo: %s: %w", strings.TrimSpace(string(out)), err)
	}

	url := strings.TrimSpace(string(out))

	if m := sshRemoteRe.FindStringSubmatch(url); m != nil {
		return m[1], m[2], nil
	}
	if m := httpsRemoteRe.FindStringSubmatch(url); m != nil {
		return m[1], m[2], nil
	}

	return "", "", fmt.Errorf("could not parse GitHub remote URL: %s", url)
}

// DetectBranch returns the current git branch name.
func (c *Client) DetectBranch() (string, error) {
	cmd := c.execCommand("git", "rev-parse", "--abbrev-ref", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("detecting branch: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}
