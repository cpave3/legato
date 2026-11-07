package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Review events Legato posts: a clean pass approves, a pass with changes
// comments (never REQUEST_CHANGES).
const (
	ReviewApprove = "APPROVE"
	ReviewComment = "COMMENT"
)

// Operations of the github-reviewer profile, and the author App's.
const (
	opReviewPost = "pr.review"
	opReviewRead = "pr.read"
	opAuthorPost = "pr.write"
)

// GitHubIdentity is who a post is made as.
type GitHubIdentity int

const (
	// AsReviewer posts as the reviewer App: reviews, rulings, replies.
	AsReviewer GitHubIdentity = iota
	// AsAuthor posts as the author App (the github credential that lists
	// the repository): the author's answers.
	AsAuthor
)

// ReviewPlace is where a finding is in a pull request: a line (or the range
// StartLine..Line) on a side, RIGHT for the head and LEFT for a deleted line.
type ReviewPlace struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	StartLine int    `json:"start_line,omitempty"`
	Side      string `json:"side,omitempty"`
}

// ReviewPostFinding is one finding of a review as posted.
type ReviewPostFinding struct {
	Title string
	Body  string
	Place *ReviewPlace
}

// ReviewPost is one GitHub review of one pull request.
type ReviewPost struct {
	Repo     string
	Number   int
	CommitID string
	Event    string
	Summary  string
	// Findings are posted inline when their place is in the diff, and in
	// the body otherwise.
	Findings []ReviewPostFinding
	// Rulings are lines of the body: rulings on earlier findings that had
	// no thread.
	Rulings []string
	// Footer names the model and the skill that made the review.
	Footer string
	// Marker identifies the review among the pull request's (pass and
	// pull request): it is hidden in the body, and found again before any
	// retry so a review is never posted twice.
	Marker string
}

// PostedReview is a review on GitHub.
type PostedReview struct {
	ID  int64
	URL string
	// Existed says the review was found, not posted by this call.
	Existed bool
	// Inline are the indexes of the findings posted inline, in order, and
	// CommentIDs the inline comments GitHub made of them, in the same order.
	Inline     []int
	CommentIDs []int64
}

// GitHubError is GitHub refusing a call.
type GitHubError struct {
	Status  int
	Message string
	// RateLimited says GitHub limited the call; RetryAt is when to try again.
	RateLimited bool
	RetryAt     time.Time
}

func (e *GitHubError) Error() string {
	if e.RateLimited {
		return fmt.Sprintf("GitHub rate limit: %s (try again at %s)", e.Message, e.RetryAt.Format(time.RFC3339))
	}
	return fmt.Sprintf("GitHub answered %d: %s", e.Status, e.Message)
}

// ReviewPublisher posts review passes to GitHub from Legato's host side: as
// the reviewer App (reviews, rulings, thread replies) or as the author App
// (the author's answers). Each call makes a token for itself and revokes it.
// It is not the tracking client, which stays GET-only.
type ReviewPublisher struct {
	channels *CredentialChannels
	reviewer string
	// Timeout bounds each request to GitHub.
	Timeout time.Duration
}

// NewReviewPublisher posts with the reviewer credential named reviewer.
func NewReviewPublisher(channels *CredentialChannels, reviewer string) *ReviewPublisher {
	return &ReviewPublisher{channels: channels, reviewer: reviewer, Timeout: 30 * time.Second}
}

// call is one authorized request to GitHub's REST API.
type githubCall struct {
	p     *ReviewPublisher
	bc    brokerCredential
	token []byte
}

// open makes a token for repo as who, for an operation, and returns the call
// and the function that revokes the token.
func (p *ReviewPublisher) open(ctx context.Context, who GitHubIdentity, repo string, write bool) (*githubCall, func(), error) {
	var bc brokerCredential
	operation := opReviewRead
	switch who {
	case AsReviewer:
		found, ok := p.channels.broker.lookup(ctx, p.reviewer)
		if !ok || found.jwt == nil || found.upstream == nil {
			return nil, nil, fmt.Errorf("no reviewer GitHub credential named %q", p.reviewer)
		}
		bc = found
		if write {
			operation = opReviewPost
		}
	case AsAuthor:
		found, err := p.channels.githubCredentialFor(repo)
		if err != nil {
			return nil, nil, err
		}
		bc, operation = found, opAuthorPost
	}
	token, release, err := p.channels.githubToken(ctx, bc, repo, operation)
	if err != nil {
		return nil, nil, err
	}
	if release == nil {
		release = func() {}
	}
	return &githubCall{p: p, bc: bc, token: token}, release, nil
}

