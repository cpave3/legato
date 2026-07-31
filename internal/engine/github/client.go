package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultBaseURL = "https://api.github.com"

// PRStatus holds the state of a pull request.
type PRStatus struct {
	HasPR          bool
	Number         int
	Title          string
	URL            string
	State          string
	IsDraft        bool
	ReviewDecision string
	CheckStatus    string
	CommentCount   int
	HeadBranch     string
	HeadSHA        string
	HeadRepo       string
	CreatedAt      string
}

// ErrorKind classifies failures that callers can surface without parsing text.
type ErrorKind string

const (
	ErrorAuthentication ErrorKind = "authentication"
	ErrorAuthorization  ErrorKind = "authorization"
	ErrorNotFound       ErrorKind = "not_found"
	ErrorRateLimit      ErrorKind = "rate_limit"
	ErrorServer         ErrorKind = "server"
	ErrorResponse       ErrorKind = "response"
)

// APIError is a classified GitHub API failure.
type APIError struct {
	Kind       ErrorKind
	StatusCode int
	Message    string
	ResetAt    time.Time
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("GitHub API: %s", e.Kind)
	}
	return fmt.Sprintf("GitHub API: %s: %s", e.Kind, e.Message)
}

// Options configures a GitHub client.
type Options struct {
	Token       string
	BaseURL     string
	HTTPClient  *http.Client
	ExecCommand func(name string, args ...string) *exec.Cmd
	// LookPath remains for source compatibility with callers migrating from the
	// old gh-backed client. It is not consulted.
	LookPath func(name string) (string, error)
}

// Client performs token-authenticated GitHub API reads and local git inspection.
type Client struct {
	token       string
	baseURL     string
	httpClient  *http.Client
	execCommand func(name string, args ...string) *exec.Cmd
}

// New creates an HTTP GitHub client. A token is required so GitHub support is
// explicitly enabled rather than depending on ambient gh authentication.
func New(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.Token) == "" {
		return nil, errors.New("GitHub token is required")
	}
	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("invalid GitHub API base URL: %w", err)
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	execCommand := opts.ExecCommand
	if execCommand == nil {
		execCommand = exec.Command
	}
	return &Client{token: opts.Token, baseURL: baseURL, httpClient: httpClient, execCommand: execCommand}, nil
}

// CheckAvailable verifies authentication with a lightweight API request.
func (c *Client) CheckAvailable() error {
	var out struct {
		Login string `json:"login"`
	}
	return c.get("/user", nil, &out)
}

type apiPR struct {
	Number         int     `json:"number"`
	Title          string  `json:"title"`
	State          string  `json:"state"`
	Draft          bool    `json:"draft"`
	Merged         bool    `json:"merged"`
	MergedAt       *string `json:"merged_at"`
	HTMLURL        string  `json:"html_url"`
	CreatedAt      string  `json:"created_at"`
	Comments       int     `json:"comments"`
	ReviewComments int     `json:"review_comments"`
	Head           struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
}

func statusFromAPI(pr apiPR) PRStatus {
	state := strings.ToUpper(pr.State)
	if pr.Merged || pr.MergedAt != nil {
		state = "MERGED"
	}
	return PRStatus{
		HasPR: true, Number: pr.Number, Title: pr.Title, URL: pr.HTMLURL,
		State: state, IsDraft: pr.Draft, CommentCount: pr.Comments + pr.ReviewComments,
		HeadBranch: pr.Head.Ref, HeadSHA: pr.Head.SHA, HeadRepo: pr.Head.Repo.FullName,
		CreatedAt: pr.CreatedAt,
	}
}

// ListRepositoryPRs returns pull requests for one repository in newest-first
// order. The bounded result is used once per repository during discovery.
func (c *Client) ListRepositoryPRs(owner, repo string) ([]PRStatus, error) {
	var raw []apiPR
	query := url.Values{"state": {"all"}, "sort": {"created"}, "direction": {"desc"}, "per_page": {"100"}}
	if err := c.get(fmt.Sprintf("/repos/%s/%s/pulls", url.PathEscape(owner), url.PathEscape(repo)), query, &raw); err != nil {
		return nil, err
	}
	statuses := make([]PRStatus, len(raw))
	for i, pr := range raw {
		statuses[i] = statusFromAPI(pr)
	}
	return statuses, nil
}

