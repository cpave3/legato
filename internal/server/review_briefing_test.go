package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cpave3/legato/internal/engine/reviewbriefing"
	"github.com/cpave3/legato/internal/service"
)

type stubReviewBriefings struct {
	index *reviewbriefing.Index
	pages map[string]string
	err   error
}

func (s stubReviewBriefings) Briefing(context.Context, string) (*reviewbriefing.Index, error) {
	return s.index, s.err
}

func (s stubReviewBriefings) Page(_ context.Context, _ string, path string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	html, ok := s.pages[path]
	if !ok {
		return "", service.ErrUnknownReviewPage
	}
	return html, nil
}

func reviewBriefingServer(t *testing.T, briefings ReviewBriefingService) *Server {
	t.Helper()
	srv := NewWithSwarm(nil, nil, nil, "", nil, nil, "")
	srv.SetReviewBriefingService(briefings, []string{"cdn.jsdelivr.net"})
	return srv
}

func TestReviewBriefingAPIListsTheAgentsPagesAndReadsOne(t *testing.T) {
	srv := reviewBriefingServer(t, stubReviewBriefings{
		index: &reviewbriefing.Index{Root: "/home/me/repo", Branch: "feat/login", Base: "main", Files: []string{".review/feat-login/index.html"}, Pages: []reviewbriefing.Page{
			{Path: ".review/feat-login/index.html", Folder: "feat-login", Label: "Overview", Version: "abc"},
		}},
		pages: map[string]string{".review/feat-login/index.html": "<h1>Login</h1>"},
	})

	list := agentUIRequest(srv, http.MethodGet, "/api/agents/task1/review-briefing")
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	var listed struct {
		Branch string                `json:"branch"`
		Pages  []reviewbriefing.Page `json:"pages"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Branch != "feat/login" || len(listed.Pages) != 1 || listed.Pages[0].Version != "abc" {
		t.Fatalf("listed = %+v", listed)
	}

	page := agentUIRequest(srv, http.MethodGet, "/api/agents/task1/review-briefing/page?path=.review/feat-login/index.html")
	if page.Code != http.StatusOK {
		t.Fatalf("page status=%d body=%s", page.Code, page.Body.String())
	}
	// The page travels inside JSON, never as a page of Legato's own address.
	if got := page.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type %q", got)
	}
	var read struct {
		HTML            string   `json:"html"`
		ResourceDomains []string `json:"resource_domains"`
	}
	if err := json.Unmarshal(page.Body.Bytes(), &read); err != nil {
		t.Fatal(err)
	}
	if read.HTML != "<h1>Login</h1>" || len(read.ResourceDomains) != 1 {
		t.Fatalf("read = %+v", read)
	}

	if missing := agentUIRequest(srv, http.MethodGet, "/api/agents/task1/review-briefing/page?path=README.md"); missing.Code != http.StatusNotFound {
		t.Fatalf("unknown page status=%d", missing.Code)
	}
}

func TestReviewBriefingAPIListsNothingForAnAgentWithoutACheckout(t *testing.T) {
	srv := reviewBriefingServer(t, stubReviewBriefings{err: service.ErrNoReviewBriefing})
	list := agentUIRequest(srv, http.MethodGet, "/api/agents/task1/review-briefing")
	if list.Code != http.StatusOK || list.Body.String() != "{\"pages\":[]}\n" {
		t.Fatalf("status=%d body=%q", list.Code, list.Body.String())
	}
}