// do sends one request and decodes a 2xx answer into out.
func (c *githubCall) do(ctx context.Context, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, c.p.Timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(c.bc.upstream.String(), "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+string(c.token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := c.p.channels.broker.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		return githubError(resp, data)
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// githubError reads a refusal: GitHub's message and its errors, and a rate
// limit's reset.
func githubError(resp *http.Response, data []byte) error {
	var answer struct {
		Message string `json:"message"`
		Errors  []any  `json:"errors"`
	}
	_ = json.Unmarshal(data, &answer)
	message := answer.Message
	for _, e := range answer.Errors {
		switch v := e.(type) {
		case string:
			message += ": " + v
		case map[string]any:
			if m, ok := v["message"].(string); ok {
				message += ": " + m
			}
		}
	}
	if message == "" {
		message = resp.Status
	}
	out := &GitHubError{Status: resp.StatusCode, Message: message}
	if resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusForbidden && (resp.Header.Get("x-ratelimit-remaining") == "0" || strings.Contains(strings.ToLower(message), "rate limit"))) {
		out.RateLimited = true
		out.RetryAt = time.Now().Add(time.Minute)
		if after, err := strconv.Atoi(resp.Header.Get("retry-after")); err == nil {
			out.RetryAt = time.Now().Add(time.Duration(after) * time.Second)
		} else if reset, err := strconv.ParseInt(resp.Header.Get("x-ratelimit-reset"), 10, 64); err == nil {
			out.RetryAt = time.Unix(reset, 0)
		}
	}
	return out
}

// reviewMarker is the hidden line that names a review among a pull
// request's.
func reviewMarker(marker string) string { return "<!-- legato-review-pass " + marker + " -->" }

// PostReview posts one review of a pull request as the reviewer App. It
// anchors each finding against the pull request's diff, moving those it
// cannot anchor into the body. A review already posted with the same
// marker is returned, not posted again; after an error or a timeout the
// pull request's reviews are read for it before the error is reported.
func (p *ReviewPublisher) PostReview(ctx context.Context, post ReviewPost) (PostedReview, error) {
	if post.Event != ReviewApprove && post.Event != ReviewComment {
		return PostedReview{}, fmt.Errorf("a review is %s or %s, not %q", ReviewApprove, ReviewComment, post.Event)
	}
	if post.Marker == "" {
		return PostedReview{}, errors.New("a review needs a marker")
	}
	read, releaseRead, err := p.open(ctx, AsReviewer, post.Repo, false)
	if err != nil {
		return PostedReview{}, err
	}
	defer releaseRead()
	if found, ok, err := p.findReview(ctx, read, post); err != nil {
		return PostedReview{}, err
	} else if ok {
		return found, nil
	}
	diff, err := p.diffLines(ctx, read, post.Repo, post.Number)
	if err != nil {
		return PostedReview{}, err
	}
	body, comments, inline := composeReview(post, diff)

	write, releaseWrite, err := p.open(ctx, AsReviewer, post.Repo, true)
	if err != nil {
		return PostedReview{}, err
	}
	defer releaseWrite()
	var created struct {
		ID      int64  `json:"id"`
		HTMLURL string `json:"html_url"`
	}
	payload := map[string]any{"commit_id": post.CommitID, "event": post.Event, "body": body, "comments": comments}
	err = write.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/pulls/%d/reviews", post.Repo, post.Number), payload, &created)
	if err == nil {
		posted := PostedReview{ID: created.ID, URL: created.HTMLURL, Inline: inline}
		posted.CommentIDs = p.reviewComments(ctx, read, post.Repo, post.Number, created.ID)
		return posted, nil
	}
	var refused *GitHubError
	if errors.As(err, &refused) && refused.Status < 500 {
		return PostedReview{}, err // GitHub saw it and said no
	}
	// No answer, or a server error: GitHub may have taken it.
	if found, ok, findErr := p.findReview(context.WithoutCancel(ctx), read, post); findErr == nil && ok {
		found.Inline = inline
		return found, nil
	}
	return PostedReview{}, err
}

// reviewComments are the IDs of a review's inline comments, in the order
// they were posted; nil when GitHub does not say.
func (p *ReviewPublisher) reviewComments(ctx context.Context, call *githubCall, repo string, number int, reviewID int64) []int64 {
	var comments []struct {
		ID int64 `json:"id"`
	}
	if err := call.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d/reviews/%d/comments?per_page=100", repo, number, reviewID), nil, &comments); err != nil {
		return nil
	}
	ids := make([]int64, 0, len(comments))
	for _, c := range comments {
		ids = append(ids, c.ID)
	}
	return ids
}