// FetchPRStatus finds the best PR with the exact head branch in a repository.
func (c *Client) FetchPRStatus(branch string, repositories ...string) (*PRStatus, error) {
	if len(repositories) == 0 || repositories[0] == "" {
		return nil, errors.New("repository is required for PR lookup")
	}
	owner, repo, ok := splitRepo(repositories[0])
	if !ok {
		return nil, fmt.Errorf("repository must be owner/name")
	}
	prs, err := c.ListRepositoryPRs(owner, repo)
	if err != nil {
		return nil, err
	}
	matches := make([]PRStatus, 0)
	for _, pr := range prs {
		if pr.HeadBranch == branch {
			matches = append(matches, pr)
		}
	}
	return pickBestStatus(matches), nil
}

// FetchPRByNumber fetches a single PR by repository and number.
func (c *Client) FetchPRByNumber(owner, repo string, number int) (*PRStatus, error) {
	var raw apiPR
	if err := c.get(fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), number), nil, &raw); err != nil {
		return nil, err
	}
	status := statusFromAPI(raw)
	var enrichErr error
	status.ReviewDecision, enrichErr = c.fetchReviewDecision(owner, repo, number)
	if enrichErr != nil {
		return nil, fmt.Errorf("fetching PR reviews: %w", enrichErr)
	}
	if status.HeadSHA != "" {
		status.CheckStatus, enrichErr = c.fetchCheckStatus(owner, repo, status.HeadSHA)
		if enrichErr != nil {
			return nil, fmt.Errorf("fetching PR checks: %w", enrichErr)
		}
	}
	return &status, nil
}

func (c *Client) fetchReviewDecision(owner, repo string, number int) (string, error) {
	var reviews []struct {
		State       string `json:"state"`
		SubmittedAt string `json:"submitted_at"`
		User        struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", url.PathEscape(owner), url.PathEscape(repo), number)
	if err := c.get(path, url.Values{"per_page": {"100"}}, &reviews); err != nil {
		return "", err
	}
	latest := make(map[string]struct{ state, submitted string })
	for _, review := range reviews {
		if review.User.Login == "" || review.SubmittedAt == "" || review.State == "COMMENTED" || review.State == "DISMISSED" || review.State == "PENDING" {
			continue
		}
		current := latest[review.User.Login]
		if review.SubmittedAt > current.submitted {
			latest[review.User.Login] = struct{ state, submitted string }{review.State, review.SubmittedAt}
		}
	}
	approved := false
	for _, review := range latest {
		if review.state == "CHANGES_REQUESTED" {
			return "CHANGES_REQUESTED", nil
		}
		if review.state == "APPROVED" {
			approved = true
		}
	}
	if approved {
		return "APPROVED", nil
	}
	return "", nil
}

func (c *Client) fetchCheckStatus(owner, repo, sha string) (string, error) {
	var response struct {
		CheckRuns []struct {
			Status     string  `json:"status"`
			Conclusion *string `json:"conclusion"`
		} `json:"check_runs"`
	}
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(sha))
	if err := c.get(path, url.Values{"per_page": {"100"}}, &response); err != nil {
		return "", err
	}
	var statuses struct {
		Statuses []struct {
			State string `json:"state"`
		} `json:"statuses"`
	}
	statusPath := fmt.Sprintf("/repos/%s/%s/commits/%s/status", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(sha))
	if err := c.get(statusPath, url.Values{"per_page": {"100"}}, &statuses); err != nil {
		return "", err
	}
	if len(response.CheckRuns) == 0 && len(statuses.Statuses) == 0 {
		return "", nil
	}
	pending := false
	for _, run := range response.CheckRuns {
		if run.Status != "completed" || run.Conclusion == nil {
			pending = true
			continue
		}
		switch *run.Conclusion {
		case "failure", "timed_out", "cancelled", "action_required", "startup_failure", "stale":
			return "fail", nil
		case "neutral", "skipped", "success":
		default:
			pending = true
		}
	}
	for _, status := range statuses.Statuses {
		switch status.State {
		case "failure", "error":
			return "fail", nil
		case "pending", "expected":
			pending = true
		case "success":
		default:
			pending = true
		}
	}
	if pending {
		return "pending", nil
	}
	return "pass", nil
}

// FetchPRsForCommit resolves PRs containing an exact commit.
func (c *Client) FetchPRsForCommit(owner, repo, sha string) (*PRStatus, error) {
	var raw []apiPR
	if err := c.get(fmt.Sprintf("/repos/%s/%s/commits/%s/pulls", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(sha)), nil, &raw); err != nil {
		return nil, err
	}
	statuses := make([]PRStatus, len(raw))
	for i, pr := range raw {
		statuses[i] = statusFromAPI(pr)
	}
	return pickBestStatus(statuses), nil
}

