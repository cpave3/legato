package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type fakeReviewLoops struct {
	repo string
	pr   int
	loop *service.FoundReviewLoop
	err  error
}

func (f *fakeReviewLoops) ActiveReviewLoop(_ context.Context, repo string, pr int) (*service.FoundReviewLoop, error) {
	f.repo, f.pr = repo, pr
	return f.loop, f.err
}

func getReviewLoops(t *testing.T, srv *Server, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/review-loops?"+query, nil)
	req.Header.Set("Authorization", "Bearer secret")
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// The loop that holds a pull request is reported as the lookup gives it,
// asked with the repository and number of the query.
func TestReviewLoopsReportsTheLoopThatHoldsAPullRequest(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	lookup := &fakeReviewLoops{loop: &service.FoundReviewLoop{LoopID: "rl-0123456789ab", Kind: "run", RunID: "wr-1",
		StepPath: "review", Phase: "waiting", State: "open", Link: "/workflows/runs/wr-1"}}
	srv.SetReviewLoops(lookup)
	rec := getReviewLoops(t, srv, "repo=acme/widget&pr=12")
	want := `{"loop":{"loop_id":"rl-0123456789ab","kind":"run","run_id":"wr-1","step_path":"review","phase":"waiting","state":"open","held":false,"outcome":"","link":"/workflows/runs/wr-1"}}`
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if lookup.repo != "acme/widget" || lookup.pr != 12 {
		t.Fatalf("asked %s#%d", lookup.repo, lookup.pr)
	}

	lookup.loop = nil
	if rec := getReviewLoops(t, srv, "repo=acme/widget&pr=13"); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"agent_author":false,"loop":null}` {
		t.Fatalf("none: %d %s", rec.Code, rec.Body)
	}

	lookup.err = errors.New("disk on fire")
	if rec := getReviewLoops(t, srv, "repo=acme/widget&pr=13"); rec.Code != http.StatusInternalServerError {
		t.Fatalf("failed lookup: %d %s", rec.Code, rec.Body)
	}
}

type fakeReviewAuthors struct{ agent map[int]bool }

func (f fakeReviewAuthors) AgentAuthor(_ context.Context, _ string, pr int) bool { return f.agent[pr] }

// With no loop on a pull request, the answer says whether a review started
// now would treat it as an agent's: only when Legato's author App opened it
// (spec LEG-559 D8). A held one says nothing of it.
func TestReviewLoopsSaysWhoseAPullRequestIsWhenNoLoopHoldsIt(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	lookup := &fakeReviewLoops{}
	srv.SetReviewLoops(lookup)
	srv.SetReviewAuthors(fakeReviewAuthors{agent: map[int]bool{12: true}})
	if rec := getReviewLoops(t, srv, "repo=acme/widget&pr=12"); strings.TrimSpace(rec.Body.String()) != `{"agent_author":true,"loop":null}` {
		t.Fatalf("agent's: %d %s", rec.Code, rec.Body)
	}
	if rec := getReviewLoops(t, srv, "repo=acme/widget&pr=13"); strings.TrimSpace(rec.Body.String()) != `{"agent_author":false,"loop":null}` {
		t.Fatalf("person's: %d %s", rec.Code, rec.Body)
	}
	lookup.loop = &service.FoundReviewLoop{LoopID: "rl-1", Kind: "handoff", Link: "/board?highlight_task=t1"}
	if rec := getReviewLoops(t, srv, "repo=acme/widget&pr=12"); strings.Contains(rec.Body.String(), "agent_author") {
		t.Fatalf("held: %d %s", rec.Code, rec.Body)
	}
}

// A bad query is a 400 before any lookup, wired or not; a server not given
// the lookup finds no loop.
func TestReviewLoopsRefusesABadQuery(t *testing.T) {
	srv := New(nil, nil, nil, ":0")
	srv.SetAuthToken("secret")
	if rec := getReviewLoops(t, srv, "repo=acme/widget&pr=12"); rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"agent_author":false,"loop":null}` {
		t.Fatalf("unwired: %d %s", rec.Code, rec.Body)
	}
	lookup := &fakeReviewLoops{}
	srv.SetReviewLoops(lookup)
	for _, query := range []string{"", "repo=acme/widget", "pr=12", "repo=acme/widget&pr=twelve", "repo=acme/widget&pr=0", "repo=acme&pr=1"} {
		if rec := getReviewLoops(t, srv, query); rec.Code != http.StatusBadRequest {
			t.Errorf("?%s: %d %s", query, rec.Code, rec.Body)
		}
	}
	if lookup.repo != "" {
		t.Fatalf("a bad query was looked up: %s#%d", lookup.repo, lookup.pr)
	}
}