// PullRequests are the open pull requests whose heads are branches, in the
// order of branches, read as the reviewer App.
func (p *ReviewPublisher) PullRequests(ctx context.Context, repo string, branches []string) ([]ReviewGitHubPR, error) {
	call, release, err := p.open(ctx, AsReviewer, repo, false)
	if err != nil {
		return nil, err
	}
	defer release()
	owner, _, _ := strings.Cut(repo, "/")
	var out []ReviewGitHubPR
	for _, branch := range branches {
		var pulls []struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			Body    string `json:"body"`
			HTMLURL string `json:"html_url"`
			Head    struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			} `json:"head"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
		}
		path := fmt.Sprintf("/repos/%s/pulls?state=open&head=%s:%s&per_page=10", repo, owner, url.QueryEscape(branch))
		if err := call.do(ctx, http.MethodGet, path, nil, &pulls); err != nil {
			return nil, err
		}
		for _, pr := range pulls {
			out = append(out, ReviewGitHubPR{Number: pr.Number, Branch: pr.Head.Ref, Base: pr.Base.Ref, Title: pr.Title, Body: pr.Body, URL: pr.HTMLURL, HeadSHA: pr.Head.SHA})
		}
	}
	return out, nil
}

// findReview looks among the pull request's reviews for one with the post's
// marker.
func (p *ReviewPublisher) findReview(ctx context.Context, call *githubCall, post ReviewPost) (PostedReview, bool, error) {
	marker := reviewMarker(post.Marker)
	for page := 1; page <= 50; page++ {
		var reviews []struct {
			ID      int64  `json:"id"`
			Body    string `json:"body"`
			HTMLURL string `json:"html_url"`
		}
		if err := call.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d/reviews?per_page=100&page=%d", post.Repo, post.Number, page), nil, &reviews); err != nil {
			return PostedReview{}, false, err
		}
		for _, review := range reviews {
			if strings.Contains(review.Body, marker) {
				return PostedReview{ID: review.ID, URL: review.HTMLURL, Existed: true}, true, nil
			}
		}
		if len(reviews) < 100 {
			break
		}
	}
	return PostedReview{}, false, nil
}

// diffSides are the lines of a file a comment may be on, per side, each
// with the hunk it is in.
type diffSides map[string]map[int]int

// diffLines reads which lines of each file the pull request's diff shows:
// path -> side -> line -> hunk.
func (p *ReviewPublisher) diffLines(ctx context.Context, call *githubCall, repo string, number int) (map[string]diffSides, error) {
	out := map[string]diffSides{}
	for page := 1; page <= 30; page++ {
		var files []struct {
			Filename string `json:"filename"`
			Patch    string `json:"patch"`
		}
		if err := call.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d/files?per_page=100&page=%d", repo, number, page), nil, &files); err != nil {
			return nil, err
		}
		for _, f := range files {
			out[f.Filename] = parsePatchLines(f.Patch)
		}
		if len(files) < 100 {
			break
		}
	}
	return out, nil
}

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// parsePatchLines reads a file's patch into the lines of each side it
// shows: RIGHT holds added and context lines at the head, LEFT deleted and
// context lines at the base.
func parsePatchLines(patch string) diffSides {
	sides := diffSides{"LEFT": {}, "RIGHT": {}}
	hunk, left, right := 0, 0, 0
	for _, line := range strings.Split(patch, "\n") {
		if m := hunkHeader.FindStringSubmatch(line); m != nil {
			hunk++
			left, _ = strconv.Atoi(m[1])
			right, _ = strconv.Atoi(m[2])
			continue
		}
		if hunk == 0 {
			continue
		}
		switch {
		case strings.HasPrefix(line, "+"):
			sides["RIGHT"][right] = hunk
			right++
		case strings.HasPrefix(line, "-"):
			sides["LEFT"][left] = hunk
			left++
		case strings.HasPrefix(line, "\\"):
		default:
			sides["LEFT"][left] = hunk
			sides["RIGHT"][right] = hunk
			left++
			right++
		}
	}
	return sides
}

// anchored says whether a place is in the diff: every line of it shown on
// its side, in one hunk.
func anchored(place *ReviewPlace, diff map[string]diffSides) bool {
	if place == nil || place.Line <= 0 {
		return false
	}
	side := place.Side
	if side == "" {
		side = "RIGHT"
	}
	lines := diff[place.Path][side]
	start := place.StartLine
	if start <= 0 {
		start = place.Line
	}
	hunk := lines[start]
	if hunk == 0 || start > place.Line {
		return false
	}
	for line := start; line <= place.Line; line++ {
		if lines[line] != hunk {
			return false
		}
	}
	return true
}

// composeReview is a review's body and inline comments: findings in the diff
// inline, the rest in the body as path:line.
func composeReview(post ReviewPost, diff map[string]diffSides) (string, []map[string]any, []int) {
	var body strings.Builder
	body.WriteString(strings.TrimSpace(post.Summary))
	comments := []map[string]any{}
	var inline []int
	var outside []string
	for i, finding := range post.Findings {
		text := "**" + strings.TrimSpace(finding.Title) + "**\n\n" + strings.TrimSpace(finding.Body)
		if anchored(finding.Place, diff) {
			comment := map[string]any{"path": finding.Place.Path, "line": finding.Place.Line, "side": sideOf(finding.Place), "body": text}
			if finding.Place.StartLine > 0 && finding.Place.StartLine < finding.Place.Line {
				comment["start_line"], comment["start_side"] = finding.Place.StartLine, sideOf(finding.Place)
			}
			comments = append(comments, comment)
			inline = append(inline, i)
			continue
		}
		where := ""
		if place := finding.Place; place != nil && place.Path != "" {
			where = "`" + place.Path
			if place.Line > 0 {
				where += ":" + strconv.Itoa(place.Line)
				if place.StartLine > 0 && place.StartLine < place.Line {
					where = "`" + place.Path + ":" + strconv.Itoa(place.StartLine) + "-" + strconv.Itoa(place.Line)
				}
			}
			where += "` "
		}
		outside = append(outside, "- "+where+strings.TrimSpace(finding.Title)+"\n\n  "+strings.ReplaceAll(strings.TrimSpace(finding.Body), "\n", "\n  "))
	}
	if len(outside) > 0 {
		body.WriteString("\n\n### Findings outside the diff\n\n" + strings.Join(outside, "\n"))
	}
	if len(post.Rulings) > 0 {
		body.WriteString("\n\n### Earlier findings\n\n- " + strings.Join(post.Rulings, "\n- "))
	}
	if post.Footer != "" {
		body.WriteString("\n\n---\n" + strings.TrimSpace(post.Footer))
	}
	body.WriteString("\n\n" + reviewMarker(post.Marker))
	return body.String(), comments, inline
}

