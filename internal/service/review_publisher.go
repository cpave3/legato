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
	"sync"
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
	// opReviewProtection reads branch protection, with administration
	// read, which a reviewer App need not have.
	opReviewProtection = "pr.protection"
	opAuthorPost       = "pr.write"
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
	// learnLogin is told the login a review was posted as: one of
	// Legato's own Apps, whose reviews never fire a trigger.
	learnLogin func(login string)
	// Timeout bounds each request to GitHub.
	Timeout time.Duration
	// logins are the bot logins each credential's App posts as, by
	// credential name, once read.
	mu     sync.Mutex
	logins map[string]string
}

// NewReviewPublisher posts with the reviewer credential named reviewer.
func NewReviewPublisher(channels *CredentialChannels, reviewer string) *ReviewPublisher {
	return &ReviewPublisher{channels: channels, reviewer: reviewer, Timeout: 30 * time.Second}
}

// SetLoginRecorder is told the login each review is posted as.
func (p *ReviewPublisher) SetLoginRecorder(record func(login string)) { p.learnLogin = record }

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

// login is the login the call's posts are made as on GitHub: its App's
// bot, <slug>[bot], the slug read with the installation. A comment or a
// review is taken for one Legato posted only when this login posted it;
// no person's login has the [bot] suffix.
func (c *githubCall) login(ctx context.Context, repo string) (string, error) {
	name := c.bc.config.Name
	c.p.mu.Lock()
	login, ok := c.p.logins[name]
	c.p.mu.Unlock()
	if ok {
		return login, nil
	}
	// Fail closed: without the App's name, a copy of a marker would be
	// taken for Legato's own post.
	if problem := c.bc.profile.AppProblem(); problem != "" {
		return "", fmt.Errorf("credential %s: %s", name, problem)
	}
	secret, err := c.p.channels.broker.readSecret(ctx, name)
	if err != nil {
		return "", err
	}
	owner, repoName, _ := strings.Cut(repo, "/")
	slug, known, err := c.bc.jwt.Actor(ctx, secret, map[string]any{"owner": owner, "repo": repoName})
	if err != nil {
		return "", err
	}
	if !known {
		return "", fmt.Errorf("credential %s: GitHub's installation of %s named no App at %s (the %s profile, %s), so what its App posted cannot be told from a copy", name, repo, c.bc.jwt.Template.Lookup.Actor, c.bc.profile.Name, c.bc.profile.origin())
	}
	login = slug + "[bot]"
	c.p.mu.Lock()
	if c.p.logins == nil {
		c.p.logins = map[string]string{}
	}
	c.p.logins[name] = login
	c.p.mu.Unlock()
	return login, nil
}

