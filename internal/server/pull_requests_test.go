package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

func stackedCardDetail() *service.CardDetail {
	return &service.CardDetail{
		ID: "t1", Title: "Task one", Status: "Doing", CreatedAt: time.Now(), UpdatedAt: time.Now(),
		PRMeta: &service.PRMetaView{Repo: "acme/app", Branch: "legato/t1-a", PRNumber: 11, SyncOutcome: "linked"},
		PullRequests: []service.PRMetaView{
			{Repo: "acme/app", Branch: "legato/t1-a", PRNumber: 11, PRURL: "https://github.com/acme/app/pull/11", State: "OPEN", CheckStatus: "fail"},
			{Repo: "acme/app", Branch: "legato/t1-b", PRNumber: 12, PRURL: "https://github.com/acme/app/pull/12", Base: "legato/t1-a", State: "OPEN", IsDraft: true, ReviewDecision: "APPROVED", CommentCount: 2},
		},
		Branches: []service.TrackedBranchView{
			{Repo: "acme/app", Branch: "legato/t1", Source: "code_context"},
			{Repo: "acme/app", Branch: "legato/t1-a", Source: "stack", PR: &service.PRMetaView{PRNumber: 11}},
			{Repo: "acme/app", Branch: "legato/t1-b", Source: "stack", PR: &service.PRMetaView{PRNumber: 12}},
		},
	}
}

func TestBoardCardsListEveryPRInStackOrder(t *testing.T) {
	svc := &mockBoardService{
		columns:    []service.Column{{Name: "Doing"}},
		cards:      map[string][]service.Card{"Doing": {{ID: "t1", Title: "Task one", Status: "Doing"}}},
		cardDetail: stackedCardDetail(),
	}
	w := httptest.NewRecorder()
	New(svc, nil, nil, ":0").Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/board", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp BoardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	prs := resp.Columns[0].Cards[0].PullRequests
	if len(prs) != 2 || prs[0].PRNumber != 11 || prs[0].CheckStatus != "fail" || prs[1].PRNumber != 12 || !prs[1].IsDraft ||
		prs[1].ReviewDecision != "APPROVED" || prs[1].CommentCount != 2 || prs[1].Branch != "legato/t1-b" || prs[1].Base != "legato/t1-a" || prs[1].PRURL == "" {
		t.Fatalf("pull_requests = %+v", prs)
	}
}

func TestTaskDetailListsEveryPRAndTrackedBranch(t *testing.T) {
	svc := &mockBoardService{columns: []service.Column{}, cards: map[string][]service.Card{}, cardDetail: stackedCardDetail()}
	w := httptest.NewRecorder()
	New(svc, nil, nil, ":0").Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/tasks/t1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp CardDetailResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.PullRequests) != 2 || resp.PullRequests[0].PRNumber != 11 || resp.PullRequests[1].PRNumber != 12 {
		t.Fatalf("pull_requests = %+v", resp.PullRequests)
	}
	if len(resp.TrackedBranches) != 3 || resp.TrackedBranches[0].Branch != "legato/t1" || resp.TrackedBranches[0].PR != nil ||
		resp.TrackedBranches[2].Source != "stack" || resp.TrackedBranches[2].PR == nil || resp.TrackedBranches[2].PR.PRNumber != 12 {
		t.Fatalf("tracked_branches = %+v", resp.TrackedBranches)
	}
	if resp.PRMeta == nil || resp.PRMeta.PRNumber != 11 {
		t.Fatalf("pr_meta = %+v", resp.PRMeta)
	}
}

// stewardStates is a review loop lookup that says which pull requests a
// steward watches (service.ReviewLoopLookup.StewardState).
type stewardStates struct {
	fakeReviewLoops
	states map[int]string
}

func (s *stewardStates) StewardState(_ context.Context, repo string, pr int) string {
	if repo != "acme/app" {
		return ""
	}
	return s.states[pr]
}

// The board's PR chip says stewarding or held (spec LEG-559 story 13, AC
// 39), from the loop that holds each pull request.
func TestBoardPullRequestsSayWhetherAStewardWatchesThem(t *testing.T) {
	svc := &mockBoardService{
		columns:    []service.Column{{Name: "Doing"}},
		cards:      map[string][]service.Card{"Doing": {{ID: "t1", Title: "Task one", Status: "Doing"}}},
		cardDetail: stackedCardDetail(),
	}
	srv := New(svc, nil, nil, ":0")
	srv.SetReviewLoops(&stewardStates{states: map[int]string{11: service.StewardStateHeld, 12: service.StewardStateStewarding}})
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/board", nil))
	var resp BoardResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	prs := resp.Columns[0].Cards[0].PullRequests
	if len(prs) != 2 || prs[0].Steward != "held" || prs[1].Steward != "stewarding" {
		t.Fatalf("pull_requests = %+v", prs)
	}

	// A lookup that cannot say leaves the chip out.
	srv.SetReviewLoops(&fakeReviewLoops{})
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/board", nil))
	if strings.Contains(w.Body.String(), `"steward"`) {
		t.Fatalf("body %s", w.Body.String())
	}
}