func sideOf(place *ReviewPlace) string {
	if place.Side == "LEFT" {
		return "LEFT"
	}
	return "RIGHT"
}

// Reply answers in the review thread whose first comment is commentID.
func (p *ReviewPublisher) Reply(ctx context.Context, who GitHubIdentity, repo string, number int, commentID int64, body string) error {
	call, release, err := p.open(ctx, who, repo, true)
	if err != nil {
		return err
	}
	defer release()
	return call.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/pulls/%d/comments/%d/replies", repo, number, commentID), map[string]string{"body": body}, nil)
}

// PostIssueComment comments on the pull request's conversation, and returns
// the comment's ID.
func (p *ReviewPublisher) PostIssueComment(ctx context.Context, who GitHubIdentity, repo string, number int, body string) (int64, error) {
	call, release, err := p.open(ctx, who, repo, true)
	if err != nil {
		return 0, err
	}
	defer release()
	var created struct {
		ID int64 `json:"id"`
	}
	err = call.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, number), map[string]string{"body": body}, &created)
	return created.ID, err
}

// ResolveThread resolves the review thread holding commentID, as the
// reviewer App. A failure is returned: the caller keeps its reply and
// leaves the thread for a person.
func (p *ReviewPublisher) ResolveThread(ctx context.Context, repo string, number int, commentID int64) error {
	call, release, err := p.open(ctx, AsReviewer, repo, true)
	if err != nil {
		return err
	}
	defer release()
	owner, name, _ := strings.Cut(repo, "/")
	threadID := ""
	after := any(nil)
	for page := 0; page < 20 && threadID == ""; page++ {
		var found struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []struct {
							ID       string `json:"id"`
							Comments struct {
								Nodes []struct {
									FullDatabaseID string `json:"fullDatabaseId"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}
		query := `query($owner: String!, $name: String!, $number: Int!, $after: String) { repository(owner: $owner, name: $name) { pullRequest(number: $number) { reviewThreads(first: 100, after: $after) { pageInfo { hasNextPage endCursor } nodes { id comments(first: 1) { nodes { fullDatabaseId } } } } } } }`
		if err := p.channels.githubGraphQL(ctx, call.bc, call.token, query, map[string]any{"owner": owner, "name": name, "number": number, "after": after}, &found); err != nil {
			return err
		}
		threads := found.Repository.PullRequest.ReviewThreads
		for _, thread := range threads.Nodes {
			if len(thread.Comments.Nodes) > 0 && thread.Comments.Nodes[0].FullDatabaseID == strconv.FormatInt(commentID, 10) {
				threadID = thread.ID
			}
		}
		if !threads.PageInfo.HasNextPage {
			break
		}
		after = threads.PageInfo.EndCursor
	}
	if threadID == "" {
		return fmt.Errorf("no review thread of %s#%d starts with comment %d", repo, number, commentID)
	}
	return p.channels.githubGraphQL(ctx, call.bc, call.token, `mutation($id: ID!) { resolveReviewThread(input: {threadId: $id}) { thread { isResolved } } }`, map[string]any{"id": threadID}, nil)
}