func pickBestStatus(statuses []PRStatus) *PRStatus {
	if len(statuses) == 0 {
		return &PRStatus{HasPR: false}
	}
	sort.SliceStable(statuses, func(i, j int) bool {
		iOpen := strings.EqualFold(statuses[i].State, "OPEN")
		jOpen := strings.EqualFold(statuses[j].State, "OPEN")
		if iOpen != jOpen {
			return iOpen
		}
		return statuses[i].CreatedAt > statuses[j].CreatedAt
	})
	best := statuses[0]
	return &best
}

// BranchQuery pairs a branch with owner/repo context.
type BranchQuery struct{ Branch, Repo string }

func (c *Client) BatchFetchPRStatus(branches []string) (map[string]*PRStatus, error) {
	queries := make([]BranchQuery, len(branches))
	for i, branch := range branches {
		queries[i] = BranchQuery{Branch: branch}
	}
	return c.BatchFetchPRStatusWithRepo(queries)
}

func (c *Client) BatchFetchPRStatusWithRepo(queries []BranchQuery) (map[string]*PRStatus, error) {
	statuses := make(map[string]*PRStatus)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	var errs []string
	for _, query := range queries {
		query := query
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			status, err := c.FetchPRStatus(query.Branch, query.Repo)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", query.Branch, err))
				return
			}
			statuses[query.Branch] = status
		}()
	}
	wg.Wait()
	if len(errs) > 0 {
		return statuses, errors.New(strings.Join(errs, "; "))
	}
	return statuses, nil
}

func (c *Client) FetchCommentCount(owner, repo string, number int) (int, error) {
	status, err := c.FetchPRByNumber(owner, repo, number)
	if err != nil {
		return 0, err
	}
	return status.CommentCount, nil
}

func (c *Client) get(path string, query url.Values, target any) error {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub API request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return decodeAPIError(response)
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		return &APIError{Kind: ErrorResponse, StatusCode: response.StatusCode, Message: "invalid JSON response"}
	}
	return nil
}

func decodeAPIError(response *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	var payload struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &payload)
	kind := ErrorResponse
	switch {
	case response.StatusCode == http.StatusUnauthorized:
		kind = ErrorAuthentication
	case response.StatusCode == http.StatusForbidden && response.Header.Get("X-RateLimit-Remaining") == "0":
		kind = ErrorRateLimit
	case response.StatusCode == http.StatusTooManyRequests:
		kind = ErrorRateLimit
	case response.StatusCode == http.StatusForbidden:
		kind = ErrorAuthorization
	case response.StatusCode == http.StatusNotFound:
		kind = ErrorNotFound
	case response.StatusCode >= 500:
		kind = ErrorServer
	}
	apiErr := &APIError{Kind: kind, StatusCode: response.StatusCode, Message: payload.Message}
	if raw := response.Header.Get("X-RateLimit-Reset"); raw != "" {
		if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
			apiErr.ResetAt = time.Unix(seconds, 0)
		}
	}
	return apiErr
}

var (
	sshRemoteRe   = regexp.MustCompile(`(?:ssh://)?git@github\.com[:/]([^/]+)/(.+?)(?:\.git)?$`)
	httpsRemoteRe = regexp.MustCompile(`https://github\.com/([^/]+)/(.+?)(?:\.git)?$`)
)

func parseRemote(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	for _, pattern := range []*regexp.Regexp{sshRemoteRe, httpsRemoteRe} {
		if match := pattern.FindStringSubmatch(raw); match != nil {
			return match[1], strings.TrimSuffix(match[2], ".git"), nil
		}
	}
	return "", "", fmt.Errorf("could not parse GitHub remote URL: %s", raw)
}

// DetectRepo returns the origin repository in the process working directory.
func (c *Client) DetectRepo() (string, string, error) { return c.DetectRepoAt("") }

// DetectRepoAt returns the origin repository for an explicit checkout.
func (c *Client) DetectRepoAt(directory string) (string, string, error) {
	args := []string{"remote", "get-url", "origin"}
	cmd := c.execCommand("git", args...)
	if directory != "" {
		cmd.Dir = directory
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("detecting repo: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return parseRemote(string(out))
}

func (c *Client) DetectBranch() (string, error) { return c.DetectBranchAt("") }

func (c *Client) DetectBranchAt(directory string) (string, error) {
	cmd := c.execCommand("git", "rev-parse", "--abbrev-ref", "HEAD")
	if directory != "" {
		cmd.Dir = directory
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("detecting branch: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func splitRepo(repo string) (string, string, bool) {
	parts := strings.Split(repo, "/")
	returnPart := func() (string, string, bool) {
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", false
		}
		return parts[0], parts[1], true
	}
	return returnPart()
}