// postedBy says whether a comment's or a review's author is login: GitHub
// gives an App's bot as <slug>[bot] in REST, in any case.
func postedBy(author, login string) bool {
	return login != "" && strings.EqualFold(author, login)
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
	login, err := read.login(ctx, post.Repo)
	if err != nil {
		return PostedReview{}, err
	}
	if found, ok, err := p.findReview(ctx, read, post, login); err != nil {
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
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	payload := map[string]any{"commit_id": post.CommitID, "event": post.Event, "body": body, "comments": comments}
	err = write.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/pulls/%d/reviews", post.Repo, post.Number), payload, &created)
	if err == nil {
		if p.learnLogin != nil && created.User.Login != "" {
			p.learnLogin(created.User.Login)
		}
		posted := PostedReview{ID: created.ID, URL: created.HTMLURL, Inline: inline}
		posted.CommentIDs = p.reviewComments(ctx, read, post.Repo, post.Number, created.ID)
		return posted, nil
	}
	var refused *GitHubError
	if errors.As(err, &refused) && refused.Status < 500 {
		return PostedReview{}, err // GitHub saw it and said no
	}
	// No answer, or a server error: GitHub may have taken it.
	if found, ok, findErr := p.findReview(context.WithoutCancel(ctx), read, post, login); findErr == nil && ok {
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
	var out []ReviewGitHubPR
	for _, branch := range branches {
		pulls, err := openPulls(ctx, call, repo, PullQuery{Head: branch})
		if err != nil {
			return nil, err
		}
		out = append(out, pulls...)
	}
	return out, nil
}

// OpenPulls are the repository's open pull requests whose head (in the
// repository itself) or base is a branch, read as the reviewer App.
func (p *ReviewPublisher) OpenPulls(ctx context.Context, repo string, q PullQuery) ([]ReviewGitHubPR, error) {
	call, release, err := p.open(ctx, AsReviewer, repo, false)
	if err != nil {
		return nil, err
	}
	defer release()
	return openPulls(ctx, call, repo, q)
}

// openPulls lists open pull requests by head (owner:branch) or base, up to
// 100.
func openPulls(ctx context.Context, call *githubCall, repo string, q PullQuery) ([]ReviewGitHubPR, error) {
	var pulls []struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			Ref  string `json:"ref"`
			SHA  string `json:"sha"`
			Repo *struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	path := fmt.Sprintf("/repos/%s/pulls?state=open&per_page=100", repo)
	if q.Head != "" {
		owner, _, _ := strings.Cut(repo, "/")
		path += "&head=" + url.QueryEscape(owner+":"+q.Head)
	}
	if q.Base != "" {
		path += "&base=" + url.QueryEscape(q.Base)
	}
	if err := call.do(ctx, http.MethodGet, path, nil, &pulls); err != nil {
		return nil, err
	}
	out := make([]ReviewGitHubPR, 0, len(pulls))
	for _, pr := range pulls {
		headRepo := ""
		if pr.Head.Repo != nil {
			headRepo = pr.Head.Repo.FullName
		}
		out = append(out, ReviewGitHubPR{Number: pr.Number, Branch: pr.Head.Ref, Base: pr.Base.Ref, Title: pr.Title, Body: pr.Body,
			URL: pr.HTMLURL, HeadSHA: pr.Head.SHA, Author: pr.User.Login, HeadRepo: headRepo})
	}
	return out, nil
}

// findReview looks among the pull request's reviews for one with the post's
// marker that the reviewer App (login) posted.
func (p *ReviewPublisher) findReview(ctx context.Context, call *githubCall, post ReviewPost, login string) (PostedReview, bool, error) {
	marker := reviewMarker(post.Marker)
	for page := 1; page <= 50; page++ {
		var reviews []struct {
			ID      int64  `json:"id"`
			Body    string `json:"body"`
			HTMLURL string `json:"html_url"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if err := call.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/pulls/%d/reviews?per_page=100&page=%d", post.Repo, post.Number, page), nil, &reviews); err != nil {
			return PostedReview{}, false, err
		}
		for _, review := range reviews {
			if strings.Contains(review.Body, marker) && postedBy(review.User.Login, login) {
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

// Kinds of comment markers. Each kind is a hidden tag of its own, apart
// from a review's (legato-review-pass), so no kind is taken for another.
const (
	// MarkerAnswer marks an author's answer to a finding, a comment or a
	// check, posted as the author App.
	MarkerAnswer = "answer"
	// MarkerRuling marks a reviewer's ruling, replied in a finding's
	// thread as the reviewer App.
	MarkerRuling = "ruling"
)

// CommentMarker names one reply or conversation comment Legato posts. It
// is hidden in the comment, and looked for before the comment is posted
// and again after a timeout or a 5xx, so a comment GitHub took is found,
// not posted twice. The key must be unique to the comment, never derived
// from its text.
type CommentMarker struct {
	Kind string
	Key  string
}

// hidden is the line that names the comment on GitHub.
func (m CommentMarker) hidden() string { return "<!-- legato-" + m.Kind + " " + m.Key + " -->" }

func (m CommentMarker) valid() error {
	if (m.Kind != MarkerAnswer && m.Kind != MarkerRuling) || m.Key == "" || strings.Contains(m.Key, "\n") || strings.Contains(m.Key, "-->") {
		return fmt.Errorf("a comment needs a marker (%s or %s, with a key), not %+v", MarkerAnswer, MarkerRuling, m)
	}
	return nil
}

// Reply answers in the review thread whose first comment is commentID, once:
// a reply in that thread with the same marker, by the App who posts it, is
// taken for it.
func (p *ReviewPublisher) Reply(ctx context.Context, who GitHubIdentity, repo string, number int, commentID int64, body string, marker CommentMarker) error {
	_, err := p.postComment(ctx, who, repo, marker, fmt.Sprintf("/repos/%s/pulls/%d/comments", repo, number),
		fmt.Sprintf("/repos/%s/pulls/%d/comments/%d/replies", repo, number, commentID), body, commentID)
	return err
}

// PostIssueComment comments on the pull request's conversation once, and
// returns the comment's ID: a comment with the same marker already there,
// by the App who posts it, is taken for it.
func (p *ReviewPublisher) PostIssueComment(ctx context.Context, who GitHubIdentity, repo string, number int, body string, marker CommentMarker) (int64, error) {
	list := fmt.Sprintf("/repos/%s/issues/%d/comments", repo, number)
	return p.postComment(ctx, who, repo, marker, list, list, body, 0)
}

// postComment posts body with its hidden marker to create, unless a comment
// in list has the marker and was posted as who (in the thread of thread,
// for a reply). After a timeout or a 5xx, GitHub may have taken it: list
// is read again, and a comment found there is the answer.
func (p *ReviewPublisher) postComment(ctx context.Context, who GitHubIdentity, repo string, marker CommentMarker, list, create, body string, thread int64) (int64, error) {
	if err := marker.valid(); err != nil {
		return 0, err
	}
	call, release, err := p.open(ctx, who, repo, true)
	if err != nil {
		return 0, err
	}
	defer release()
	login, err := call.login(ctx, repo)
	if err != nil {
		return 0, err
	}
	if id, ok, err := findComment(ctx, call, list, marker, login, thread); err != nil {
		return 0, err
	} else if ok {
		return id, nil
	}
	var created struct {
		ID int64 `json:"id"`
	}
	err = call.do(ctx, http.MethodPost, create, map[string]string{"body": body + "\n\n" + marker.hidden()}, &created)
	if err == nil {
		return created.ID, nil
	}
	var refused *GitHubError
	if errors.As(err, &refused) && refused.Status < 500 {
		return 0, err // GitHub saw it and said no
	}
	if id, ok, findErr := findComment(context.WithoutCancel(ctx), call, list, marker, login, thread); findErr == nil && ok {
		return id, nil
	}
	return 0, err
}

// findComment looks through a list of comments for one with the marker
// that login posted, and, when thread is set, posted as a reply in the
// thread whose first comment is thread.
func findComment(ctx context.Context, call *githubCall, list string, marker CommentMarker, login string, thread int64) (int64, bool, error) {
	hidden := marker.hidden()
	for page := 1; page <= 50; page++ {
		var comments []struct {
			ID        int64  `json:"id"`
			Body      string `json:"body"`
			InReplyTo int64  `json:"in_reply_to_id"`
			User      struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if err := call.do(ctx, http.MethodGet, fmt.Sprintf("%s?per_page=100&page=%d", list, page), nil, &comments); err != nil {
			return 0, false, err
		}
		for _, c := range comments {
			if strings.Contains(c.Body, hidden) && postedBy(c.User.Login, login) && (thread == 0 || c.InReplyTo == thread) {
				return c.ID, true, nil
			}
		}
		if len(comments) < 100 {
			break
		}
	}
	return 0, false, nil
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
