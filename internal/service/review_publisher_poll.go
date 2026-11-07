package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// PRComment is one comment on a pull request: a review's body, an inline
// comment (a thread's first, or a reply in it) or a conversation comment.
type PRComment struct {
	// Kind is review, inline or issue.
	Kind        string
	ID          int64
	ThreadRoot  int64 // an inline comment's thread: the ID of its first comment
	Author      string
	Bot         bool
	Association string
	Body        string
	URL         string
	Path        string
	Line        int
	State       string // a review's state
	Outdated    bool   // an inline comment whose lines changed since
	Resolved    bool   // an inline comment whose thread a person resolved
}

// CommentPoll is what one poll of a pull request found.
type CommentPoll struct {
	// Changed says a list changed since the last poll; Comments are all
	// of them either way.
	Changed  bool
	Comments []PRComment
}

// PollPages keeps the pages of a pull request's comment lists between
// polls, with their ETags: a page GitHub answers 304 for is read from here
// and costs no rate limit.
type PollPages interface {
	PollPage(ctx context.Context, list string, page int) (etag, body string, ok bool)
	SavePollPage(ctx context.Context, list string, page int, etag, body string) error
}

// pollLists are the lists polled: name -> path.
var pollLists = []struct{ name, path, kind string }{
	{"reviews", "/repos/%s/pulls/%d/reviews", "review"},
	{"comments", "/repos/%s/pulls/%d/comments", "inline"},
	{"issue_comments", "/repos/%s/issues/%d/comments", "issue"},
}

// PollComments reads a pull request's comments as the reviewer App: each
// list page by page with its ETag, and the review threads (resolved,
// outdated) only when a list changed.
func (p *ReviewPublisher) PollComments(ctx context.Context, repo string, number int, pages PollPages) (CommentPoll, error) {
	call, release, err := p.open(ctx, AsReviewer, repo, false)
	if err != nil {
		return CommentPoll{}, err
	}
	defer release()
	var poll CommentPoll
	for _, list := range pollLists {
		for page := 1; page <= 50; page++ {
			etag, cached, ok := pages.PollPage(ctx, list.name, page)
			status, newTag, body, err := call.conditional(ctx, fmt.Sprintf(list.path, repo, number)+"?per_page=100&page="+strconv.Itoa(page), etag)
			if err != nil {
				return CommentPoll{}, err
			}
			if status == http.StatusNotModified && ok {
				body = cached
			} else {
				if !ok || body != cached {
					poll.Changed = true
				}
				if err := pages.SavePollPage(ctx, list.name, page, newTag, body); err != nil {
					return CommentPoll{}, err
				}
			}
			n, err := appendComments(&poll.Comments, list.kind, body)
			if err != nil {
				return CommentPoll{}, err
			}
			if n < 100 {
				break
			}
		}
	}
	if !poll.Changed {
		return poll, nil
	}
	threads, err := p.threads(ctx, call, repo, number)
	if err != nil {
		return CommentPoll{}, err
	}
	for i := range poll.Comments {
		c := &poll.Comments[i]
		if c.Kind != "inline" {
			continue
		}
		if t, ok := threads[c.ThreadRoot]; ok {
			c.Resolved = t.resolved
			c.Outdated = c.Outdated || t.outdated
		}
	}
	return poll, nil
}

// conditional is a GET with If-None-Match: its status, ETag and body.
func (c *githubCall) conditional(ctx context.Context, path, etag string) (int, string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.p.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(c.bc.upstream.String(), "/")+path, nil)
	if err != nil {
		return 0, "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+string(c.token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	client := c.p.channels.broker.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusNotModified {
		return resp.StatusCode, etag, "", nil
	}
	if resp.StatusCode >= 300 {
		return 0, "", "", githubError(resp, data)
	}
	return resp.StatusCode, resp.Header.Get("ETag"), string(data), nil
}

// appendComments decodes a page of a list and says how many it held.
func appendComments(out *[]PRComment, kind, body string) (int, error) {
	var rows []struct {
		ID          int64  `json:"id"`
		InReplyTo   int64  `json:"in_reply_to_id"`
		Body        string `json:"body"`
		HTMLURL     string `json:"html_url"`
		Association string `json:"author_association"`
		State       string `json:"state"`
		Path        string `json:"path"`
		Line        *int   `json:"line"`
		User        struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"user"`
	}
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		return 0, fmt.Errorf("reading GitHub's %s comments: %w", kind, err)
	}
	for _, r := range rows {
		c := PRComment{Kind: kind, ID: r.ID, Author: r.User.Login, Bot: r.User.Type == "Bot", Association: r.Association,
			Body: r.Body, URL: r.HTMLURL, Path: r.Path, State: r.State}
		if kind == "inline" {
			c.ThreadRoot = r.ID
			if r.InReplyTo != 0 {
				c.ThreadRoot = r.InReplyTo
			}
			if r.Line != nil {
				c.Line = *r.Line
			} else if r.InReplyTo == 0 {
				c.Outdated = true
			}
		}
		*out = append(*out, c)
	}
	return len(rows), nil
}

type threadState struct{ resolved, outdated bool }

// threads are a pull request's review threads, by their first comment.
func (p *ReviewPublisher) threads(ctx context.Context, call *githubCall, repo string, number int) (map[int64]threadState, error) {
	owner, name, _ := strings.Cut(repo, "/")
	out := map[int64]threadState{}
	var after any
	for page := 0; page < 20; page++ {
		var found struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
						Nodes []struct {
							IsResolved bool `json:"isResolved"`
							IsOutdated bool `json:"isOutdated"`
							Comments   struct {
								Nodes []struct {
									FullDatabaseID string `json:"fullDatabaseId"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}
		query := `query($owner: String!, $name: String!, $number: Int!, $after: String) { repository(owner: $owner, name: $name) { pullRequest(number: $number) { reviewThreads(first: 100, after: $after) { pageInfo { hasNextPage endCursor } nodes { isResolved isOutdated comments(first: 1) { nodes { fullDatabaseId } } } } } } }`
		if err := p.channels.githubGraphQL(ctx, call.bc, call.token, query, map[string]any{"owner": owner, "name": name, "number": number, "after": after}, &found); err != nil {
			return nil, err
		}
		threads := found.Repository.PullRequest.ReviewThreads
		for _, t := range threads.Nodes {
			if len(t.Comments.Nodes) == 0 {
				continue
			}
			if id, err := strconv.ParseInt(t.Comments.Nodes[0].FullDatabaseID, 10, 64); err == nil {
				out[id] = threadState{resolved: t.IsResolved, outdated: t.IsOutdated}
			}
		}
		if !threads.PageInfo.HasNextPage {
			break
		}
		after = threads.PageInfo.EndCursor
	}
	return out, nil
}
