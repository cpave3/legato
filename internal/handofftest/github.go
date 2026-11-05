package handofftest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/github"
)

// FakePR is a pull request the fake GitHub serves.
type FakePR struct {
	Number int
	Head   string
	// Base is the branch it merges into; main when empty.
	Base  string
	Draft bool
	// State is open (the default) or closed.
	State  string
	Merged bool
	// Review is the one reviewer's latest review (APPROVED,
	// CHANGES_REQUESTED), if any.
	Review string
	// Check is the one check run's conclusion (success, failure), if any.
	Check string
}

// FakeGitHub is GitHub's API on an httptest server, serving the reads a
// main's PR tracking makes: a repository's PR list, one PR, its reviews,
// and the checks and statuses of its head commit. Give it to a main with
// MainOptions.GitHub.
type FakeGitHub struct {
	t      testing.TB
	server *httptest.Server

	mu       sync.Mutex
	repos    map[string][]FakePR
	lists    int
	inFlight int
	most     int
	held     chan struct{}
}

// NewFakeGitHub starts a fake GitHub; t's cleanup stops it.
func NewFakeGitHub(t testing.TB) *FakeGitHub {
	t.Helper()
	f := &FakeGitHub{t: t, repos: map[string][]FakePR{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

// Set replaces a repository's PRs (owner/name), oldest first.
func (f *FakeGitHub) Set(repo string, prs ...FakePR) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repos[repo] = prs
}

// Client is a read-only GitHub client of the fake, as PR tracking uses.
func (f *FakeGitHub) Client() *github.Client {
	f.t.Helper()
	c, err := github.New(github.Options{Token: "handofftest-token", BaseURL: f.server.URL})
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}

// Hold makes every request wait until release is called, as a slow GitHub
// would; release lets them through. Call release before the test ends.
func (f *FakeGitHub) Hold() (release func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	held := make(chan struct{})
	f.held = held
	var once sync.Once
	return func() {
		once.Do(func() {
			f.mu.Lock()
			if f.held == held {
				f.held = nil
			}
			f.mu.Unlock()
			close(held)
		})
	}
}

// Lists counts the repository PR listings asked for so far.
func (f *FakeGitHub) Lists() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lists
}

// InFlight is the number of requests being answered now; MostInFlight the
// most there ever were at once.
func (f *FakeGitHub) InFlight() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inFlight
}

func (f *FakeGitHub) MostInFlight() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.most
}

func (f *FakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.inFlight++
	if f.inFlight > f.most {
		f.most = f.inFlight
	}
	held := f.held
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()
	if held != nil {
		select {
		case <-held:
		case <-r.Context().Done():
			return
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
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
		f.lists++
		list := make([]any, 0, len(prs))
		for i := len(prs) - 1; i >= 0; i-- { // newest first
			list = append(list, fakePRJSON(repo, prs[i]))
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
					reviews = append(reviews, map[string]any{"state": pr.Review, "submitted_at": "2026-11-05T09:00:00Z", "user": map[string]any{"login": "reviewer"}})
				}
				write(reviews)
				return
			}
			write(fakePRJSON(repo, pr))
			return
		}
		http.NotFound(w, r)
	case len(parts) == 6 && parts[3] == "commits" && parts[5] == "check-runs":
		runs := []any{}
		for _, pr := range prs {
			if fakeSHA(pr) == parts[4] && pr.Check != "" {
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

func fakePRJSON(repo string, pr FakePR) map[string]any {
	state := pr.State
	if state == "" {
		state = "open"
	}
	base := pr.Base
	if base == "" {
		base = "main"
	}
	return map[string]any{
		"number": pr.Number, "title": pr.Head, "state": state, "draft": pr.Draft, "merged": pr.Merged,
		"html_url":   "https://github.com/" + repo + "/pull/" + strconv.Itoa(pr.Number),
		"created_at": "2026-11-05T09:00:00Z",
		"head":       map[string]any{"ref": pr.Head, "sha": fakeSHA(pr), "repo": map[string]any{"full_name": repo}},
		"base":       map[string]any{"ref": base},
	}
}

func fakeSHA(pr FakePR) string { return "sha-" + strconv.Itoa(pr.Number) }
